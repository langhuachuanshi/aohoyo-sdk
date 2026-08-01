# @aohoyo/client-sdk 知识库

## 项目定位

客户端 JS SDK（npm 包 `@aohoyo/client-sdk` v0.5.2），供 **Web / 桌面端（Tauri/Wails/Electron）/ 移动端** 集成使用。封装用户中心（UC）和管理服务（AS）的全部客户端 API，通过 JWT Bearer Token 认证。

与 [`sdk/server-go`](../server-go/CLAUDE.md) 区别：client-js 是**客户端 SDK**（JWT），server-go 是**服务端 SDK**（S2S HMAC 签名）。

---

## 技术栈

| 项 | 值 |
|----|-----|
| 语言 | TypeScript 6 |
| 编译目标 | ES2020，输出 `dist/` |
| 运行时依赖 | **零**（axios 为 peerDependency，由接入方安装） |
| 包名 | `@aohoyo/client-sdk` |
| 发布 | 打 tag `client-js/v*` → GitHub Actions 自动发布 npm |

---

## 架构

```
src/
├── index.ts          ← createSdk() 工厂入口，组装全部模块
├── client.ts         ← HTTP 客户端（axios + JWT 拦截 + 401 自动刷新 + 设备指纹采集）
├── device.ts         ← 设备指纹生成（浏览器多维度指纹 / 原生注入兜底）
├── types/            ← 全部 TypeScript 类型定义
│   ├── index.ts      ← 统一导出
│   ├── auth.ts       ← LoginResponse, RegisterParams, SessionMode 等
│   ├── user.ts       ← UserInfoResponse, MenuItem, PasswordPolicy
│   ├── upgrade.ts    ← UpgradeCheckResponse, UpgradeStrategy, AutoCheckConfig
│   ├── device.ts     ← DeviceInfo, NativeDeviceInfo, DeviceVerifyResponse
│   ├── oauth.ts      ← OAuthProvider, OAuthCallbackResult 等
│   ├── stats.ts      ← StatsEvent, StatsConfig
│   └── captcha.ts    ← AliyunCaptchaInitConfig, AliyunCaptchaCallbacks
└── modules/          ← 功能模块（每个 export create*Module 工厂函数）
    ├── auth.ts       ← 登录/注册/验证码/密码重置
    ├── user.ts       ← 用户信息/资料/密码/手机邮箱绑定/菜单
    ├── session.ts    ← 会话管理（heartbeat/login/open 三种模式）
    ├── device.ts     ← 设备上报/验证（HMAC-SHA256 签名）
    ├── upgrade.ts    ← 版本升级检测/自动轮询
    ├── oauth.ts      ← 第三方登录（OAuth）
    ├── stats.ts      ← 统计埋点（session/page_view/error/custom）
    ├── captcha.ts    ← 验证码（阿里云滑块 + 图片验证码）
    ├── storage.ts    ← 文件存储（头像上传）
    └── cloud.ts      ← ⚠️ 云变量/云函数（已下线，保留兼容）
```

### 模块 API 前缀归属

| 模块 | API 前缀 | 归属服务 |
|------|----------|----------|
| auth / user / oauth / session | `/api/user/*` | UC（用户中心） |
| captcha / stats / storage | `/api/v1/captcha/*` `/api/v1/stats/*` `/api/v1/storage/*` | AS |
| upgrade / device | `/api/v1/upgrade/*` `/api/v1/devices/*` | AS |
| notifications | `/api/v1/notifications/*` | AS |
| upgrade / device | `/api/app/*` | AS（管理服务） |

---

## 快速开始

