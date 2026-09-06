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
| `check_upgrade` | 升级检测（POST /as/v1/upgrade/check，按平台取该平台最新可用版本） | 版本管理 |
| `verify_check_result` | 检测结果清单签名复算（字节级 manifest，防篡改） | upgrade_signature |
| `download_file` | 安装包下载（断点续传 Range + 进度回调）。**仅接受 https 地址**，`set_allow_http(true)` 才放行 http（本地调试） | 版本管理 |
| `verify_file` | 文件哈希校验（SHA256 优先 / MD5） | 版本管理 |
| `install` | 启动安装器（Windows msi/exe 静默 + zip 解包自替换；Linux deb/rpm/AppImage；macOS pkg），分离进程 | 版本管理 |
| `perform_upgrade` | 一站式升级：检测 → 验签 → 下载 → 校验 → 安装（on_stage 阶段回调） | 版本管理 |

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
- Windows 升级 zip 包：内容 = 应用安装目录完整内容（或单个主 exe），主 exe = zip 根下与包同名 exe
  （否则根下唯一 exe）；单文件包进程内换血（下次启动生效），多文件包分离脚本整目录换血并自动重启。
- Linux/macOS：machine-id/DMI 虚拟机检测、TracerPid、flock 文件锁、0600 文件存储（macOS 如需 Keychain 级保护请在宿主侧接入钥匙串）。

## 升级执行链（check_upgrade → install）

完整契约见主仓库 `docs/specs/upgrade-integration.md`。一站式用法（Tauri command 示例）：

```rust
use aohoyo_native::{CheckRequest, Native, UpgradeOptions};

#[tauri::command]
fn perform_upgrade(native: tauri::State<'_, Native>) -> Result<String, String> {
    let report = native
        .perform_upgrade(
            &CheckRequest {
                current_version_code: 10203,          // 必须与后台同源（1.2.3 → 10203）
                platform: "windows".into(),           // 强烈建议必传（平台独立节奏）
                channel_code: "official".into(),
                device_id: device_id_or_empty(),      // 参与确定性灰度
            },
            UpgradeOptions {
                on_stage: Some(Box::new(|stage, recv, total| {
                    // checking / downloading(recv,total) / verifying / installing
                })),
                ..Default::default()
            },
        )
        .map_err(|e| e.to_string())?;
    if report.install_launched {
        // 安装器已分离启动——宿主应尽快退出当前进程
    }
    Ok(report.installer_path)
}
```

要点：

- `platform` 不传回退「全局最新 + 第一个包」（仅兼容旧接入，多平台会拿错包）。
- `CheckResult.download_url` 为空 ≠ 无更新（该平台暂无包），应提示用户。
- `verify_check_result` 用服务端响应**原始字节**（删 signature 尾段）复算 HMAC，跨语言无序列化差异；
  应用未开启 upgrade_signature 时自动跳过。
- `download_file` 断点续传落 `dest.part`，续传被拒自动从头重下；下载不完整保留 `.part` 下次续传。
- **下载地址安全门**：非 `https://` 的安装包地址默认拒绝（防清单篡改导向明文源），`set_allow_http(true)`
  显式放开 http 仅供本地调试；`file:`/`ftp:` 等其他 scheme 任何情况都拒绝。
- `install` 静默参数默认 `/SILENT`（NSIS/Inno 兼容），`InstallOptions.silent_args` 可覆盖。
