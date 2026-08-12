# native — 桌面端原生安全模块（Go / Wails）

给 Wails（或任意 Go 桌面壳）提供与平台「应用安全」配套的原生能力。
与 `sdk-js` 的 `window.__AOHOYO_NATIVE__` 契约一一对应，**app_secret 只存在于本模块，JS 永远拿不到**。

## 能力

| 方法 | 说明 | 对应平台配置 |
|------|------|-------------|
| `GetMachineFingerprint` | 多信号组合机器指纹（Windows: MachineGuid+MAC；Unix: machine-id+hostname+MAC） | 一机一码授权绑定 |
| `DetectRisks` | debug / emulator / root / hook / multiopen 检测（威慑层，可被绕过） | anti_debug / emulator_detect / root_detect / hook_detect |
| `SignRequest` | DeviceSign 协议签名 + 一次性 nonce | app_secret / replay_protection |
| `VerifyUpgrade` | 升级清单 HMAC 校验 | upgrade_signature |
| `SelfIntegrity` | 当前 exe SHA256 | integrity_check |
| `AcquireMutex` | 防多开互斥（Windows Global Mutex / Unix flock） | anti_multi_open |
| `SecureGet/Set` | Windows DPAPI / Unix 0600 文件 | refresh_token / 离线凭证 / 激活码存储 |
| `SetCertPin/GetCertPin` | TLS 证书固定 pin 存取 | cert_pin |

## 使用（Wails 集成）

```go
import "github.com/langhuachuanshi/aohoyo-sdk/go/native"

// 启动时创建并绑定（app_secret 从环境变量/配置文件读取，绝不下发到前端）
sec := native.New(appID, os.Getenv("AOHOYO_APP_SECRET"), "https://api.aohoyo.com")

// Wails v2：
// err := wails.Run(&options.App{ Bind: []interface{}{sec}, ... })
```

前端按 `window.__AOHOYO_NATIVE__` 契约调用：

```ts
// 假设 Wails 绑定名为 Native
const native = window.go.main.Native
const { hash } = await native.GetMachineFingerprint()
const { flags } = await native.DetectRisks()
const { sign, timestamp, nonce } = await native.SignRequest(deviceId, body)
```

## 契约说明

- `SignRequest(body)` 的 `body` 必须是实际发送的原始字节串（JSON.stringify 后），服务端按
  `HMAC-SHA256(app_secret, deviceID + "\n" + timestamp + "\n" + body)` 复算（与 AS `pkg/sign` 同构）。
- `VerifyUpgrade(manifestJSON, signature)`：`manifestJSON` 为升级接口响应 `data` 去掉 `signature` 字段后的 JSON 字符串。
- 风险检测为**威慑层**：可被 patch 绕过，服务端不得仅凭 `risk_flags` 做封禁级决策（见主仓库 `docs/plans/desktop-security-solution.md`）。

## 平台说明

- Windows：DPAPI 加密、注册表 MachineGuid、IsDebuggerPresent、模块扫描、Global Mutex。
- Linux/macOS：machine-id + DMI 虚拟机检测、TracerPid、flock 文件锁、0600 文件存储（macOS 如需 Keychain 级保护请在宿主侧接入钥匙串）。
