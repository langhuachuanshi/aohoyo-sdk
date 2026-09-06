//! Windows zip 包安装：解压自替换（与 go/native upgrade_zip.go 同构）。
//!
//! zip 语义约定（主仓库 docs/specs/upgrade-integration.md §7）：
//! - zip 内容 = 应用安装目录的完整内容（主 exe + 同目录依赖），或仅一个主 exe
//! - 主 exe 识别：zip 根下与 zip 同名（去 .zip）的 `.exe`，否则根下唯一的 `.exe`；识别不出即报错
//! - 单文件 zip：进程内换血（当前 exe 改名 `.old` 保留 → 新 exe 落位），下次启动生效，不自动重启（同 AppImage）
//! - 多文件 zip：解压到安装目录旁 staging → 分离脚本等宿主退出后整目录换血（旧目录留 `.old`）并自动重启新版
//! - 非 Windows 平台维持「暂不支持」报错，由宿主自行处理

use std::path::Path;

/// zip 包安装入口。install() 按 `.zip` 后缀路由到此处。
pub(crate) fn install_zip(zip_path: &str) -> Result<(), Box<dyn std::error::Error>> {
    #[cfg(not(target_os = "windows"))]
    {
        let _ = zip_path;
        return Err("当前平台暂不支持自动安装 .zip 包，请宿主自行处理".into());
    }

    #[cfg(target_os = "windows")]
    {
        let self_path = std::env::current_exe()?;
        let real = std::fs::canonicalize(&self_path).unwrap_or(self_path);
        let app_dir = real
            .parent()
            .ok_or("无法定位安装目录")?
            .to_string_lossy()
            .trim_end_matches('\\')
            .to_string();
        let ts = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_secs())
            .unwrap_or(0);
        let pid = std::process::id();

        // staging 必须与安装目录同盘同父目录，脚本才能用 move 做整目录换血
        let staging = format!("{app_dir}.update-{ts}-{pid}");
        let _ = std::fs::remove_dir_all(&staging);
        if let Err(e) = extract_zip(zip_path, Path::new(&staging)) {
            let _ = std::fs::remove_dir_all(&staging);
            return Err(e);
        }

        let zip_stem = Path::new(zip_path)
            .file_stem()
            .map(|s| s.to_string_lossy().to_string())
            .unwrap_or_default();
        let exe_rel = match find_main_exe(Path::new(&staging), &zip_stem) {
            Ok(p) => p,
            Err(e) => {
                let _ = std::fs::remove_dir_all(&staging);
                return Err(e.into());
            }
        };

        // 单文件 zip：进程内换血（Windows 允许重命名运行中的 exe），无需脚本
        if is_single_file_package(Path::new(&staging)) {
            let new_exe = Path::new(&staging)
                .join(&exe_rel)
                .to_string_lossy()
                .to_string();
            if let Err(e) = super::install::replace_self(&new_exe) {
                let _ = std::fs::remove_dir_all(&staging);
                return Err(e);
            }
            let _ = std::fs::remove_dir_all(&staging);
            return Ok(());
        }

        // 多文件 zip：分离脚本执行换血（宿主退出后 move 换血 → 启动新版 → 自删）
        let script = std::env::temp_dir().join(format!("aohoyo-upgrade-{ts}-{pid}.bat"));
        std::fs::write(&script, build_upgrade_script(&app_dir, &staging, &exe_rel.to_string_lossy()))
            .map_err(|e| format!("写升级脚本失败: {e}"))?;
        super::install::run_detached("cmd", &["/c", &script.to_string_lossy()])
    }
}