```ts
import { createSdk } from '@aohoyo/client-sdk'

const sdk = createSdk({
  baseURL: 'https://api.example.com',
  app_id: 'your_app_id',
  app_secret: 'your_app_secret',        // device 模块签名需要
  channel_code: 'official',             // 渠道代码
  current_version_code: 10203,          // 当前版本号（upgrade 使用）
  platform: 'windows',                  // 当前平台（upgrade 使用）
  onTokenExpired: () => router.push('/login'),
})

// 登录
const res = await sdk.auth.login({ username: 'test', password: '123456' })
sdk.client.setTokens(res.access_token, res.refresh_token)
// 会话已自动启动（按后台下发的 session_mode）

// 版本检测
const upgrade = await sdk.upgrade.checkUpgrade()
if (upgrade.has_update) {
  if (upgrade.force_update) {
    // 强制更新 — 阻断式弹窗
  } else {
    // 普通更新 — 提示用户
  }
  // upgrade.download_url / upgrade.md5 / upgrade.sha256
}

// 统计埋点
sdk.stats.trackPageView('/home')
sdk.stats.trackEvent('button_click', { button: 'buy' })
```

---

## 完整 API 参考

### 1. auth 模块 — 认证

| 函数 | 签名 | 说明 |
|------|------|------|
| `login` | `(params: { username, password }) => Promise<LoginResponse>` | 账号密码登录，自动附加 app_id 和设备信息 |
| `register` | `(params: RegisterParams) => Promise<LoginResponse>` | 用户注册（手机号/邮箱二选一），需先 sendCode |
| `codeLogin` | `(params: { type, target, code }) => Promise<LoginResponse>` | 验证码登录，未注册自动注册 |
| `logout` | `() => Promise<void>` | 退出登录，自动清除 Token |
| `refreshToken` | `(refresh_token: string) => Promise<RefreshResponse>` | 手动刷新 Token |
| `sendCode` | `(data: { scene, type, target, ... }) => Promise<void>` | 发送验证码（注册/登录/找回密码），需先过人机验证 |
| `resetPassword` | `(data: { type, target, code, password }) => Promise<void>` | 未登录状态下重置密码 |
| `verifyCode` | `(data: { scene, type, target, code }) => Promise<void>` | 仅校验验证码（不消费），用于两步流程第一步 |
| `verifyEmail` | `(token: string) => Promise<void>` | 校验邮箱验证链接 token（公开接口） |

**LoginResponse 关键字段：**
- `access_token` / `refresh_token` — JWT 令牌对
- `user` — 用户信息（id, username, email, phone, has_password, role 等）
- `session_mode` — 后台下发的会话模式（`heartbeat` / `login` / `open`）

### 2. user 模块 — 用户

| 函数 | 签名 | 说明 |
|------|------|------|
| `getUserInfo` | `() => Promise<UserInfoResponse>` | 获取当前用户信息（含角色/权限），每次请求不缓存 |
| `updateProfile` | `(data: Record<string, any>) => Promise<void>` | 修改个人资料 |
| `updatePassword` | `(params: { newPassword, oldPassword? }) => Promise<void>` | 修改密码，首次设置 oldPassword 可为空 |
| `getPasswordPolicy` | `() => Promise<PasswordPolicy>` | 获取密码策略（公开接口），用于前端预校验 |
| `sendProfileCode` | `(data: { scene, type, target, ... }) => Promise<void>` | 发送绑定/换绑验证码（需登录态） |
| `bindPhone` | `(data: { phone, code }) => Promise<void>` | 绑定手机（当前无手机时） |
| `changePhone` | `(data: { phone, code }) => Promise<void>` | 换绑手机（当前已有手机时） |
| `bindEmail` | `(data: { email, code }) => Promise<void>` | 绑定邮箱（当前无邮箱时） |
| `changeEmail` | `(data: { email, code }) => Promise<void>` | 换绑邮箱（当前已有邮箱时） |
| `sendEmailVerify` | `() => Promise<void>` | 发送邮箱验证码到当前用户邮箱 |
| `verifyEmail` | `(code: string) => Promise<void>` | 校验邮箱验证码，成功后 email_verified 置 1 |
| `getMenuTree` | `(appId?: string) => Promise<MenuItem[]>` | 获取当前用户菜单树（可按应用过滤） |

