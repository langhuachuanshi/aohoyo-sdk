//! 安装器启动（分离进程）与一站式升级。
//!
//! 与 go/native 的 Install / PerformUpgrade 同构，行为差异仅在语言习惯（命名/错误类型）。

use crate::{CheckRequest, InstallOptions, Native, UpgradeOptions, UpgradeReport};

/// 升级流程阶段（UpgradeOptions.on_stage 的 stage 取值）。
pub const STAGE_CHECKING: &str = "checking";
pub const STAGE_DOWNLOADING: &str = "downloading";
pub const STAGE_VERIFYING: &str = "verifying";
pub const STAGE_INSTALLING: &str = "installing";

impl Native {
    /// 启动安装器（分离进程，不等待安装完成）。返回 Ok 即已启动，宿主应尽快退出当前进程。
    ///
    /// - Windows: `.msi` → `msiexec /quiet`；`.exe` → `silent_args`（默认 `/SILENT`，NSIS/Inno 兼容）；
    ///   `.zip` → 解包自替换（单文件包换血下次启动生效 / 多文件包脚本换血并自动重启，见 zip_install）
    /// - Linux: `.deb` → `dpkg -i` / `.rpm` → `rpm -Uvh`（自动尝试 pkexec 提权）；`.AppImage` → 替换当前可执行文件
    /// - macOS: `.pkg` → `installer -pkg`；`.dmg` 场景差异大，返回错误由宿主处理
    pub fn install(&self, installer_path: &str, opts: Option<&InstallOptions>) -> Result<(), Box<dyn std::error::Error>> {
        if !std::path::Path::new(installer_path).exists() {
            return Err(format!("安装包不存在: {installer_path}").into());
        }
        let silent = opts
            .and_then(|o| o.silent_args.as_deref())
            .filter(|s| !s.is_empty())
            .unwrap_or("/SILENT");
        #[cfg(not(windows))]
        let _ = silent;
        let ext = installer_path
            .rsplit('.')
            .next()
            .map(|e| e.to_ascii_lowercase())
            .unwrap_or_default();

        #[cfg(target_os = "windows")]
        {
            match ext.as_str() {
                "msi" => return run_detached("msiexec", &["/i", installer_path, "/quiet", "/norestart"]),
                "exe" => return run_detached(installer_path, &[silent]),
                "zip" => return crate::zip_install::install_zip(installer_path),
                _ => {}
            }
        }
        #[cfg(target_os = "linux")]
        {
            match ext.as_str() {
                "deb" => return run_root("dpkg", &["-i", installer_path]),
                "rpm" => return run_root("rpm", &["-Uvh", installer_path]),
                "appimage" => return replace_self(installer_path),
                _ => {}
            }
        }
        #[cfg(target_os = "macos")]
        {
            if ext == "pkg" {
                return run_root("installer", &["-pkg", installer_path, "-target", "/"]);
            }
        }
        #[cfg(not(any(target_os = "windows", target_os = "linux", target_os = "macos")))]
        let _ = (&ext, silent);

        Err(format!("当前平台暂不支持自动安装 .{ext} 安装包，请宿主自行处理").into())
    }

    /// 一站式升级：检测 → 验签 → 下载（断点续传）→ 哈希校验 → 启动安装。
    ///
    /// has_update=false 或无下载地址时直接返回（install_launched=false）；
    /// 任一步失败返回 Err（下载不完整时 .part 保留供下次续传）。
    /// install_launched=true 时安装器已分离启动，宿主应立即退出当前进程。
    pub fn perform_upgrade(
        &self,
        req: &CheckRequest,
        mut opts: UpgradeOptions,
    ) -> Result<UpgradeReport, Box<dyn std::error::Error>> {
        if let Some(cb) = opts.on_stage.as_mut() {
            cb(STAGE_CHECKING, 0, 0);
        }
        let r = self.check_upgrade(req)?;
        if !r.has_update || r.download_url.is_empty() {
            return Ok(UpgradeReport {
                result: r,
                installer_path: String::new(),
                install_launched: false,
            });
        }

        if !self.verify_check_result(&r)? {
            return Err("清单签名校验失败，拒绝升级".into());
        }

        let dir = opts
            .download_dir
            .clone()
            .unwrap_or_else(|| std::env::temp_dir().to_string_lossy().to_string());
        let dest = std::path::Path::new(&dir).join(filename_from_url(&r.download_url));
        let dest = dest.to_string_lossy().to_string();

        if let Some(cb) = opts.on_stage.as_mut() {
            cb(STAGE_DOWNLOADING, 0, r.file_size.max(0) as u64);
        }
        // install 选项先行取走，避免与下面的 on_stage 可变借用冲突
        let install_opts = opts.install.clone();
        let on_stage = &mut opts.on_stage;
        let mut progress = |recv: u64, total: u64| {
            if let Some(cb) = on_stage.as_mut() {
                cb(STAGE_DOWNLOADING, recv, total);
            }
        };
        self.download_file(&r.download_url, &dest, r.file_size.max(0) as u64, Some(&mut progress))?;

        if let Some(cb) = opts.on_stage.as_mut() {
            cb(STAGE_VERIFYING, 0, 0);
        }
        if let Err(e) = self.verify_file(&dest, &r.sha256, &r.md5) {
            let _ = std::fs::remove_file(&dest);
            return Err(format!("安装包校验失败: {e}").into());
        }

        if let Some(cb) = opts.on_stage.as_mut() {
            cb(STAGE_INSTALLING, 0, 0);
        }
        self.install(&dest, install_opts.as_ref())?;

        Ok(UpgradeReport {
            result: r,
            installer_path: dest,
            install_launched: true,
        })
    }
}

