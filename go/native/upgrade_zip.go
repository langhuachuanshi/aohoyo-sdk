package native

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Windows zip 包安装：解压自替换（与 rust/native zip_install.rs 同构）。
//
// zip 语义约定（主仓库 docs/specs/upgrade-integration.md §7）：
//   - zip 内容 = 应用安装目录的完整内容（主 exe + 同目录依赖），或仅一个主 exe
//   - 主 exe 识别：zip 根下与 zip 同名（去 .zip）的 .exe，否则根下唯一的 .exe；识别不出即报错
//   - 单文件 zip：进程内换血（当前 exe 改名 .old 保留 → 新 exe 落位），下次启动生效，不自动重启（同 AppImage）
//   - 多文件 zip：解压到安装目录旁 staging → 分离脚本等宿主退出后整目录换血（旧目录留 .old）并自动重启新版
//   - 非 Windows 平台维持「暂不支持」报错，由宿主自行处理

// installZip zip 包安装入口。Install 按 .zip 后缀路由到此处。
func installZip(zipPath string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("平台 %s 暂不支持自动安装 .zip 安装包，请宿主自行处理", runtime.GOOS)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(self)
	if err != nil {
		real = self
	}
	appDir := filepath.Dir(real)
	ts := time.Now().Unix()
	pid := os.Getpid()

	// staging 必须与安装目录同盘同父目录，脚本才能用 move 做整目录换血
	staging := fmt.Sprintf("%s.update-%d-%d", appDir, ts, pid)
	_ = os.RemoveAll(staging)
	if err := extractZip(zipPath, staging); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}

	zipStem := strings.TrimSuffix(filepath.Base(zipPath), filepath.Ext(zipPath))
	exeRel, err := findMainExe(staging, zipStem)
	if err != nil {
		_ = os.RemoveAll(staging)
		return err
	}

	// 单文件 zip：进程内换血（Windows 允许重命名运行中的 exe），无需脚本
	if isSingleFilePackage(staging) {
		if err := replaceSelf(filepath.Join(staging, exeRel)); err != nil {
			_ = os.RemoveAll(staging)
			return err
		}
		_ = os.RemoveAll(staging)
		return nil
	}

	// 多文件 zip：分离脚本执行换血（宿主退出后 move 换血 → 启动新版 → 自删）
	script := filepath.Join(os.TempDir(), fmt.Sprintf("aohoyo-upgrade-%d-%d.bat", ts, pid))
	if err := os.WriteFile(script, []byte(buildUpgradeScript(appDir, staging, exeRel)), 0o644); err != nil {
		return fmt.Errorf("写升级脚本失败: %w", err)
	}
	return runDetached("cmd", "/c", script)
}

// extractZip 解压 zip 到 destDir：路径安全校验（防 Zip Slip）+ 顶层唯一目录自动剥壳。
func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("读取 zip 失败: %w", err)
	}
	defer r.Close()

	entries := make([]struct {
		Name  string
		IsDir bool
	}, 0, len(r.File))
	for _, f := range r.File {
		entries = append(entries, struct {
			Name  string
			IsDir bool
		}{f.Name, f.FileInfo().IsDir()})
	}
	prefix, err := commonRootPrefix(entries)
	if err != nil {
		return err
	}

	for _, f := range r.File {
		rel, err := sanitizeEntryName(f.Name)
		if err != nil {
			return err
		}
		if rel == "" {
			continue
		}
		if prefix != "" {
			trimmed, ok := strings.CutPrefix(rel, prefix)
			if !ok || trimmed == "" {
				continue // 顶层包裹目录自身，跳过
			}
			rel = trimmed
		}
		out := filepath.Join(destDir, filepath.FromSlash(rel))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(out, 0o755); err != nil {
				return fmt.Errorf("创建目录 %s 失败: %w", rel, err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return fmt.Errorf("创建目录失败: %w", err)
		}
		if err := copyZipEntry(f, out, rel); err != nil {
			return err
		}
	}
	return nil
}

func copyZipEntry(f *zip.File, outPath, rel string) error {
	src, err := f.Open()
	if err != nil {
		return fmt.Errorf("读取 zip 条目 %s 失败: %w", rel, err)
	}
	defer src.Close()
	dst, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("写文件 %s 失败: %w", rel, err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("解压条目 %s 失败: %w", rel, err)
	}
	return nil
}