### 3. session 模块 — 会话

三种会话模式，由后台下发 `session_mode` 控制：

| 模式 | 行为 | 适用场景 |
|------|------|----------|
| `heartbeat` | 定时心跳保活（1~5分钟随机间隔），页面关闭不额外上报 | 需要实时在线状态的应用 |
| `login` | 记录会话开始，页面关闭/隐藏时上报 `session_end`（含 duration） | 按登录次数统计的应用 |
| `open` | 同 login，但打开即计（不要求登录） | 按打开次数统计的应用 |

| 函数 | 说明 |
|------|------|
| `start()` | 根据 sessionMode 启动对应行为（登录后/SDK初始化时自动调用） |
| `stop()` | 停止心跳（登出时调用） |
| `end()` | 上报会话结束并清理（主动退出时调用） |

**注意：** session_id 由 stats 模块统一生成（`client.sessionId`），session 模块复用同一个 ID，保证 page_view 和 session_end 口径一致。

### 4. device 模块 — 设备

| 函数 | 签名 | 说明 |
|------|------|------|
| `report` | `(params?: { user_id?: number }) => Promise<void>` | 设备上报，使用 HMAC-SHA256 签名（需要 app_secret） |
| `verify` | `() => Promise<DeviceVerifyResponse>` | 设备验证，同样需要签名 |

**签名机制：** `HMAC-SHA256(app_secret, "device_id\ntimestamp\nbody")`，通过 `X-App-ID` / `X-Device-Sign` / `X-Device-ID` / `X-Timestamp` 请求头发送。

### 5. upgrade 模块 — 版本升级

| 函数 | 签名 | 说明 |
|------|------|------|
| `checkUpgrade` | `() => Promise<UpgradeCheckResponse>` | 单次版本检测（自动带 app_id / version_code / platform / channel_code / device_id） |
| `getStrategy` | `() => Promise<UpgradeStrategy \| null>` | 获取应用最新已发布版本（不受灰度限制，用于调试/预览） |
| `startAutoCheck` | `(config?: { intervalMs?, onUpdate?, onForceUpdate? }) => void` | 启动定时自动检测，默认间隔 30 分钟 ± 1 分钟随机抖动 |
| `stopAutoCheck` | `() => void` | 停止自动检测 |

**UpgradeCheckResponse 关键字段：**
```ts
{
  has_update: boolean       // 是否有新版本
  force_update: boolean     // 是否强制更新
  latest_version: string    // 最新版本号（如 "1.3.0"）
  latest_version_code: number // 最新版本 code
  platform: string          // 平台
  download_url: string      // 下载地址
  file_size: number         // 文件大小（字节）
  md5: string               // MD5 校验值
  sha256: string            // SHA256 校验值
  update_log: string        // 更新日志
}
```

### 6. oauth 模块 — 第三方登录

| 函数 | 签名 | 说明 |
|------|------|------|
| `getProviders` | `() => Promise<OAuthProvider[]>` | 获取已启用的第三方登录渠道列表 |
| `getAuthURL` | `(provider: string) => Promise<OAuthAuthURL>` | 获取第三方授权跳转 URL |
| `callback` | `(provider, code, state?) => Promise<OAuthCallbackResult>` | 授权回调（已绑定自动登录，未绑定返回临时信息） |
| `bind` | `(params: OAuthBindParams) => Promise<void>` | 绑定第三方账号到当前用户 |
| `unbind` | `(provider: string) => Promise<void>` | 解绑第三方账号 |
| `getBindings` | `() => Promise<OAuthBinding[]>` | 查询当前用户已绑定的三方账号 |

### 7. stats 模块 — 统计埋点

