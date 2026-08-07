# Changelog

本文件记录 sdk-js 的所有变更。格式遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

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

## [Unreleased]

### 新增

- **storage 模块接入 SDK**：`createSdk()` 返回新增 `sdk.storage`，客户端统一走 `sdk.storage.*`
  - `uploadAvatar(file)` —— 封装 `POST /storage/avatar`（multipart），头像路径由服务端固定 `avatars/`，仅图片、≤2MB
  - `SdkClient` 新增 `upload(url, formData)` 通用 multipart 上传方法
  - 新增导出类型 `StorageModule` / `AvatarUploadResult`
- **user 模块新增修改密码**：`sdk.user.updatePassword(oldPassword, newPassword)` —— 封装 `PUT /user/profile/password`（自助，校验原密码）
- **user 模块新增「绑定/换绑 手机/邮箱」**：均需登录态，对应后端个人中心接口
  - `sendProfileCode({scene, type, target, captcha_*?})` —— 封装 `POST /user/profile/code/send`（登录场景发码：bind_phone / change_phone / bind_email / change_email）
  - `bindPhone({phone, code})` / `changePhone({phone, code})` —— 绑定 / 换绑手机
  - `bindEmail({email, code})` / `changeEmail({email, code})` —— 绑定 / 换绑邮箱
- **auth 注册/验证码登录补发来源字段**：`register` / `codeLogin` 顶层加发 `channel`（= `channel_code`）与 `app_version`（= `current_version_code`），供后端写入用户 `register_env` 的注册来源

### 破坏性变更

- 移除 `sdk.auth.changePassword(userId, password)`：该方法封装的是管理员重置（`PUT /users/:id/password`），本 SDK 面向第三方客户端、不含后台管理能力。找回密码仍用 `sdk.auth.resetPassword`（验证码）

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
