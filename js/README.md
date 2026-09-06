# @aohoyo/client-sdk

![TypeScript](https://img.shields.io/badge/TypeScript-6-3178c6?logo=typescript)
![MIT License](https://img.shields.io/badge/License-MIT-green)

Aohoyo 前端 SDK，封装用户中心（UC 认证/资料/菜单）与管理服务（AS 设备/统计/升级/验证码/反馈）的客户端 API。供 **Web / 桌面端（Tauri/Wails/Electron）/ 移动端** 应用集成，通过 JWT Bearer Token 认证。

> 当前版本 v0.10.0。接口路由为服务前缀制：UC `/uc/v1/*`、AS `/as/v1/*`（`baseURL` 不带 `/api`）。

## 安装

```bash
npm install @aohoyo/client-sdk
```

> 需要 peer 依赖 `axios >= 1.17`。发布走 GitHub tag `js/v*` → CI 自动发 npm。

## 快速使用

```ts
import { createSdk } from '@aohoyo/client-sdk'

const sdk = createSdk({
  baseURL: '',                    // 或 'https://api.example.com'，不带 /api
  app_id: 'com.example.app',
  app_secret: '...',              // device/feedback 签名需要（可选）
  channel_code: 'official',       // 可选，升级/设备上报用
  current_version_code: 10203,    // 可选，升级检测 + 事件 app_version
  platform: 'windows',            // 可选
  onTokenExpired: () => router.push('/login'),
})

// 登录（app_id 由 SDK 自动附加）
const res = await sdk.auth.login({ username: 'admin', password: '123456' })
sdk.client.setTokens(res.access_token, res.refresh_token)

// 登录成功后会话按后台「会话模式」自动启动（heartbeat / login / open），
// 通常无需手动调用 sdk.session.start()。

// 获取当前用户信息
const info = await sdk.user.getUserInfo()

// 登出（自动清 token + userId）
await sdk.auth.logout()
```

## 数据统计

基础统计开箱即用——`createSdk()` 后自动启动（仅浏览器环境），事件批量上报（10s 或满 50 条 flush），页面关闭用 `sendBeacon` 兜底。

| 自动采集 | 说明 |
| -------- | ---- |
| 打开/会话 | `session_start`；`session_end` 每 60s 覆盖式 checkpoint（同 session_id 累计时长，服务端按会话取 MAX）+ unload 最终一条 |
| 用户维度 | 登录自动注入 user_id，登出自动清空 |
| app_version | 所有事件带版本号（供管理后台 by_version 分布） |

> ⚠️ v0.9.0 起 `window.onerror` / `unhandledrejection` 自动错误捕获已下线（SDK-4），
> 不再自动产生 error 事件；`trackError` 保留为 @deprecated 手动通道。错误收集请业务侧自建。

按需接入 PV / 自定义事件：

```ts
router.afterEach((to) => sdk.stats.trackPageView(to.fullPath))
sdk.stats.trackEvent('withdraw', { amount: 100 })
```

## 设备与安全（SEC-1，桌面端）

```ts
// 设备上报：risk_flags 自动取原生桥风险检测（Wails: DetectRisks / Tauri: detect_risks 命名兼容），
// 映射服务端枚举 debug/emulator/multiopen/root/hook，按安全策略开关裁剪；纯浏览器为 []
await sdk.device.report({ user_id: Number(info.user.id) })

// 拉取桌面端安全策略（DeviceSign 鉴权，自动缓存）
const cfg = await sdk.device.getSecurityConfig()
// cfg.risk_policy: 'report' | 'observe' | 'block'

// risk_policy=block 且检测到风险时，上报完成后触发回调（阻断 UI 由宿主决定）：
const sdk = createSdk({
  ...,
  onRiskBlocked: (flags) => showDialog(`检测到风险环境: ${flags.join(', ')}`),
})
```

## 换绑两步验证（UC-17）

目标应用启用旧号验证时，换绑需先验当前号/邮箱拿 `old_code`，再向新号发码提交：

```ts
// 1. 向当前手机号发码（target=当前号）→ 用户填码得到 oldCode
await sdk.user.sendProfileCode({ scene: 'change_phone', type: 'phone', target: currentPhone, ... })
// 2. 向新手机号发码
await sdk.user.sendProfileCode({ scene: 'change_phone', type: 'phone', target: newPhone, ... })
// 3. 提交（缺 old_code 会收到「请先验证当前手机号」）
await sdk.user.changePhone({ phone: newPhone, code: newCode, old_code: oldCode })
```

邮箱换绑同理（`changeEmail` + `scene: 'change_email'`）。

## 模块列表

| 模块 | 访问方式 | 功能 |
| ---- | -------- | ---- |
| `auth` | `sdk.auth` | 登录、登出、验证码、刷新 Token、密码重置 |
| `user` | `sdk.user` | 用户信息、资料、密码、手机/邮箱绑定换绑、菜单 |
| `session` | `sdk.session` | 会话管理（heartbeat / login / open 三模式） |
| `device` | `sdk.device` | 设备上报/验证（HMAC 签名）、风险检测、安全策略 |
| `oauth` | `sdk.oauth` | 第三方登录/绑定/解绑 |
| `captcha` | `sdk.captcha` | 阿里云滑块 + 图片验证码 |
| `upgrade` | `sdk.upgrade` | 版本检测、定时轮询、升级下载 |
| `stats` | `sdk.stats` | 统计上报（自动 + trackPageView/trackEvent） |
| `storage` | `sdk.storage` | 头像上传 |
| `feedback` | `sdk.feedback` | 用户反馈（登录 JWT / 免登录 DeviceSign） |
| `ads` | `sdk.ads` | 广告拉取（按广告位分组）+ 曝光/点击上报（登录 JWT / 免登录 DeviceSign 自动切换） |

### SdkClient

`createSdk()` 返回的 `client` 实例提供底层能力：

| 方法 / 属性 | 说明 |
| ----------- | ---- |
| `setTokens(a, r)` / `clearTokens()` | Token 存取（登出同时清 userId） |
| `appId` / `deviceId` / `deviceInfo` | 应用与设备标识 |
| `sessionId` / `userId` | 统计口径共享的会话/用户 ID |
| `isLoggedIn` | 是否已登录 |

## 客户端广告（AD）

```ts
// 拉取在投广告（按广告位 code 分组）；登录走 JWT，免登录自动 DeviceSign（GET 空 body 签名）
const ads = await sdk.ads.getAds({ position: 'splash' })   // position 省略 = 全部启用广告位
const item = ads.positions.splash?.[0]

if (item) {
  await sdk.ads.reportImpression(item.id)   // 渲染后上报曝光
  onClick(async () => {
    await sdk.ads.reportClick(item.id)      // 点击先上报再跳转
    window.open(item.link_url)
  })
}
```

> `ad_type` 决定渲染形态（1=文字 2=图片 3=弹窗 4=开屏 5=横幅），宽高建议值来自广告位配置。
> 完整契约见主仓库 `docs/specs/ad.md`。

## 开发

```bash
npm run build     # TypeScript 编译（strict），输出到 dist/
npm run dev       # 监听模式编译
```

构建产物为 `dist/index.js`（ESM），类型声明为 `dist/index.d.ts`。开发规范见 [`AGENTS.md`](./AGENTS.md)。

## License

MIT
