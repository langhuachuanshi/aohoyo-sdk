# Changelog

本文件记录 sdk-js 的所有变更。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

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
