//! aohoyo 桌面端原生安全模块（Tauri / 任意 Rust 桌面壳）。
//!
//! 与 sdk-js 的 `window.__AOHOYO_NATIVE__` 契约一一对应。
//! `app_secret` 只存在于本模块内，WebView/JS 层永远拿不到。
//!
//! Tauri 集成：在 `tauri::generate_handler!` 中包装本模块方法为 command，
//! 或直接在 Rust 侧调用本模块原语（见 README 示例）。

pub mod download;
pub mod fingerprint;
pub mod install;
pub mod mutex;
pub mod risks;
pub mod secure;
pub mod sign;
pub mod upgrade;
mod zip_install;

use serde::{Deserialize, Serialize};
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

/// 升级检测请求。platform 建议必传——服务端按平台取「该平台有包的最新版本」（平台独立节奏）。
#[derive(Debug, Clone, Deserialize)]
pub struct CheckRequest {
    pub current_version_code: i64,
    #[serde(default)]
    pub platform: String, // windows/macos/linux/android/ios
    #[serde(default)]
    pub channel_code: String,
    #[serde(default)]
    pub device_id: String, // 灰度确定性分配依赖此值
}

/// 升级检测结果。字段序与服务端 CheckResp 严格一致（签名相关，勿调整声明顺序）。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CheckResult {
    pub has_update: bool,
    pub force_update: bool,
    pub latest_version: String,
    pub latest_version_code: i64,
    pub platform: String,
    pub download_url: String,
    pub file_size: i64,
    pub md5: String,
    pub sha256: String,
    pub update_log: String,
    /// 镜像解析端点 URL（302 直链或回退主源）；未镜像/服务端未配置为空，下载候选链用
    #[serde(default)]
    pub mirror_download_url: String,
    #[serde(default)]
    pub signature: String,
    /// 服务端响应 data 的原始 JSON（已删 signature 尾段），verify_check_result 用它复算 HMAC
    #[serde(skip)]
    pub raw_manifest: String,
}

/// 安装选项。silent_args 覆盖 exe 安装器默认静默参数（默认 /SILENT，NSIS/Inno 兼容）。
#[derive(Debug, Clone, Default)]
pub struct InstallOptions {
    pub silent_args: Option<String>,
}

/// 一站式升级选项。
pub struct UpgradeOptions {
    pub install: Option<InstallOptions>,
    /// 安装包落地目录，默认系统临时目录
    pub download_dir: Option<String>,
    /// 阶段回调：checking / downloading（received/total 持续回调）/ verifying / installing
    pub on_stage: Option<Box<dyn FnMut(&str, u64, u64)>>,
}

impl Default for UpgradeOptions {
    fn default() -> Self {
        Self { install: None, download_dir: None, on_stage: None }
    }
}

/// 一站式升级结果。install_launched=true 时安装器已启动，宿主应立即退出当前进程。
pub struct UpgradeReport {
    pub result: CheckResult,
    pub installer_path: String,
    pub install_launched: bool,
}

/// 桌面端安全原语集合
pub struct Native {
    app_id: String,
    app_secret: String,
    #[allow(dead_code)]
    base_url: String,
    cert_pin: Mutex<Option<String>>,
    allow_http: std::sync::atomic::AtomicBool,
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
            allow_http: std::sync::atomic::AtomicBool::new(false),
            mutex: Mutex::new(mutex::InstanceMutex::default()),
        }
    }

    /// 是否放行 http 明文下载地址（默认 false，仅本地调试用；生产必须保持 false）。
    /// 语义见 download.rs check_download_url。
    pub fn set_allow_http(&self, allow: bool) {
        self.allow_http
            .store(allow, std::sync::atomic::Ordering::Relaxed);
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
