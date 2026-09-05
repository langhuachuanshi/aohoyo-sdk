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
| `GetSecurityConfig` | 拉取桌面端安全策略（DeviceSign 鉴权） | risk_policy + 各检测项开关 |
| `ReportDeviceWithRisks` | 检测风险并上报设备（devices/report，flags 映射服务端枚举、按策略裁剪） | SEC-1 全流程 |
| `CheckUpgrade` | 升级检测（POST /as/v1/upgrade/check，按平台取该平台最新可用版本） | 版本管理 |
| `VerifyCheckResult` | 检测结果清单签名复算（字节级 manifest，防篡改） | upgrade_signature |
| `DownloadFile` | 安装包下载（断点续传 Range + 进度回调） | 版本管理 |
| `VerifyFile` | 文件哈希校验（SHA256 优先 / MD5） | 版本管理 |
| `Install` | 启动安装器（Windows msi/exe 静默 + zip 解包自替换；Linux deb/rpm/AppImage；macOS pkg），分离进程 | 版本管理 |
| `PerformUpgrade` | 一站式升级：检测 → 验签 → 下载 → 校验 → 安装（OnStage 阶段回调） | 版本管理 |

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
- `ReportDeviceWithRisks` 的 risk_flags 只上报服务端 `risk.Analyze` 接受的枚举
  （debug/emulator/multiopen/root/hook，debug 对应 RiskDebug=6）；检测项按安全策略开关裁剪

## 升级执行链（CheckUpgrade → Install）

完整契约见主仓库 `docs/specs/upgrade-integration.md`。一站式用法：

```go
report, err := sec.PerformUpgrade(native.CheckRequest{
    CurrentVersionCode: currentCode, // 必须与后台同源（如 1.2.3 → 10203）
    Platform:           "windows",   // 强烈建议必传（平台独立节奏）
    DeviceID:           deviceID,    // 参与确定性灰度
}, &native.UpgradeOptions{
    OnStage: func(stage string, received, total int64) { /* checking/downloading/verifying/installing */ },
})
if err != nil { ... }
if report.InstallLaunched {
    os.Exit(0) // 安装器已分离启动，宿主立即退出
}
```

要点：

- `CheckRequest.Platform` 不传回退「全局最新 + 第一个包」（仅兼容旧接入，多平台会拿错包）。
- `CheckResult.DownloadURL` 为空 ≠ 无更新（该平台暂无包），应提示用户。
- `VerifyCheckResult` 用服务端响应**原始字节**（删 signature 尾段）复算 HMAC，跨语言无序列化差异；
  应用未开启 upgrade_signature 时自动跳过。
- `DownloadFile` 断点续传落 `dest+".part"`，续传被拒自动从头重下；下载不完整保留 `.part` 下次续传。
- `Install` 静默参数默认 `/SILENT`（NSIS/Inno 兼容），可用 `InstallOptions.SilentArgs` 覆盖（如 MSI 专用场景）。
- Windows 升级 zip 包：内容 = 应用安装目录完整内容（或单个主 exe），主 exe = zip 根下与包同名 exe
  （否则根下唯一 exe）；单文件包进程内换血（下次启动生效），多文件包分离脚本整目录换血并自动重启。
  （anti_debug=false 等则不上报该项）；策略拉取失败不阻断上报，
  `risk_policy=block` 且有风险时返回 `*RiskBlockedError`
  （上报已完成，宿主 `errors.As` 捕获后自行决定阻断 UI）。

## 平台说明

- Windows：DPAPI 加密、注册表 MachineGuid、IsDebuggerPresent、模块扫描、Global Mutex。
- Linux/macOS：machine-id + DMI 虚拟机检测、TracerPid、flock 文件锁、0600 文件存储（macOS 如需 Keychain 级保护请在宿主侧接入钥匙串）。