| 函数 | 签名 | 说明 |
|------|------|------|
| `init` | `() => void` | 初始化：采集设备信息、启动 session_start、定时上报（10s）、全局错误监听 |
| `destroy` | `() => void` | 销毁：结束会话、清除定时器、移除事件监听 |
| `trackPageView` | `(path: string, title?: string) => void` | 上报页面浏览事件 |
| `trackEvent` | `(name: string, params?: Record<string, any>) => void` | 上报自定义事件 |
| `trackError` | `(error: Error \| string, stack?: string) => void` | 上报错误事件 |
| `setUserId` | `(id: string) => void` | 设置用户 ID（登录后） |
| `clearUserId` | `() => void` | 清除用户 ID（登出时） |

**自动采集：**
- 定时批量上报（默认 10s 间隔，50 条/批）
- JS 运行时错误（`window.error` + `unhandledrejection`）
- 页面隐藏时 flush，关闭前 sendBeacon 同步发送
- session_start / session_end 自动管理

### 8. captcha 模块 — 验证码

| 函数 | 签名 | 说明 |
|------|------|------|
| `loadSDK` | `() => Promise<void>` | 加载阿里云验证码 JS SDK（CDN，单例去重） |
| `initAliyun` | `(config, callbacks) => Promise<void>` | 初始化阿里云滑块验证码 |
| `cleanup` | `() => void` | 清理全局配置（组件销毁时） |
| `getConfig` | `(scene: CaptchaScene) => Promise<CaptchaConfig>` | 取验证码前端配置（阿里云/图片回退） |
| `createImage` | `(opts?: CreateImageOptions) => Promise<ImageCaptcha>` | 获取图片验证码（支持 char/math 模式） |
| `verifyImage` | `(captcha_id, code) => Promise<void>` | 校验图片验证码（一次性，失败达上限锁定） |

### 9. storage 模块 — 存储

| 函数 | 签名 | 说明 |
|------|------|------|
| `uploadAvatar` | `(file: File \| Blob) => Promise<AvatarUploadResult>` | 上传头像（需登录态，图片 ≤2MB） |

### 10. cloud 模块 — 云变量/云函数 ⚠️ 已下线

> 服务端路由已移除，云变量/云函数管理改走 admin-web 后台（`/api/v1/admin/cloud-vars`、`/api/v1/admin/cloud-funcs`）。
> 保留仅为兼容性，待确认无下游引用后删除。

| 函数 | 说明 |
|------|------|
| `getVars(scopeKey?)` | 获取所有可读云变量 |
| `getVar(key, scopeKey?)` | 获取单个云变量 |
| `setVar(key, value, scopeKey)` | 写入云变量值 |
| `call<T>(funcName, params?, scopeKey?)` | 调用云函数 |

---

## SdkClient 核心类

### 构造函数配置 `SdkConfig`

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `baseURL` | `string` | ✅ | API 基础地址 |
| `app_id` | `string` | ✅ | 应用 ID |
| `app_secret` | `string` | ❌ | 应用密钥（device 模块签名需要） |
| `channel_code` | `string` | ❌ | 渠道代码（华为/小米等） |
| `current_version_code` | `number` | ❌ | 当前版本号（upgrade 使用） |
| `platform` | `string` | ❌ | 当前平台（android/ios/windows/macos/linux） |
| `onTokenExpired` | `() => void` | ❌ | Token 过期回调 |
| `storage` | `{ getItem, setItem, removeItem }` | ❌ | 自定义存储（默认 localStorage） |
| `timeout` | `number` | ❌ | 请求超时（ms） |
| `session_mode` | `SessionMode` | ❌ | 会话模式，默认 `heartbeat` |

### HTTP 方法

| 方法 | 说明 |
|------|------|
| `get<T>(url, params?)` | GET 请求，自动解包 `{ code, data }` |
| `post<T>(url, data?)` | POST 请求 |
| `put<T>(url, data?)` | PUT 请求 |
| `delete<T>(url, params?)` | DELETE 请求 |
| `postWithHeaders<T>(url, data, headers)` | POST 带自定义请求头（device 签名使用） |
| `upload<T>(url, formData)` | 文件上传（multipart/form-data） |

