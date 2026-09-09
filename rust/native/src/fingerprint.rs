//! 机器指纹：多信号组合哈希（排序后 SHA256）。

use crate::FingerprintResult;
use sha2::{Digest, Sha256};
use std::collections::HashMap;

fn combine_hash(fields: &HashMap<String, String>) -> String {
    let mut keys: Vec<&String> = fields.keys().collect();
    keys.sort();
    let mut sb = String::new();
    for k in keys {
        sb.push_str(k);
        sb.push('=');
        sb.push_str(&fields[k]);
        sb.push(';');
    }
    let mut hasher = Sha256::new();
    hasher.update(sb.as_bytes());
    hex::encode(hasher.finalize())
}

#[cfg(windows)]
/// 无效序列号黑名单（厂商占位值），命中即视为无此信号。
fn is_valid_serial(v: &str) -> bool {
    let t = v.trim().to_ascii_lowercase();
    !t.is_empty()
        && !matches!(
            t.as_str(),
            "none"
                | "default"
                | "default string"
                | "to be filled by o.e.m."
                | "system serial number"
                | "serial number"
                | "0"
                | "000"
                | "000000000"
        )
}

/// PowerShell 一次调用取主板/BIOS 序列号（零三方依赖；本机调用约 0.5s，指纹仅启动算一次）。
#[cfg(windows)]
fn read_hw_serials() -> (Option<String>, Option<String>) {
    let out = std::process::Command::new("powershell")
        .args(["-NoProfile", "-Command",
               "(Get-CimInstance Win32_BaseBoard).SerialNumber; (Get-CimInstance Win32_BIOS).SerialNumber"])
        .output();
    let stdout = match out {
        Ok(o) if o.status.success() => String::from_utf8_lossy(&o.stdout).to_string(),
        _ => return (None, None),
    };
    let mut lines = stdout.lines().map(|l| l.trim()).filter(|l| !l.is_empty());
    let board = lines.next().map(|s| s.to_string());
    let bios = lines.next().map(|s| s.to_string());
    (
        board.filter(|v| is_valid_serial(v)),
        bios.filter(|v| is_valid_serial(v)),
    )
}

#[cfg(windows)]
pub fn machine_fingerprint() -> Result<FingerprintResult, Box<dyn std::error::Error>> {
    let mut fields = HashMap::new();
    if let Ok(guid) = super::risks::read_machine_guid() {
        if !guid.is_empty() {
            fields.insert("machine_guid".into(), guid);
        }
    }
    if let Ok(h) = std::env::var("COMPUTERNAME") {
        if !h.is_empty() {
            fields.insert("hostname".into(), h);
        }
    }
    // 指纹 v2（2026-09-10）：补硬件级信号。读不到/厂商占位值则跳过（字段集确定性由
    // 「硬件不变 → 读数不变」保证；注意与 go/native fingerprint_windows.go 严格同步）。
    let (board, bios) = read_hw_serials();
    if let Some(b) = board {
        fields.insert("board_serial".into(), b);
    }
    if let Some(b) = bios {
        fields.insert("bios_serial".into(), b);
    }
    let hash = combine_hash(&fields);
    Ok(FingerprintResult { hash, fields })
}

#[cfg(not(windows))]
pub fn machine_fingerprint() -> Result<FingerprintResult, Box<dyn std::error::Error>> {
    use std::path::Path;
    let mut fields = HashMap::new();

    for (key, path) in [
        ("machine_id", "/etc/machine-id"),
        ("product_uuid", "/sys/class/dmi/id/product_uuid"),
    ] {
        if Path::new(path).exists() {
            if let Ok(s) = std::fs::read_to_string(path) {
                let v = s.trim().to_string();
                if !v.is_empty() {
                    fields.insert(key.to_string(), v);
                }
            }
        }
    }
    if fields.is_empty() {
        // macOS 等无 machine-id：退回 hostname
        if let Ok(h) = std::env::var("HOSTNAME").or_else(|_| std::env::var("COMPUTERNAME")) {
            fields.insert("hostname".into(), h);
        }
    }

    let hash = combine_hash(&fields);
    Ok(FingerprintResult { hash, fields })
}
