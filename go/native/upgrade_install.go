package native

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// 升级流程阶段（UpgradeOptions.OnStage 的 stage 取值）。
const (
	StageChecking    = "checking"
	StageDownloading = "downloading"
	StageVerifying   = "verifying"
	StageInstalling  = "installing"
)

// InstallOptions 安装选项。SilentArgs 覆盖 exe 安装器默认静默参数（默认 /SILENT，NSIS/Inno 均兼容）。
type InstallOptions struct {
	SilentArgs string
}

// UpgradeOptions 一站式升级选项。
type UpgradeOptions struct {
	Install     *InstallOptions
	DownloadDir string // 安装包落地目录，默认系统临时目录
	// OnStage 阶段回调：checking/downloading(verifying 前 received/total 持续回调)/verifying/installing
	OnStage func(stage string, received, total int64)
}

// UpgradeReport 一站式升级结果。
type UpgradeReport struct {
	Result         *CheckResult // 检测结果（含版本信息与更新日志）
	InstallerPath  string       // 安装包落地路径
	InstallLaunched bool        // true = 安装器已启动，宿主应立即退出当前进程
}

// Install 启动安装器（分离进程，不等待安装完成）。返回 nil 即已启动，宿主应尽快退出。
//
//	Windows: .msi → msiexec /quiet；.exe → SilentArgs（默认 /SILENT）
//	Linux:   .deb → dpkg -i / .rpm → rpm -Uvh（自动尝试 pkexec 提权）；.AppImage → 替换当前可执行文件
//	macOS:   .pkg → installer -pkg；.dmg 场景差异大，返回错误由宿主处理
func (n *Native) Install(installerPath string, opts *InstallOptions) error {
	if _, err := os.Stat(installerPath); err != nil {
		return fmt.Errorf("安装包不存在: %w", err)
	}
	if opts == nil {
		opts = &InstallOptions{}
	}
	silent := opts.SilentArgs
	if silent == "" {
		silent = "/SILENT"
	}
	ext := strings.ToLower(filepath.Ext(installerPath))

	switch {
	case runtime.GOOS == "windows" && ext == ".msi":
		return runDetached("msiexec", "/i", installerPath, "/quiet", "/norestart")
	case runtime.GOOS == "windows" && ext == ".exe":
		return runDetached(installerPath, silent)
	case runtime.GOOS == "linux" && ext == ".deb":
		return runRoot("dpkg", "-i", installerPath)
	case runtime.GOOS == "linux" && ext == ".rpm":
		return runRoot("rpm", "-Uvh", installerPath)
	case runtime.GOOS == "linux" && ext == ".appimage":
		return replaceSelf(installerPath)
	case runtime.GOOS == "darwin" && ext == ".pkg":
		return runRoot("installer", "-pkg", installerPath, "-target", "/")
	default:
		return fmt.Errorf("平台 %s 暂不支持自动安装 %s 安装包，请宿主自行处理", runtime.GOOS, ext)
	}
}

// PerformUpgrade 一站式升级：检测 → 验签 → 下载（断点续传）→ 哈希校验 → 启动安装。
//
// has_update=false 或无下载地址时直接返回（InstallLaunched=false）；
// 任一步失败返回错误（下载不完整时 .part 保留供下次续传）。
// InstallLaunched=true 时安装器已分离启动，宿主应立即退出当前进程。
func (n *Native) PerformUpgrade(req CheckRequest, opts *UpgradeOptions) (*UpgradeReport, error) {
	if opts == nil {
		opts = &UpgradeOptions{}
	}
	stage := func(name string, received, total int64) {
		if opts.OnStage != nil {
			opts.OnStage(name, received, total)
		}
	}

	stage(StageChecking, 0, 0)
	r, err := n.CheckUpgrade(req)
	if err != nil {
		return nil, err
	}
	if !r.HasUpdate || r.DownloadURL == "" {
		return &UpgradeReport{Result: r}, nil
	}

	if ok, err := n.VerifyCheckResult(r); err != nil {
		return nil, err
	} else if !ok {
		return nil, fmt.Errorf("清单签名校验失败，拒绝升级")
	}

	dir := opts.DownloadDir
	if dir == "" {
		dir = os.TempDir()
	}
	dest := filepath.Join(dir, filenameFromURL(r.DownloadURL))
	stage(StageDownloading, 0, r.FileSize)
	if err := n.DownloadFile(r.DownloadURL, dest, r.FileSize, func(recv, total int64) {
		stage(StageDownloading, recv, total)
	}); err != nil {
		return nil, err
	}

	stage(StageVerifying, 0, 0)
	if err := n.VerifyFile(dest, r.SHA256, r.MD5); err != nil {
		_ = os.Remove(dest)
		return nil, fmt.Errorf("安装包校验失败: %w", err)
	}

	stage(StageInstalling, 0, 0)
	if err := n.Install(dest, opts.Install); err != nil {
		return nil, err
	}
	return &UpgradeReport{Result: r, InstallerPath: dest, InstallLaunched: true}, nil
}

// filenameFromURL 取 URL 尾段做文件名：去掉 query/fragment，非法字符替换，无文件名兜底。
func filenameFromURL(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	noFile := strings.HasSuffix(u, "/") || strings.HasSuffix(u, "\\")
	if i := strings.LastIndexAny(u, "/\\"); i >= 0 {
		u = u[i+1:]
	}
	if noFile || u == "" {
		return "installer.bin"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case ':', '*', '?', '"', '<', '>', '|':
			return '_'
		}
		return r
	}, u)
}

// runDetached 分离启动进程：不等待退出，宿主退出不牵连子进程。
func runDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动安装器失败: %w", err)
	}
	go func() { _, _ = cmd.Process.Wait() }() // 回收资源，不阻塞调用方
	return nil
}

// runRoot 需要管理员的安装命令：有 pkexec 用它提权，没有则直接执行（失败原因带回调用方）。
func runRoot(name string, args ...string) error {
	if p, err := exec.LookPath("pkexec"); err == nil {
		return runDetached(p, append([]string{name}, args...)...)
	}
	return runDetached(name, args...)
}

// replaceSelf AppImage 自替换：旧文件改名保留，新文件落到当前可执行路径。
func replaceSelf(newPath string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(self)
	if err != nil {
		real = self
	}
	if err := os.Rename(real, real+".old"); err != nil {
		return fmt.Errorf("旧版本改名失败: %w", err)
	}
	in, err := os.Open(newPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(real, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return nil // 宿主随后退出即可，新版本下次启动生效
}
