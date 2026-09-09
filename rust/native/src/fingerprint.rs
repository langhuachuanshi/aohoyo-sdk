//! 机器指纹：多硬件信号组合哈希（排序后 MD5，32 位）。Windows 走注册表/系统 API 直读（毫秒级）。

use crate::FingerprintResult;
use md5::Md5;
use md5::Digest;
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
    // 指纹标识用 MD5（32 位 hex，用户可读性优先；非安全场景，碰撞风险可接受）
    let mut hasher = Md5::new();
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

/// 注册表直读值（RegGetValueW，毫秒级）。键不存在/类型不符返回 None。
#[cfg(windows)]
fn reg_get_string(subkey: &str, value: &str) -> Option<String> {
    use std::os::windows::ffi::OsStrExt;
    use windows_sys::Win32::System::Registry::{RegGetValueW, HKEY, HKEY_LOCAL_MACHINE, KEY_READ, RRF_RT_REG_SZ};
    let sub_w: Vec<u16> = std::ffi::OsStr::new(subkey).encode_wide().chain([0]).collect();
    let val_w: Vec<u16> = std::ffi::OsStr::new(value).encode_wide().chain([0]).collect();
    let mut size: u32 = 0;
    let mut buf: Vec<u16>;
    // 先取长度再取数据
    let r = unsafe {
        RegGetValueW(HKEY_LOCAL_MACHINE as HKEY, sub_w.as_ptr(), val_w.as_ptr(), RRF_RT_REG_SZ,
                     std::ptr::null_mut(), std::ptr::null_mut(), &mut size)
    };
    if r != 0 || size == 0 {
        return None;
    }
    buf = vec![0u16; size as usize / 2 + 1];
    let r = unsafe {
        RegGetValueW(HKEY_LOCAL_MACHINE as HKEY, sub_w.as_ptr(), val_w.as_ptr(), RRF_RT_REG_SZ,
                     std::ptr::null_mut(), buf.as_mut_ptr() as *mut core::ffi::c_void, &mut size)
    };
    if r != 0 {
        return None;
    }
    let end = buf.iter().position(|&c| c == 0).unwrap_or(buf.len());
    let text = String::from_utf16_lossy(&buf[..end]);
    let t = text.trim();
    if t.is_empty() { None } else { Some(t.to_string()) }
}

/// 物理内存字节（GetPhysicallyInstalledSystemMemory，SMBIOS 口径，毫秒级）。
#[cfg(windows)]
fn physical_memory_bytes() -> Option<u64> {
    use windows_sys::Win32::System::SystemInformation::GetPhysicallyInstalledSystemMemory;
    let mut kb: u64 = 0;
    let r = unsafe { GetPhysicallyInstalledSystemMemory(&mut kb) };
    if r != 0 && kb > 0 {
        Some(kb * 1024)
    } else {
        None
    }
}

/// 显卡名列表：枚举显示适配器类子键 0000~0031 的 DriverDesc（毫秒级）。
#[cfg(windows)]
fn read_gpu_names() -> Option<String> {
    let class = r"SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}";
    let mut names: Vec<String> = Vec::new();
    for i in 0..32 {
        if let Some(desc) = reg_get_string(&format!("{class}\\{i:04}"), "DriverDesc") {
            let lower = desc.to_ascii_lowercase();
            if !lower.contains("microsoft basic display") {
                names.push(lower);
            }
        }
    }
    if names.is_empty() { None } else { Some(names.join("|")) }
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
    // 指纹 v4（2026-09-10）：全硬件信号改注册表/系统 API 直读（毫秒级，不再走 PowerShell/WMI）。
    let insert = |fields: &mut HashMap<String, String>, k: &str, v: Option<String>| {
        if let Some(v) = v {
            if is_valid_serial(&v) {
                fields.insert(k.into(), v);
            }
        }
    };
    let bios_key = r"HARDWARE\DESCRIPTION\System\BIOS";
    insert(&mut fields, "board_serial", reg_get_string(&format!("{bios_key}\\BaseBoardSerialNumber"), ""));
    insert(&mut fields, "bios_serial", reg_get_string(&format!("{bios_key}\\SystemSerialNumber"), ""));
    insert(&mut fields, "cpu_identifier", reg_get_string(r"HARDWARE\DESCRIPTION\System\CentralProcessor\0", "Identifier"));
    insert(&mut fields, "gpu_names", read_gpu_names());
    insert(&mut fields, "ram_bytes", physical_memory_bytes().map(|b| b.to_string()));
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
