//! DeviceSign 签名 + 升级清单校验。
//! 与平台 AS `pkg/sign` 同构：signData = deviceID + "\n" + timestamp + "\n" + body。

use crate::SignResult;
use hmac::{Hmac, Mac};
use sha2::Sha256;

type HmacSha256 = Hmac<Sha256>;

fn hmac_sha256_hex(key: &str, data: &str) -> String {
    let mut mac = HmacSha256::new_from_slice(key.as_bytes()).expect("HMAC key");
    mac.update(data.as_bytes());
    hex::encode(mac.finalize().into_bytes())
}

/// 生成一次性 nonce（32 hex 字符）。
fn random_nonce() -> String {
    use rand::RngCore;
    let mut b = [0u8; 16];
    rand::rngs::OsRng.fill_bytes(&mut b);
    hex::encode(b)
}

pub fn sign_request(
    app_secret: &str,
    device_id: &str,
    body: &str,
) -> Result<SignResult, Box<dyn std::error::Error>> {
    let timestamp = std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map_err(|e| e.to_string())?
        .as_secs()
        .to_string();
    let sign_data = format!("{device_id}\n{timestamp}\n{body}");
    let sign = hmac_sha256_hex(app_secret, &sign_data);
    Ok(SignResult {
        sign,
        timestamp,
        nonce: random_nonce(),
    })
}

/// 校验升级清单签名（HMAC(app_secret, manifestJSON)）。
pub fn verify_upgrade(app_secret: &str, manifest_json: &str, signature: &str) -> bool {
    if signature.is_empty() {
        return false;
    }
    let expected = hmac_sha256_hex(app_secret, manifest_json);
    expected == signature
}