/// 取 URL 尾段做文件名：去掉 query/fragment，无文件名兜底。
pub(crate) fn filename_from_url(u: &str) -> String {
    let u = u.split(['?', '#']).next().unwrap_or("");
    let name = u.rsplit(['/', '\\']).next().unwrap_or("");
    let name = name
        .chars()
        .map(|c| match c {
            ':' | '*' | '?' | '"' | '<' | '>' | '|' => '_',
            _ => c,
        })
        .collect::<String>();
    if name.is_empty() || u.ends_with('/') || u.ends_with('\\') {
        "installer.bin".to_string()
    } else {
        name
    }
}

/// 分离启动进程：不等待退出，宿主退出不牵连子进程。
pub(crate) fn run_detached(program: &str, args: &[&str]) -> Result<(), Box<dyn std::error::Error>> {
    std::process::Command::new(program)
        .args(args)
        .spawn()
        .map(|_| ())
        .map_err(|e| format!("启动安装器失败: {e}").into())
}

/// 需要管理员的安装命令：有 pkexec 用它提权，没有则直接执行。
fn run_root(program: &str, args: &[&str]) -> Result<(), Box<dyn std::error::Error>> {
    if let Ok(pk) = which_pkexec() {
        return std::process::Command::new(&pk)
            .arg(program)
            .args(args)
            .spawn()
            .map(|_| ())
            .map_err(|e| format!("启动安装器失败: {e}").into());
    }
    run_detached(program, args)
}

fn which_pkexec() -> Result<String, std::io::Error> {
    for dir in ["/usr/bin", "/usr/local/bin", "/bin"] {
        let p = std::path::Path::new(dir).join("pkexec");
        if p.exists() {
            return Ok(p.to_string_lossy().to_string());
        }
    }
    Err(std::io::Error::new(std::io::ErrorKind::NotFound, "pkexec"))
}

/// 自替换：旧文件改名保留（统一 `.old` 后缀），新文件落到当前可执行路径（下次启动生效）。
/// AppImage（Linux）与 Windows 单文件 zip 包共用。
pub(crate) fn replace_self(new_path: &str) -> Result<(), Box<dyn std::error::Error>> {
    let self_path = std::env::current_exe()?;
    let real = std::fs::canonicalize(&self_path).unwrap_or(self_path);
    let file_name = real
        .file_name()
        .ok_or("无法定位当前可执行文件名")?
        .to_string_lossy()
        .to_string();
    let old = real.with_file_name(format!("{file_name}.old"));
    std::fs::rename(&real, &old).map_err(|e| format!("旧版本改名失败: {e}"))?;
    if let Err(e) = std::fs::copy(new_path, &real) {
        // 新文件落位失败 → 回滚改名，保持原样
        let _ = std::fs::rename(&old, &real);
        return Err(format!("新版本落位失败: {e}").into());
    }
    set_executable(&real)?;
    Ok(())
}

#[cfg(unix)]
fn set_executable(p: &std::path::Path) -> Result<(), Box<dyn std::error::Error>> {
    use std::os::unix::fs::PermissionsExt;
    let mut perm = std::fs::metadata(p)?.permissions();
    perm.set_mode(0o755);
    std::fs::set_permissions(p, perm)?;
    Ok(())
}

#[cfg(not(unix))]
fn set_executable(_p: &std::path::Path) -> Result<(), Box<dyn std::error::Error>> {
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::filename_from_url;

    #[test]
    fn filename_from_url_cases() {
        assert_eq!(
            filename_from_url("https://cdn.example.com/apps/demo/versions/setup-1.3.0.exe"),
            "setup-1.3.0.exe"
        );
        assert_eq!(filename_from_url("https://x.com/a/b/app-2.0.msi?sign=abc"), "app-2.0.msi");
        assert_eq!(filename_from_url("https://x.com/"), "installer.bin");
    }
}
