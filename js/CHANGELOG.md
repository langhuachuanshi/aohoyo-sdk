# Changelog

本文件记录 sdk-js 的所有变更。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

## [Unreleased]

## [0.10.2] - 2026-09-10

### 新增

- **`SdkClient.httpInstance`**: 暴露内部 axios 实例，宿主可挂请求/响应拦截器（升级测试器的
  请求日志窗即基于此）；只读用途，替换实例行为未定义



## [0.10.1] - 2026-09-09

### 新增

- **CheckResponse 新增可选 `mirror_download_url`（UPG-1 镜像契约）**: 镜像解析端点 URL（302 直链或回退主源），未镜像/服务端未配置为空；JS 仅透传，桌面端原生层据此排下载候选链

## [0.10.0] - 2026-09-06

### 新增

- **ads 广告模块（2026-09-06 主仓库 AD 契约）**: `getAds`（拉取在投广告，按广告位 code 分组；登录走
  JWT、免登录自动 DeviceSign——GET 空 body 参与签名）+ `reportImpression` / `reportClick`（曝光/点击上报，
  公开接口自动带 device_id）。类型 `AdItem` / `ClientAdsResponse` / `AdType` 等（`types/ad.ts`）。
  `SdkClient` 底层新增 `getWithHeaders` 支撑签名 GET

### 变更

- **upgrade 模块契约对齐（2026-09-04 主仓库升级链路定稿）**: `UpgradeCheckResponse` 新增可选 `signature`
  字段（仅透传——app_secret 不下发 WebView，验签由宿主原生层 rust `verify_check_result` / go
  `VerifyCheckResult` 完成）；`platform` 参数语义升级并在类型/JSDoc 标注强烈建议必传（服务端按平台取
  「该平台有包的最新版本」，平台独立节奏，不传回退全局最新兼容旧版本）。检测路径确认 /as/v1/upgrade/check

## [0.9.0] - 2026-09-03

### 新增

- **device 模块风险检测接线（SEC-1）**：`device.report` 的 `risk_flags` 优先取桌面原生桥
  `window.__AOHOYO_NATIVE__.DetectRisks()` 结果（映射服务端枚举 debug/emulator/multiopen/root/hook），
  纯浏览器环境保持空数组，桥缺失/调用失败静默降级不影响主流程
- **安全策略下发消费（SEC-1）**：新增 `sdk.device.getSecurityConfig()`（POST /as/v1/app/security/config，
  DeviceSign 鉴权），首次 report 前自动拉取一次并缓存；检测项按策略开关裁剪（如 emulator_detect=false
  则该项不上报、不参与阻断判定）；`risk_policy=block` 且检测到风险时上报完成后触发
  `SdkConfig.onRiskBlocked` 回调（可中断信号，由宿主应用决定阻断 UI）；新增 `sdk.device.detectRisks()`
- **新增类型**：`RiskFlag`、`SecurityConfig`（types/device.ts，统一导出）
- **事件补 app_version（AW-2）**: stats 事件统一携带 `app_version`（取 SDK 初始化的版本号，
  与 device.report 取值对齐）；服务端 stats_events 加列前多出的字段被静默忽略，加列后自然生效
- **session_end 周期 checkpoint（AW-2）**: 会话进行中每 60s 发一条覆盖式 `session_end`
  （同一 session_id、duration=自会话起的累计秒数，后到更大值覆盖先到的），兜底移动端/崩溃/杀进程
  导致的 beforeunload 丢失；页面隐藏时立即落一条 checkpoint、重新可见时对齐周期；
  同一时刻仅一条在途 checkpoint（防 duration 重复计入）；`StatsConfig.checkpointInterval` 可配（0=关闭）
- **换绑两步验证类型（UC-17 配套）**: `changePhone` / `changeEmail` 参数补可选 `old_code`
  （先向当前号/邮箱发码拿到 old_code，再向新号发码，提交时带 old_code + code；
  目标应用启用旧号验证时缺 old_code 会收到「请先验证当前手机号/邮箱」）
