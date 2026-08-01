# @aohoyo/client-sdk

![TypeScript](https://img.shields.io/badge/TypeScript-6-3178c6?logo=typescript)
![MIT License](https://img.shields.io/badge/License-MIT-green)

Aohoyo 前端 SDK，提供认证、设备采集、用户管理、版本检测等能力的统一封装。供终端应用和管理后台共同使用。

## 安装

```bash
npm install @aohoyo/client-sdk
```

> 需要 peer 依赖 `axios >= 1.17`。

本项目通过 `file:../sdk-js` 被 admin-web 本地引用，无需发布到 npm 即可使用。

## 快速使用

```ts
import { createSdk } from '@aohoyo/client-sdk'

const sdk = createSdk({
  baseURL: '/api',
  app_id: 'com.example.app',
  onTokenExpired: () => {
    // Token 过期，跳转登录
    router.push('/login')
  },
})

// 登录（app_id 由 SDK 自动附加）
const res = await sdk.auth.login({
  username: 'admin',
  password: '123456',
})
sdk.client.setTokens(res.access_token, res.refresh_token)

// 登录成功后，会话已按后台「会话模式」配置自动启动：
//   heartbeat → 定时心跳保活；login/open → 计时 + 关闭时上报时长
// 通常无需手动调用 sdk.session.start()。

// 获取当前用户信息
const user = await sdk.user.getProfile()

// 登出
sdk.client.clearTokens()
```

## 邮箱验证码登录 / 注册

邮箱验证码流程与手机号一致，`type` 传 `'email'`，`target` 传邮箱地址：

```ts
// 邮箱注册
await sdk.auth.sendCode({ scene: 'register', type: 'email', target: email, captcha_verify_param })
const res = await sdk.auth.register({ username, password, email, code })
sdk.client.setTokens(res.access_token, res.refresh_token)

// 邮箱验证码登录（未注册自动注册）
await sdk.auth.sendCode({ scene: 'login', type: 'email', target: email, captcha_verify_param })
const res = await sdk.auth.codeLogin({ type: 'email', target: email, code })

// 已登录态：绑定 / 换绑邮箱（需 Token）
await sdk.user.sendProfileCode({ scene: 'bind_email', type: 'email', target: email, captcha_verify_param })
await sdk.user.bindEmail({ email, code })
```

## 邮箱验证

已登录用户可发送验证链接到邮箱，点击链接完成验证（`email_verified` 置 1）：

```ts
// 发送验证链接（链接发到当前用户邮箱）
await sdk.user.sendEmailVerify()
// 用户点击邮件中的链接 → 跳转前端验证页 → 调用：
await sdk.auth.verifyEmail(token) // token 取自链接 query
```

> 验证链接 base 地址由系统配置 `uc:web_base_url` 决定。

## 数据统计

SDK 自带客户端数据上报（PV、会话、活跃、错误等），聚合后在管理后台「数据统计 / 仪表盘」展示。
**基础统计开箱即用**——`createSdk()` 后自动启动，接入方通常无需手动配置。

### 自动上报（无需接入方操作）

| 指标 | 触发时机 | 说明 |
| ---- | -------- | ---- |
| 新增用户 | 用户注册成功 | user-center 注册流程自动写事件，仪表盘「新增用户」据此统计 |
| DAU / 活跃 / 使用次数 | 心跳保活（heartbeat 模式）或 SDK 初始化 | 每日每用户去重，heartbeat 模式应用也有数据 |
| 会话时长 | 页面关闭 / 切到后台 | SDK 自动上报 `session_end`（单位：秒） |
| 用户维度（user_id）| 登录成功 | SDK 自动注入 `user.id`，用户/留存统计才准确 |
| JS 错误 | `window.onerror` / `unhandledrejection` | SDK 自动监听并上报 |

> SDK 在 `deviceId` 就绪后自动调用 `sdk.stats.init()`（仅浏览器环境），启动定时上报与错误监听。

### 可选上报（按需调用）

基础统计自动覆盖。若需要 **PV / 跳出率 / 自定义业务事件**，按需接入：

```ts
// 1. 页面浏览（PV / 跳出率）——接 Vue Router afterEach
router.afterEach((to) => {
  sdk.stats.trackPageView(to.fullPath)
})

// 2. 自定义业务事件（提现、邀请、下单等）
sdk.stats.trackEvent('withdraw', { amount: 100 })
sdk.stats.trackEvent('invite', { invitee_id: 'u_123' })

// 3. 手动上报错误（自动监听之外的捕获）
try {
  riskyOperation()
} catch (e) {
  sdk.stats.trackError(e instanceof Error ? e : String(e))
}
```

| 方法 | 作用 | 不调的影响 |
| ---- | ---- | ---------- |
| `trackPageView(path, title?)` | 上报页面浏览 | PV / 跳出率 / 页面统计缺失 |
| `trackEvent(name, params?)` | 上报自定义事件 | 无自定义业务事件 |
| `trackError(error, stack?)` | 手动上报错误 | 仅自动监听的错误会记录 |

> 事件批量上报（默认每 10s 或满 50 条 flush），页面关闭时用 `sendBeacon` 兜底发送。

## API 列表

### 核心模块（终端应用）

| 模块       | 访问方式          | 功能                         |
| ---------- | ----------------- | ---------------------------- |
| `auth`     | `sdk.auth`        | 登录、登出、刷新 Token       |
| `user`     | `sdk.user`        | 用户信息、注册、修改密码     |
| `device`   | `sdk.device`      | 设备注册、设备信息上报       |
| `session`  | `sdk.session`     | 会话管理                     |
| `oauth`    | `sdk.oauth`       | 第三方登录/绑定              |
| `captcha`  | `sdk.captcha`     | 验证码配置/图片验证码/阿里云滑块初始化 |
| `cloud`    | `sdk.cloud`       | 云变量、云函数               |
| `upgrade`  | `sdk.upgrade`     | 版本检测、升级下载           |
| `stats`    | `sdk.stats`       | 客户端统计数据上报           |

### 管理端模块（admin-web）

| 模块           | 访问方式              | 功能                       |
| -------------- | --------------------- | -------------------------- |
| `adminStats`   | `sdk.adminStats`      | Dashboard/流量/设备/用户统计 |
| `app`          | `sdk.app`             | 应用管理 CRUD              |
| `system`       | `sdk.system`          | 系统配置、菜单、字典        |
| `log`          | `sdk.log`             | 审计日志、身份日志          |
| `cms`          | `sdk.cms`             | 内容管理                   |
| `config`       | `sdk.config`          | 配置管理                   |
| `file`         | `sdk.file`            | 文件上传                   |
| `provider`     | `sdk.provider`        | 服务商配置                 |
| `appResource`  | `sdk.appResource`     | 应用资源管理               |

### SdkClient

`createSdk()` 返回的 `client` 实例提供底层能力：

| 方法 / 属性      | 说明                                       |
| ----------------- | ------------------------------------------ |
| `setTokens(a, r)` | 存储 access/refresh token                  |
| `clearTokens()`   | 清除 token（登出）                         |
| `appId`           | 当前 app_id                                |
| `deviceInfo`      | 设备信息（UA、OS、平台等）                 |
| `deviceId`        | 持久化设备 UUID                            |

## 开发

```bash
npm run build     # TypeScript 编译，输出到 dist/
npm run dev       # 监听模式编译
```

构建产物为 `dist/index.js`（ESM），类型声明为 `dist/index.d.ts`。
