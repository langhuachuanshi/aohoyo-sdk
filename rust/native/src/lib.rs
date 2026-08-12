//! aohoyo 桌面端原生安全模块（Tauri / 任意 Rust 桌面壳）。
//!
//! 与 sdk-js 的 `window.__AOHOYO_NATIVE__` 契约一一对应。
//! `app_secret` 只存在于本模块内，WebView/JS 层永远拿不到。
//!
//! Tauri 集成：在 `tauri::generate_handler!` 中包装本模块方法为 command，
//! 或直接在 Rust 侧调用本模块原语（见 README 示例）。

pub mod fingerprint;
pub mod mutex;
pub mod risks;
pub mod secure;
pub mod sign;
pub mod upgrade;

use serde::Serialize;
use std::sync::Mutex;

/// 机器指纹结果
#[derive(Debug, Clone, Serialize)]
pub struct FingerprintResult {
    pub hash: String,
    pub fields: std::collections::HashMap<String, String>,
}

/// 风险检测结果（debug / emulator / root / hook / multiopen）
#[derive(Debug, Clone, Serialize)]
pub struct RisksResult {
    pub flags: Vec<String>,
}

/// 请求签名结果（DeviceSign 协议）
#[derive(Debug, Clone, Serialize)]
pub struct SignResult {
    pub sign: String,
    pub timestamp: String,
    pub nonce: String,
}

/// exe 完整性结果
#[derive(Debug, Clone, Serialize)]
pub struct IntegrityResult {
    pub hash: String,
    pub ok: bool,
}

/// 桌面端安全原语集合
pub struct Native {
    app_id: String,
    app_secret: String,
    #[allow(dead_code)]
    base_url: String,
    cert_pin: Mutex<Option<String>>,
    mutex: Mutex<mutex::InstanceMutex>,
}

impl Native {
    /// 创建原生模块实例。`app_secret` 仅存于原生层，绝不外泄给 JS。
    pub fn new(
        app_id: impl Into<String>,
        app_secret: impl Into<String>,
        base_url: impl Into<String>,
    ) -> Self {
        Self {
            app_id: app_id.into(),
            app_secret: app_secret.into(),
            base_url: base_url.into(),
            cert_pin: Mutex::new(None),
            mutex: Mutex::new(mutex::InstanceMutex::default()),
        }
    }

    /// 多信号组合机器指纹（防克隆/伪造）。
    pub fn machine_fingerprint(&self) -> Result<FingerprintResult, Box<dyn std::error::Error>> {
        fingerprint::machine_fingerprint()
    }

    /// 风险检测（威慑层，可被绕过；服务端不得仅凭此做封禁决策）。
    pub fn detect_risks(&self) -> Result<RisksResult, Box<dyn std::error::Error>> {
        risks::detect_risks()
    }

    /// DeviceSign 协议签名 + 一次性 nonce。
    pub fn sign_request(
        &self,
        device_id: &str,
        body: &str,
    ) -> Result<SignResult, Box<dyn std::error::Error>> {
        if self.app_secret.is_empty() {
            return Err("app_secret 未配置".into());
        }
        sign::sign_request(&self.app_secret, device_id, body)
    }

    /// 校验升级清单签名（HMAC(app_secret, manifestJSON)）。
    pub fn verify_upgrade(&self, manifest_json: &str, signature: &str) -> bool {
        if self.app_secret.is_empty() {
            return false;
        }
        sign::verify_upgrade(&self.app_secret, manifest_json, signature)
    }

    /// 当前可执行文件 SHA256（完整性自检/基线上报）。
    pub fn self_integrity(&self) -> Result<IntegrityResult, Box<dyn std::error::Error>> {
        let hash = upgrade::executable_hash()?;
        Ok(IntegrityResult {
            hash: hex::encode(hash),
            ok: true,
        })
    }

    /// 防多开互斥：成功获取返回 true；已有实例返回 false。
    pub fn acquire_mutex(&self) -> Result<bool, Box<dyn std::error::Error>> {
        let mut m = self.mutex.lock().map_err(|_| "mutex 状态损坏")?;
        m.acquire(&self.app_id)
    }

    /// 设置 TLS 证书固定指纹（由宿主 HTTP 层消费）。
    pub fn set_cert_pin(&self, pin: impl Into<String>) {
        if let Ok(mut p) = self.cert_pin.lock() {
            *p = Some(pin.into());
        }
    }

    /// 读取证书固定指纹。
    pub fn get_cert_pin(&self) -> Option<String> {
        self.cert_pin.lock().ok().and_then(|p| p.clone())
    }

    /// 从系统安全存储读取（Windows DPAPI / Unix 0600 文件）。
    pub fn secure_get(&self, key: &str) -> Result<String, Box<dyn std::error::Error>> {
        secure::secure_get(key)
    }

    /// 写入系统安全存储。
    pub fn secure_set(&self, key: &str, val: &str) -> Result<(), Box<dyn std::error::Error>> {
        secure::secure_set(key, val)
    }
}
