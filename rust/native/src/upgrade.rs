//! 升级执行链（与 go/native 同构）：检测 → 验签 → 下载 → 校验 → 安装。
//!
//! 平台契约：主仓库 `docs/specs/upgrade-integration.md`。

use crate::{CheckRequest, CheckResult, Native};
use sha2::{Digest, Sha256};

/// exe 完整性自检（保留：启动基线/自检场景）。
pub fn executable_hash() -> Result<Vec<u8>, Box<dyn std::error::Error>> {
    let exe = std::env::current_exe()?;
    let mut f = std::fs::File::open(exe)?;
    let mut hasher = Sha256::new();
    std::io::copy(&mut f, &mut hasher)?;
    Ok(hasher.finalize().to_vec())
}

impl Native {
    /// 调用平台升级检测接口 POST /as/v1/upgrade/check（公开接口）。
    /// Platform 建议必传——服务端按平台取「该平台有包的最新版本」（平台独立节奏）。
    pub fn check_upgrade(&self, req: &CheckRequest) -> Result<CheckResult, Box<dyn std::error::Error>> {
        let body = serde_json::json!({
            "app_id": self.app_id,
            "current_version_code": req.current_version_code,
            "platform": req.platform,
            "channel_code": req.channel_code,
            "device_id": req.device_id,
        });

        let agent = http_agent();
        let body_str = body.to_string();
        let resp = agent
            .post(&format!("{}/as/v1/upgrade/check", self.base_url.trim_end_matches('/')))
            .set("Content-Type", "application/json")
            .send_string(&body_str)?;

        let mut text = String::new();
        use std::io::Read;
        resp.into_reader().take(1 << 20).read_to_string(&mut text)?;

        let v: serde_json::Value = serde_json::from_str(&text)?;
        if v["code"].as_i64() != Some(200) {
            let msg = v["message"].as_str().unwrap_or("unknown");
            return Err(format!("升级检测失败: {msg}").into());
        }

        let mut r: CheckResult = serde_json::from_value(v["data"].clone())?;
        // 字节级提取被签名的 manifest（不经过反序列化重排，与服务端签名字节严格一致）
        r.raw_manifest = extract_manifest(&text).unwrap_or_default();
        Ok(r)
    }

    /// 校验清单签名。应用未开启签名（signature 为空）时直接通过。
    pub fn verify_check_result(&self, r: &CheckResult) -> Result<bool, Box<dyn std::error::Error>> {
        if r.signature.is_empty() {
            return Ok(true);
        }
        if self.app_secret.is_empty() {
            return Err("app_secret 未配置".into());
        }
        Ok(crate::sign::verify_upgrade(
            self.app_secret.as_str(),
            &r.raw_manifest,
            &r.signature,
        ))
    }
}

/// 升级链路共享 HTTP agent（连接超时兜底；下载大文件不走整体超时，靠总量校验）。
pub(crate) fn http_agent() -> ureq::Agent {
    ureq::AgentBuilder::new()
        .timeout_connect(std::time::Duration::from_secs(15))
        .build()
}

/// 从完整响应文本中字节级提取被签名的 manifest：
/// `{"code":200,...,"data":{...}}` → data 原文删掉 `,"signature":"..."` 尾段。
///
/// 依赖服务端响应结构（utils.Response）：data 为外层最后键、CheckResp.signature 为内层最后字段。
/// 字节级操作避免跨语言序列化差异（键序 / escapeHTML）导致验签失败。
pub(crate) fn extract_manifest(text: &str) -> Option<String> {
    let key = ",\"data\":";
    let start = text.rfind(key)? + key.len();
    let t = text.trim_end();
    if !t.ends_with('}') || start >= t.len() {
        return None;
    }
    let inner = &t[start..t.len() - 1]; // 去掉最外层 '}'
    const M: &str = ",\"signature\":\"";
    match inner.rfind(M) {
        Some(i) => Some(format!("{}{}", &inner[..i], "}")),
        None => Some(inner.to_string()),
    }
}

#[cfg(test)]
mod tests {
    use super::extract_manifest;

    #[test]
    fn extract_manifest_strips_signature() {
        let text = r#"{"code":200,"message":"success","data":{"has_update":true,"force_update":false,"latest_version":"2026.9.4","latest_version_code":20260904,"platform":"windows","download_url":"https://x.com/s.exe","file_size":7,"md5":"","sha256":"abc","update_log":"修复 <问题> & 改进","signature":"aabbcc"}}"#;
        let m = extract_manifest(text).unwrap();
        assert!(!m.contains("signature"));
        assert!(m.starts_with('{') && m.ends_with('}'));
        assert!(m.contains(r#""update_log":"修复 <问题> & 改进""#));
        assert!(m.contains(r#""sha256":"abc""#));
    }

    #[test]
    fn extract_manifest_unsigned_keeps_all() {
        let text = r#"{"code":200,"message":"success","data":{"has_update":false,"force_update":false,"latest_version":"","latest_version_code":0,"platform":"","download_url":"","file_size":0,"md5":"","sha256":"","update_log":""}}"#;
        let m = extract_manifest(text).unwrap();
        assert!(m.contains(r#""has_update":false"#));
    }

    #[test]
    fn extract_manifest_rejects_malformed() {
        assert!(extract_manifest("not json").is_none());
    }
}
