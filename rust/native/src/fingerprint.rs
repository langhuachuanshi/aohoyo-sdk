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
