//! 机器指纹：多信号组合哈希（排序后 SHA256）。

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

/// PowerShell 一次调用取全部硬件信号（带标签 k=v 输出，空实例输出空值不占行位错乱）。
/// 信号：主板/BIOS 序列号、CPU ProcessorId、显卡名列表（排序 | 拼接）、内存总容量字节。
/// 零三方依赖；本机调用约 1s，指纹仅启动算一次。
#[cfg(windows)]
fn read_hw_signals() -> HashMap<String, String> {
    let mut out_map = HashMap::new();
    let script = "(Get-CimInstance Win32_BaseBoard).SerialNumber;                   (Get-CimInstance Win32_BIOS).SerialNumber;                   (Get-CimInstance Win32_Processor | Select-Object -First 1).ProcessorId;                   (Get-CimInstance Win32_VideoController | Sort-Object Name | ForEach-Object Name) -join '|';                   (Get-CimInstance Win32_PhysicalMemory | Measure-Object -Property Capacity -Sum).Sum";
    let stdout = match std::process::Command::new("powershell")
        .args(["-NoProfile", "-Command", script])
        .output()
    {
        Ok(o) if o.status.success() => String::from_utf8_lossy(&o.stdout).to_string(),
        _ => return out_map,
    };
    // 无标签场景：按行序对应 [board, bios, cpu, gpu, ram]；空实例产生空行，用行序容错
    let rows: Vec<&str> = stdout.lines().map(|l| l.trim()).collect();
    let pick = |i: usize| rows.get(i).copied().unwrap_or("").to_string();
    let insert = |map: &mut HashMap<String, String>, k: &str, v: String| {
        if is_valid_serial(&v) {
            map.insert(k.into(), v);
        }
    };
    insert(&mut out_map, "board_serial", pick(0));
    insert(&mut out_map, "bios_serial", pick(1));
    insert(&mut out_map, "cpu_id", pick(2));
    // 显卡：过滤驱动未装时的软件渲染占位；多卡按名排序拼接（装/换卡 → 指纹变）
    let gpu_line = pick(3);
    let gpu: Vec<&str> = gpu_line
        .split('|')
        .filter(|g| !g.is_empty() && !g.to_ascii_lowercase().contains("microsoft basic display"))
        .collect();
    if !gpu.is_empty() {
        let mut g = gpu.join("|");
        g.make_ascii_lowercase();
        out_map.insert("gpu_names".into(), g);
    }
    // 内存：总容量字节（加/换内存条 → 指纹变）
    if let Ok(bytes) = pick(4).parse::<u64>() {
        if bytes > 0 {
            out_map.insert("ram_bytes".into(), bytes.to_string());
        }
    }
    out_map
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
    // 指纹 v3（2026-09-10）：主板/BIOS/CPU/显卡/内存 全硬件信号。读不到/厂商占位值则跳过
    //（字段集确定性由「硬件配置不变 → 读数不变」保证；与 go/native 严格同步）。
    for (k, v) in read_hw_signals() {
        fields.insert(k, v);
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