// sanitizeEntryName 条目路径安全校验：拒绝 .. 穿越、绝对路径、盘符/冒号、空字节；`\` 归一为 `/`。空目录条目返回空串。
func sanitizeEntryName(name string) (string, error) {
	normalized := strings.ReplaceAll(name, `\`, "/")
	if strings.HasPrefix(normalized, "/") {
		return "", fmt.Errorf("zip 条目为绝对路径，拒绝解包: %s", name)
	}
	segs := strings.Split(normalized, "/")
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		if s == "" {
			continue
		}
		if s == ".." {
			return "", fmt.Errorf("zip 条目含 .. 路径穿越，拒绝解包: %s", name)
		}
		if strings.Contains(s, ":") {
			return "", fmt.Errorf("zip 条目含盘符/冒号，拒绝解包: %s", name)
		}
		if strings.Contains(s, "\x00") {
			return "", fmt.Errorf("zip 条目含空字节，拒绝解包: %s", name)
		}
		out = append(out, s)
	}
	return strings.Join(out, "/"), nil
}

// commonRootPrefix 顶层唯一目录剥壳判定：所有条目都挂在同一个目录下（且根下没有散文件）时返回 `目录名/`。
// 根下存在任何散文件（不含目录项）→ 无包裹，返回空串。
func commonRootPrefix(entries []struct {
	Name  string
	IsDir bool
}) (string, error) {
	top := ""
	for _, e := range entries {
		n := strings.ReplaceAll(e.Name, `\`, "/")
		segs := strings.Split(n, "/")
		compact := make([]string, 0, len(segs))
		for _, s := range segs {
			if s != "" {
				compact = append(compact, s)
			}
		}
		if len(compact) == 0 {
			continue
		}
		if len(compact) == 1 && !e.IsDir {
			return "", nil // 根下有散文件，无顶层包裹
		}
		t := compact[0]
		if top == "" {
			top = t
		} else if top != t {
			return "", nil
		}
	}
	if top == "" {
		return "", nil
	}
	return top + "/", nil
}

// findMainExe 主 exe 识别：根下唯一 exe 直接采用；多个 exe 时取与 zip 同名（去 .zip，忽略大小写）的那个。
// 返回相对根目录的路径（`/` 分隔）。
func findMainExe(dir, zipStem string) (string, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("读取解压目录失败: %w", err)
	}
	var exes []string
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(de.Name()), ".exe") {
			exes = append(exes, de.Name())
		}
	}
	switch {
	case len(exes) == 0:
		return "", fmt.Errorf("zip 内未找到主程序 exe：请把应用目录内容（含主 exe）打进 zip")
	case len(exes) == 1:
		return exes[0], nil
	}
	want := strings.ToLower(zipStem)
	for _, name := range exes {
		if strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name))) == want {
			return name, nil
		}
	}
	return "", fmt.Errorf("zip 根下有多个 exe 且无与包名 %s 同名的主程序，无法识别", zipStem)
}

// isSingleFilePackage 单文件包判定：解压结果顶层只有 1 个文件、无目录。
func isSingleFilePackage(dir string) bool {
	des, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	files := 0
	for _, de := range des {
		if de.IsDir() {
			return false
		}
		files++
	}
	return files == 1
}

// buildUpgradeScript 生成分离升级脚本（bat，UTF-8 + chcp 65001，行尾 CRLF）：
// 清理上次残留 → 等宿主退出 → 整目录换血（move 失败退 robocopy）→ 启动新版 → 自删。
// 与 rust/native zip_install.rs 的 build_upgrade_script 输出逐行一致，改动两侧同步。
func buildUpgradeScript(appDir, stagingDir, exeRel string) string {
	app := strings.TrimSuffix(appDir, `\`)
	staging := strings.TrimSuffix(stagingDir, `\`)
	exePath := app + `\` + strings.ReplaceAll(exeRel, "/", `\`)
	lines := []string{
		`@echo off`,
		`chcp 65001 >nul`,
		`set "APP=` + app + `"`,
		`set "NEW=` + staging + `"`,
		`set "EXE=` + exePath + `"`,
		`for /d %%D in ("` + app + `.update-*") do (if /i not "%%~fD"=="` + staging + `" rd /s /q "%%~fD")`,
		`rd /s /q "` + app + `.old" >nul 2>&1`,
		`ping -n 3 127.0.0.1 >nul`,
		`set /a N=0`,
		`:swap`,
		`move /y "%APP%" "%APP%.old" >nul 2>&1`,
		`if not exist "%APP%\" goto :apply`,
		`set /a N+=1`,
		`if %N% lss 15 (`,
		`  ping -n 2 127.0.0.1 >nul`,
		`  goto :swap`,
		`)`,
		`:apply`,
		`move /y "%NEW%" "%APP%" >nul 2>&1`,
		`if exist "%APP%\" goto :launch`,
		`robocopy "%NEW%" "%APP%" /e /purge /r:2 /w:1 >nul`,
		`:launch`,
		`start "" "%EXE%"`,
		`(goto) 2>nul & del "%~f0"`,
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}