/// 解压 zip 到 dest：路径安全校验（防 Zip Slip）+ 顶层唯一目录自动剥壳。
pub(crate) fn extract_zip(zip_path: &str, dest: &Path) -> Result<(), Box<dyn std::error::Error>> {
    let file = std::fs::File::open(zip_path).map_err(|e| format!("打开 zip 失败: {e}"))?;
    let mut archive = zip::ZipArchive::new(file).map_err(|e| format!("读取 zip 失败: {e}"))?;

    let mut entries: Vec<(usize, String, bool)> = Vec::with_capacity(archive.len());
    for i in 0..archive.len() {
        let e = archive.by_index(i).map_err(|e| format!("读取 zip 条目失败: {e}"))?;
        entries.push((i, e.name().to_string(), e.is_dir()));
    }
    let prefix = common_root_prefix(&entries)?;

    for (idx, name, is_dir) in &entries {
        let rel = sanitize_entry_name(name)?;
        if rel.is_empty() {
            continue;
        }
        let rel = match &prefix {
            Some(p) => match rel.strip_prefix(p.as_str()) {
                Some(r) if !r.is_empty() => r.to_string(),
                _ => continue, // 顶层包裹目录自身，跳过
            },
            None => rel,
        };
        let out = dest.join(&rel);
        if *is_dir {
            std::fs::create_dir_all(&out).map_err(|e| format!("创建目录 {rel} 失败: {e}"))?;
        } else {
            if let Some(parent) = out.parent() {
                std::fs::create_dir_all(parent).map_err(|e| format!("创建目录失败: {e}"))?;
            }
            let mut src = archive
                .by_index(*idx)
                .map_err(|e| format!("读取 zip 条目 {rel} 失败: {e}"))?;
            let mut out_file = std::fs::File::create(&out)
                .map_err(|e| format!("写文件 {rel} 失败: {e}"))?;
            std::io::copy(&mut src, &mut out_file)
                .map_err(|e| format!("解压条目 {rel} 失败: {e}"))?;
        }
    }
    Ok(())
}

/// 条目路径安全校验：拒绝 `..` 穿越、绝对路径、盘符/冒号、空字节；`\` 归一为 `/`。空目录条目返回空串。
pub(crate) fn sanitize_entry_name(name: &str) -> Result<String, String> {
    let normalized = name.replace('\\', "/");
    if normalized.starts_with('/') {
        return Err(format!("zip 条目为绝对路径，拒绝解包: {name}"));
    }
    let segs: Vec<&str> = normalized.split('/').filter(|s| !s.is_empty()).collect();
    if segs.is_empty() {
        return Ok(String::new());
    }
    for s in &segs {
        if *s == ".." {
            return Err(format!("zip 条目含 .. 路径穿越，拒绝解包: {name}"));
        }
        if s.contains(':') {
            return Err(format!("zip 条目含盘符/冒号，拒绝解包: {name}"));
        }
        if s.contains('\0') {
            return Err(format!("zip 条目含空字节，拒绝解包: {name}"));
        }
    }
    Ok(segs.join("/"))
}

/// 顶层唯一目录剥壳判定：所有条目都挂在同一个目录下（且根下没有散文件）时返回 `目录名/`。
/// 根下存在任何散文件（不含目录项）→ 无包裹，返回 None。
pub(crate) fn common_root_prefix(entries: &[(usize, String, bool)]) -> Result<Option<String>, String> {
    let mut top: Option<String> = None;
    for (_, name, is_dir) in entries {
        let n = name.replace('\\', "/");
        let segs: Vec<&str> = n.split('/').filter(|s| !s.is_empty()).collect();
        if segs.is_empty() {
            continue;
        }
        if segs.len() == 1 && !is_dir {
            return Ok(None); // 根下有散文件，无顶层包裹
        }
        let t = segs[0].to_string();
        match &top {
            None => top = Some(t),
            Some(prev) if *prev == t => {}
            Some(_) => return Ok(None),
        }
    }
    Ok(top.map(|t| format!("{t}/")))
}

/// 主 exe 识别：根下唯一 exe 直接采用；多个 exe 时取与 zip 同名（去 .zip，忽略大小写）的那个。
/// 返回相对根目录的路径（`/` 分隔）。
pub(crate) fn find_main_exe(dir: &Path, zip_stem: &str) -> Result<std::path::PathBuf, String> {
    let rd = std::fs::read_dir(dir).map_err(|e| format!("读取解压目录失败: {e}"))?;
    let mut exes: Vec<std::path::PathBuf> = Vec::new();
    for e in rd.flatten() {
        let p = e.path();
        if p.is_file()
            && p.extension()
                .map(|x| x.to_string_lossy().eq_ignore_ascii_case("exe"))
                .unwrap_or(false)
        {
            exes.push(e.file_name().into());
        }
    }
    match exes.len() {
        0 => Err("zip 内未找到主程序 exe：请把应用目录内容（含主 exe）打进 zip".to_string()),
        1 => Ok(exes.remove(0)),
        _ => {
            let want = zip_stem.to_ascii_lowercase();
            exes.iter()
                .find(|p| {
                    p.file_stem()
                        .map(|s| s.to_string_lossy().to_ascii_lowercase() == want)
                        .unwrap_or(false)
                })
                .cloned()
                .ok_or_else(|| {
                    format!("zip 根下有多个 exe 且无与包名 {zip_stem} 同名的主程序，无法识别")
                })
        }
    }
}