- **risk_flags 放行 debug 枚举**: 服务端 `risk.Analyze` 新增 RiskDebug=6，
  原生桥检测到的调试器风险随 devices/report 上报落库；anti_debug 策略关闭则不上报
- **原生桥 Tauri 命名兼容**: 桥探测同时认 `DetectRisks`（Wails/Go，PascalCase）与
  `detect_risks`（Tauri/Rust，snake_case），命中任一即调用——修复 Tauri 应用正确集成
  rust crate 后 risk_flags 仍为空数组的静默失效；两者返回结构均为 `{ flags }`，无字段差异

### 变更

- **错误自动捕获下线（SDK-4）**: 移除 `window error` / `unhandledrejection` 全局监听及上报，
  SDK 不再自动产生 error 事件。`StatsEventType` 的 `error` 仅为协议兼容保留（存量客户端仍可上报，
  服务端通道保留）；`trackError` 标记 @deprecated（手动调用仍可用），`StatsConfig.autoError` 不再生效
  （字段保留仅为避免接入方编译破坏）。批量上报请求结构不变

## [0.8.0] - 2026-08-10

### 破坏性变更（Breaking）

- **路由前缀重构**: 对齐后端服务前缀架构，所有接口路径改为服务前缀 + 版本号
  - UC 接口（auth/profile/menus/oauth/session）: `/v1/*` → `/uc/v1/*`
  - AS 接口（captcha/device/feedback/storage/stats/upgrade）: `/v1/*` → `/as/v1/*`
  - `baseURL` 不再需要带 `/api`，直接用域名或空即可
- **删除 cloud 模块**: 云变量/云函数模块已下线（服务端路由早已移除），移除 `sdk.cloud` 及 `createCloudModule` 导出

### 修复

- **captcha create 路径缺前缀**: `/captcha/create` → `/as/v1/captcha/create`
- **upgrade strategy 路径前缀错误**: `/app/upgrade/strategy/*` → `/as/v1/upgrade/strategy/*`

### 升级指南

```diff
- const sdk = createSdk({ baseURL: '/api', app_id })
+ const sdk = createSdk({ baseURL: '', app_id })  // 或直接域名
```

## [0.7.1] - 2026-08-09

### 修复

- **`username_updated_at` 改为可空**：类型从 `string` 改为 `string \| null`，从未改过 username 时后端返回 `null`（而非回退为 `created_at`），新注册用户可立即修改 username

---

## [0.7.0] - 2026-08-09

### 新增

- **user 模块新增 `updateUsername`**：`sdk.user.updateUsername(username)` 对应 `PUT /v1/profile/username`，独立于 `updateProfile`（username 有冷却期和唯一性约束，不能走通用资料修改）
- **UserInfoResponse 新增字段**：`user.username_updated_at`（最近修改时间，从未改过则为 created_at）+ 顶层 `username_cooldown_days`（冷却天数），前端可通过 `getUserInfo()` 直接获取并判断是否允许修改

---

## [0.6.0] - 2026-08-08

### 新增

- **feedback 模块**：`sdk.feedback` 支持反馈提交、图片上传、我的反馈列表
  - `uploadImage(file)` —— 上传反馈图片（JWT 或 DeviceSign HMAC 双认证，multipart）
  - `submit(params)` —— 提交反馈（JWT 或 DeviceSign HMAC，自动上传 File 对象）
  - `myList(params)` —— 查询我的反馈列表（仅 JWT 登录用户）
  - 新增类型 `FeedbackSubmitParams` / `FeedbackSubmitResponse` / `FeedbackItem` / `FeedbackMyListParams` / `FeedbackMyListResponse` / `UploadImageResponse`

---

## [0.5.2] - 2026-07-01

### 变更

