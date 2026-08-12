//! 安全存储：Windows DPAPI / Unix 0600 文件。

#[cfg(windows)]
pub fn secure_get(key: &str) -> Result<String, Box<dyn std::error::Error>> {
    let blob = std::fs::read_to_string(secure_path(key))?;
    let raw = hex::decode(blob.trim())?;
    let plain = crate::risks::imp::unprotect_data(&raw)?;
    Ok(String::from_utf8_lossy(&plain).into_owned())
}

#[cfg(windows)]
pub fn secure_set(key: &str, val: &str) -> Result<(), Box<dyn std::error::Error>> {
    let enc = crate::risks::imp::protect_data(val.as_bytes())?;
    let path = secure_path(key);
    if let Some(dir) = std::path::Path::new(&path).parent() {
        std::fs::create_dir_all(dir)?;
    }
    std::fs::write(path, hex::encode(enc))?;
    Ok(())
}

#[cfg(windows)]
fn secure_path(key: &str) -> String {
    let base = std::env::var("APPDATA").unwrap_or_else(|_| ".".into());
    format!("{base}/aohoyo/secure/{}", sanitize(key))
}

#[cfg(not(windows))]
pub fn secure_get(key: &str) -> Result<String, Box<dyn std::error::Error>> {
    Ok(std::fs::read_to_string(secure_path(key))?)
}

#[cfg(not(windows))]
pub fn secure_set(key: &str, val: &str) -> Result<(), Box<dyn std::error::Error>> {
    use std::io::Write;
    use std::os::unix::fs::OpenOptionsExt;
    let path = secure_path(key);
    if let Some(dir) = std::path::Path::new(&path).parent() {
        std::fs::create_dir_all(dir)?;
    }
    let mut f = std::fs::OpenOptions::new()
        .create(true)
        .truncate(true)
        .write(true)
        .mode(0o600)
        .open(path)?;
    f.write_all(val.as_bytes())?;
    Ok(())
}

#[cfg(not(windows))]
fn secure_path(key: &str) -> String {
    let home = std::env::var("XDG_CONFIG_HOME").unwrap_or_else(|_| {
        format!(
            "{}/.config",
            std::env::var("HOME").unwrap_or_else(|_| "/tmp".into())
        )
    });
    format!("{home}/aohoyo/secure/{}", sanitize(key))
}

fn sanitize(key: &str) -> String {
    let out: String = key
        .chars()
        .filter(|c| c.is_ascii_alphanumeric() || matches!(c, '_' | '-' | '.'))
        .collect();
    if out.is_empty() {
        "_".into()
    } else {
        out
    }
}