/// 单文件包判定：解压结果顶层只有 1 个文件、无目录。
pub(crate) fn is_single_file_package(dir: &Path) -> bool {
    let rd = match std::fs::read_dir(dir) {
        Ok(rd) => rd,
        Err(_) => return false,
    };
    let mut files = 0;
    for e in rd.flatten() {
        match e.file_type() {
            Ok(t) if t.is_dir() => return false,
            Ok(_) => files += 1,
            Err(_) => return false,
        }
    }
    files == 1
}

/// 生成分离升级脚本（bat，UTF-8 + `chcp 65001`，行尾 CRLF）：
/// 清理上次残留 → 等宿主退出 → 整目录换血（move 失败退 robocopy）→ 启动新版 → 自删。
/// 与 go/native upgrade_zip.go 的 buildUpgradeScript 输出逐行一致，改动两侧同步。
pub(crate) fn build_upgrade_script(app_dir: &str, staging_dir: &str, exe_rel: &str) -> String {
    let app = app_dir.trim_end_matches('\\');
    let staging = staging_dir.trim_end_matches('\\');
    let exe_path = format!("{app}\\{}", exe_rel.replace('/', "\\"));
    let lines = [
        "@echo off".to_string(),
        "chcp 65001 >nul".to_string(),
        format!(r#"set "APP={app}""#),
        format!(r#"set "NEW={staging}""#),
        format!(r#"set "EXE={exe_path}""#),
        format!(
            r#"for /d %%D in ("{app}.update-*") do (if /i not "%%~fD"=="{staging}" rd /s /q "%%~fD")"#
        ),
        format!(r#"rd /s /q "{app}.old" >nul 2>&1"#),
        "ping -n 3 127.0.0.1 >nul".to_string(),
        "set /a N=0".to_string(),
        ":swap".to_string(),
        r#"move /y "%APP%" "%APP%.old" >nul 2>&1"#.to_string(),
        r#"if not exist "%APP%\" goto :apply"#.to_string(),
        "set /a N+=1".to_string(),
        "if %N% lss 15 (".to_string(),
        "  ping -n 2 127.0.0.1 >nul".to_string(),
        "  goto :swap".to_string(),
        ")".to_string(),
        ":apply".to_string(),
        r#"move /y "%NEW%" "%APP%" >nul 2>&1"#.to_string(),
        r#"if exist "%APP%\" goto :launch"#.to_string(),
        r#"robocopy "%NEW%" "%APP%" /e /purge /r:2 /w:1 >nul"#.to_string(),
        ":launch".to_string(),
        r#"start "" "%EXE%""#.to_string(),
        r#"(goto) 2>nul & del "%~f0""#.to_string(),
    ];
    lines.join("\r\n") + "\r\n"
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    fn write_test_zip(path: &Path, entries: &[(&str, &str)]) {
        let f = std::fs::File::create(path).unwrap();
        let mut w = zip::ZipWriter::new(f);
        for (name, content) in entries {
            if name.ends_with('/') {
                w.add_directory(*name, zip::write::SimpleFileOptions::default()).unwrap();
            } else {
                w.start_file(*name, zip::write::SimpleFileOptions::default()).unwrap();
                w.write_all(content.as_bytes()).unwrap();
            }
        }
        w.finish().unwrap();
    }

    #[test]
    fn sanitize_rejects_traversal_and_absolute() {
        assert_eq!(sanitize_entry_name("a/b/c.txt").unwrap(), "a/b/c.txt");
        assert_eq!(sanitize_entry_name("a\\b.txt").unwrap(), "a/b.txt");
        assert_eq!(sanitize_entry_name("dir/").unwrap(), "dir");
        assert!(sanitize_entry_name("../evil.txt").is_err());
        assert!(sanitize_entry_name("a/../../evil.txt").is_err());
        assert!(sanitize_entry_name("/abs/path.txt").is_err());
        assert!(sanitize_entry_name("C:/evil.txt").is_err());
        assert!(sanitize_entry_name("C:evil.txt").is_err());
        assert!(sanitize_entry_name("a\\..\\..\\evil.txt").is_err());
    }

    #[test]
    fn prefix_detects_single_top_dir() {
        let mk = |v: &[(&str, bool)]| v
            .iter()
            .enumerate()
            .map(|(i, (n, d))| (i, n.to_string(), *d))
            .collect::<Vec<_>>();
        let wrapped = mk(&[("app/app.exe", false), ("app/lib/data.txt", false), ("app/", true)]);
        assert_eq!(common_root_prefix(&wrapped).unwrap(), Some("app/".to_string()));

        let flat = mk(&[("app.exe", false), ("lib/data.txt", false)]);
        assert_eq!(common_root_prefix(&flat).unwrap(), None);

        let multi = mk(&[("a/1.txt", false), ("b/2.txt", false)]);
        assert_eq!(common_root_prefix(&multi).unwrap(), None);

        let root_file = mk(&[("readme.txt", false), ("app/1.txt", false)]);
        assert_eq!(common_root_prefix(&root_file).unwrap(), None);
    }

    #[test]
    fn extract_strips_top_dir_and_keeps_content() {
        let tmp = std::env::temp_dir().join(format!("aohoyo-zip-test-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();
        let zip_path = tmp.join("t.zip");
        write_test_zip(
            &zip_path,
            &[("myapp/app.exe", "MZ"), ("myapp/bin/helper.exe", "HZ"), ("myapp/data.ini", "cfg")],
        );
        let dest = tmp.join("out");
        std::fs::create_dir_all(&dest).unwrap();
        extract_zip(zip_path.to_str().unwrap(), &dest).unwrap();

        assert!(dest.join("app.exe").is_file());
        assert!(dest.join("bin/helper.exe").is_file());
        assert!(dest.join("data.ini").is_file());
        assert!(!dest.join("myapp").exists(), "顶层目录应被剥壳");
        assert_eq!(std::fs::read_to_string(dest.join("app.exe")).unwrap(), "MZ");

        let _ = std::fs::remove_dir_all(&tmp);
    }

    #[test]
    fn find_main_exe_rules() {
        let tmp = std::env::temp_dir().join(format!("aohoyo-exe-test-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();

        // 无 exe
        std::fs::write(tmp.join("readme.txt"), "x").unwrap();
        assert!(find_main_exe(&tmp, "myapp").is_err());
        std::fs::remove_file(tmp.join("readme.txt")).unwrap();

        // 唯一 exe（大小写不敏感扩展名）
        std::fs::write(tmp.join("Main.EXE"), "x").unwrap();
        assert_eq!(find_main_exe(&tmp, "myapp").unwrap(), Path::new("Main.EXE"));
        std::fs::remove_file(tmp.join("Main.EXE")).unwrap();

        // 多 exe：按包名匹配
        std::fs::write(tmp.join("myapp.exe"), "x").unwrap();
        std::fs::write(tmp.join("uninstall.exe"), "x").unwrap();
        assert_eq!(find_main_exe(&tmp, "myapp").unwrap(), Path::new("myapp.exe"));

        // 多 exe 且无同名 → 报错
        assert!(find_main_exe(&tmp, "other").is_err());

        let _ = std::fs::remove_dir_all(&tmp);
    }

    #[test]
    fn single_file_package_detection() {
        let tmp = std::env::temp_dir().join(format!("aohoyo-single-test-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();
        std::fs::write(tmp.join("myapp.exe"), "x").unwrap();
        assert!(is_single_file_package(&tmp));
        std::fs::write(tmp.join("cfg.ini"), "x").unwrap();
        assert!(!is_single_file_package(&tmp));
        std::fs::remove_file(tmp.join("cfg.ini")).unwrap();
        std::fs::create_dir(tmp.join("sub")).unwrap();
        assert!(!is_single_file_package(&tmp));
        let _ = std::fs::remove_dir_all(&tmp);
    }

    #[test]
    fn upgrade_script_contains_key_steps() {
        let s = build_upgrade_script(r"C:\Program Files\MyApp", r"C:\Program Files\MyApp.update-1-2", "myapp.exe");
        assert!(s.contains(r#"set "APP=C:\Program Files\MyApp""#));
        assert!(s.contains(r#"set "NEW=C:\Program Files\MyApp.update-1-2""#));
        assert!(s.contains(r#"set "EXE=C:\Program Files\MyApp\myapp.exe""#));
        assert!(s.contains("chcp 65001"));
        // 清理残留 staging 时排除本次 NEW（bat 文件内循环变量为 %%D 双百分号）
        assert!(s.contains(r#"if /i not "%%~fD"=="C:\Program Files\MyApp.update-1-2""#));
        assert!(s.contains(r#"move /y "%APP%" "%APP%.old""#));
        assert!(s.contains(r#"robocopy "%NEW%" "%APP%" /e /purge /r:2 /w:1"#));
        assert!(s.contains(r#"start "" "%EXE%""#));
        assert!(s.ends_with("\r\n"));
        // 子目录 exe 用反斜杠拼接
        let nested = build_upgrade_script(r"C:\App", r"C:\App.update-1-2", "bin/app.exe");
        assert!(nested.contains(r#"set "EXE=C:\App\bin\app.exe""#));
    }
}