- **统计自动初始化**：Web 环境下 `client.ready` 后自动调用 `stats.init()`，接入方无需手动调用
- **登录自动注入 userId**：`onLoginSuccess` 回调自动写入 `client.userId`，统计事件自动带上用户标识
- **userId 生命周期绑定登出**：`clearTokens()` 清除 token 时一并清空 `userId`，避免登出后统计事件仍带旧用户
- `sessionId` / `userId` 提升到 `SdkClient` 共享层，stats/session 模块统一引用
- `storage.uploadAvatar` 注释更新：后端 `/storage/avatar` 已要求登录态（AS-20），SDK 行为不变（已登录态自动带 token）；未登录调用将返回 401

## [0.5.1] - 2026-06-28

### 变更

- **SDK 内部清理（移除死代码 / 减少冗余）**，公共 API 行为不变：
  - 移除 `SdkClient.getRefreshToken()`（无任何调用方，内部刷新直接读存储）
  - 移除 `getCachedDeviceId()` 及其顶层导出（同上，模块内未使用）
  - 移除 `captcha.isSDKLoaded()`（`loadSDK()` 内部已去重判断）
  - 移除 `session.startHeartbeat()` / `session.stopHeartbeat()` 向后兼容别名（无调用方，统一用 `start()` / `stop()`）
  - `stats` 模块删除重复的 `parseOS`，复用 `client.deviceInfo.os_name`
  - `session` / `stats` 的 sendBeacon 拼接由脆弱的 `(client as any).http.defaults.baseURL` 改为公开的 `client.baseURL` getter（`SdkClient` 新增 `baseURL` 只读属性）
  - `auth` 模块抽取 `register` / `codeLogin` 共用的来源字段，消除重复的版本号格式化

---

## [0.5.0] - 2026-06-15

### 新增

- **captcha 模块接入 SDK**：`createSdk()` 返回新增 `sdk.captcha`，客户端统一走 `sdk.captcha.*`，不再自己 fetch
  - `getConfig(scene)` —— 封装 `GET /captcha/config?scene=`，返回 `{ mode, prefix?, scene_id?, region?, encrypted_scene_id? }`
  - `createImage()` —— 封装 `POST /captcha/create`（mode=image 回退），返回 `{ captcha_id, captcha_base64 }`
  - 新增导出类型 `CaptchaScene` / `CaptchaConfig` / `ImageCaptcha`

### 破坏性变更

- `createCaptchaModule` 改为必填 `client` 参数（`createCaptchaModule(client)`），与 auth/user 等模块一致；独立使用需传入 `sdk.client`

---

## [0.4.1] - 2026-06-15

### 新增

- **sendCode**：增加可选参数 `captcha_verify_param` / `captcha_id` / `captcha_code`。发送验证码前由 user-center 校验服务商验证码（图形/行为），调用方需先完成人机验证再将凭证传入。向后兼容（参数可选）。

---

## [0.4.0] - 2026-06-14

### 修复

- **sendCode**：路径由错误的 `/user/auth/register/code`（404）改为后端实际的 `/user/auth/code/send`；参数由 `{ phone?, email? }` 改为统一验证码服务的 `{ scene, type, target }`。
- **resetPassword**：路径由错误的 `/user/auth/reset-password`（404）改为 `/user/auth/password/reset`；参数由 `{ phone?, email?, code, password }` 改为 `{ type, target, code, password }`。
- **LoginResponse.user**：收窄为后端实际返回的 5 个字段 `{ id, username, user_code, nickname, avatar }`（原先错误地包含了 email/phone/gender/birthday/bio/status 等后端登录接口从不返回的字段）。完整用户信息请通过 `getUserInfo()` 获取。
- **session**：移除 `trackOpen()`，open 模式合并到 login（会话生命周期）；open 模式的使用次数/日活/会话数改由 stats 模块经公开端点 `/stats/events` 上报 session_start/session_end，避免双模块重复计数

### 新增

- **codeLogin**：新增验证码登录方法 `codeLogin({ type, target, code })`，对应后端 `/user/auth/login/code`（未注册自动注册），自动附加 app_id 与设备信息，返回 `LoginResponse`（与 login/register 一致）。
- **LoginUser**：新增类型，表示登录/注册/验证码登录返回的精简用户信息。
