# aohoyo-native — 桌面端原生安全模块（Rust / Tauri）

给 Tauri（或任意 Rust 桌面壳）提供与平台「应用安全」配套的原生能力。
与 sdk-js 的 `window.__AOHOYO_NATIVE__` 契约一一对应，**app_secret 只存在于本 crate，JS 永远拿不到**。

## 能力

| 方法 | 说明 | 对应平台配置 |
|------|------|-------------|
| `machine_fingerprint` | 机器指纹（Windows: MachineGuid+主机名；Unix: machine-id+DMI UUID） | 一机一码授权绑定 |
| `detect_risks` | debug / emulator / root / hook / multiopen 检测（威慑层，可被绕过） | anti_debug / emulator_detect / root_detect / hook_detect |
| `sign_request` | DeviceSign 协议签名 + 一次性 nonce | app_secret / replay_protection |
| `verify_upgrade` | 升级清单 HMAC 校验 | upgrade_signature |
| `self_integrity` | 当前 exe SHA256 | integrity_check |
| `acquire_mutex` | 防多开互斥（Windows Global Mutex / Unix flock） | anti_multi_open |
| `secure_get/set` | Windows DPAPI / Unix 0600 文件 | refresh_token / 离线凭证 / 激活码存储 |
| `set_cert_pin/get_cert_pin` | TLS 证书固定 pin 存取 | cert_pin |

## 使用（Tauri 集成）

```toml
[dependencies]
aohoyo-native = { path = "../rust/native" }
```

```rust
use aohoyo_native::Native;
use std::sync::Mutex;

// 全局单例（app_secret 从环境变量/配置文件读取，绝不下发到前端）
static NATIVE: Mutex<Option<Native>> = Mutex::new(None);

fn init(app_id: &str, app_secret: &str, base_url: &str) {
    *NATIVE.lock().unwrap() = Some(Native::new(app_id, app_secret, base_url));
}

#[tauri::command]
fn get_machine_fingerprint() -> Result<aohoyo_native::FingerprintResult, String> {
    NATIVE.lock().unwrap().as_ref().ok_or("未初始化")?
        .machine_fingerprint().map_err(|e| e.to_string())
}

// 注册：tauri::generate_handler![get_machine_fingerprint, ...]
```

## 契约说明

- `sign_request(body)` 的 `body` 必须是实际发送的原始字节串（JSON.stringify 后），服务端按
  `HMAC-SHA256(app_secret, deviceID + "\n" + timestamp + "\n" + body)` 复算（与 AS `pkg/sign` 同构）。
- `verify_upgrade(manifest_json, signature)`：`manifest_json` 为升级接口响应 `data` 去掉 `signature` 字段后的 JSON 字符串。
- 风险检测为**威慑层**：可被 patch 绕过，服务端不得仅凭 `risk_flags` 做封禁级决策（见主仓库 `docs/plans/desktop-security-solution.md`）。

## 平台说明

- Windows：DPAPI 加密、注册表 MachineGuid、IsDebuggerPresent、模块扫描、Global Mutex。
- Linux/macOS：machine-id/DMI 虚拟机检测、TracerPid、flock 文件锁、0600 文件存储（macOS 如需 Keychain 级保护请在宿主侧接入钥匙串）。