### Token 管理

| 方法/属性 | 说明 |
|------|------|
| `setTokens(access, refresh)` | 设置 Token 对，持久化到 storage |
| `clearTokens()` | 清除 Token + userId（登出时调用） |
| `getAccessToken()` | 获取当前 access_token（未登录返回 null） |
| `isLoggedIn` | 是否已登录（getter） |

### 配置访问器

| getter | 来源 |
|------|------|
| `appId` | `config.app_id` |
| `baseURL` | `config.baseURL` |
| `appSecret` | `config.app_secret` |
| `channelCode` | `config.channel_code` |
| `versionCode` | `config.current_version_code` |
| `platform` | `config.platform` |
| `sessionMode` | `config.session_mode`，默认 `heartbeat` |

### 实例属性

| 属性 | 类型 | 说明 |
|------|------|------|
| `deviceInfo` | `DeviceInfo` | 设备信息缓存（构造时采集一次） |
| `deviceId` | `string` | 设备码（硬件指纹），异步就绪 |
| `ready` | `Promise<void>` | 设备码初始化完成的 Promise |
| `sessionId` | `string` | 统一会话 ID（stats 生成，session 复用） |
| `userId` | `string` | 当前登录用户 ID（登录写入，登出清空） |

### 内置能力

- **401 自动刷新**：响应拦截器检测到 401 → 自动用 refresh_token 换新 token → 重放原请求
- **防并发刷新**：多个请求同时 401 时只发一次刷新请求
- **设备指纹**：Web 端用 Canvas/WebGL/AudioContext/字体探测等多维度生成 SHA256 指纹；原生端从 `window.__NATIVE_DEVICE_INFO__` 读取
- **统一响应解包**：自动从 `{ code, data, message }` 中提取 `data`

---

## 桌面端集成要点

当在 Tauri / Wails / Electron 等桌面端集成时：

1. **Native 设备信息注入**：在 `window.__NATIVE_DEVICE_INFO__` 注入：
   ```ts
   window.__NATIVE_DEVICE_INFO__ = {
     device_id: 'desktop_unique_id',  // 硬件唯一标识
     platform: 'windows',             // 当前平台
     os_name: 'Windows',
     os_version: '10/11',
     os_language: 'zh-CN',
     os_theme: 'dark',
     vendor: '',
     model: '',
   }
   ```

2. **版本升级流程**：
   ```
   启动 App → sdk.upgrade.startAutoCheck({
     onUpdate: (resp) => { /* 提示用户，展示 update_log + download_url */ },
     onForceUpdate: (resp) => { /* 阻断式弹窗，必须更新 */ },
   })
   → 用户确认 → 下载 .exe/.dmg/.deb（用系统原生下载能力，支持断点续传）
   → MD5/SHA256 校验 → 静默安装或提示用户安装
   ```

3. **Token 存储**：桌面端应传入自定义 `storage` 实现（如 Tauri 的 `tauri-plugin-store`），避免 WebView localStorage 被清除。

4. **退出前上报**：桌面端关闭时应调用 `sdk.session.end()` 确保会话时长被记录。

---

## 发布流程

1. 更新 `package.json` version
2. 更新 `CHANGELOG.md`
3. `npm run build` 确保编译通过
4. 打 tag `client-js/v{version}` 并推送
5. GitHub Actions 自动发布到 npm

---

## 开发约定

- **axios** 为 peerDependency，由接入方安装；除此零运行时依赖
- 所有公共 API 必须有完整 TypeScript 类型
- 公共方法签名变更 = major 版本，优先加新方法不改旧的
- 新模块使用 `create*Module(client: SdkClient)` 工厂函数模式
- 错误处理：网络错误静默忽略（stats/upgrade/session），认证错误走拦截器
