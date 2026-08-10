export { SdkClient, type SdkConfig } from './client'
export { getDeviceInfo, initDeviceId } from './device'
export { createAuthModule } from './modules/auth'
export { createUserModule } from './modules/user'
export { createUpgradeModule } from './modules/upgrade'
export { createDeviceModule } from './modules/device'
export { createSessionModule } from './modules/session'
export { createOAuthModule } from './modules/oauth'
export { createStatsModule } from './modules/stats'
export {
  createCaptchaModule,
  type CaptchaModule,
  type CaptchaScene,
  type CaptchaConfig,
  type ImageCaptcha,
} from './modules/captcha'
export { createStorageModule, type StorageModule, type AvatarUploadResult } from './modules/storage'
export { createFeedbackModule } from './modules/feedback'
export * from './types'

import { SdkClient, type SdkConfig } from './client'
import { createAuthModule } from './modules/auth'
import { createUserModule } from './modules/user'
import { createUpgradeModule } from './modules/upgrade'
import { createDeviceModule } from './modules/device'
import { createSessionModule } from './modules/session'
import { createOAuthModule } from './modules/oauth'
import { createStatsModule } from './modules/stats'
import { createCaptchaModule } from './modules/captcha'
import { createStorageModule } from './modules/storage'
import { createFeedbackModule } from './modules/feedback'

/**
 * 创建 SDK 实例
 *
 * 登录成功后，SDK 会自动用后台下发的 session_mode 覆盖本地配置并启动会话模块
 * （heartbeat→心跳保活，login/open→计时+关闭上报），接入方通常无需手动调用 session.start()。
 *
 * @example
 * ```ts
 * const sdk = createSdk({
 *   baseURL: '',
 *   app_id,
 *   onTokenExpired: () => router.push('/login'),
 * })
 *
 * const res = await sdk.auth.login({ username, password })
 * sdk.client.setTokens(res.access_token, res.refresh_token)
 * // 会话已按后台配置自动启动
 * ```
 */
export function createSdk(config: SdkConfig) {
  const client = new SdkClient(config)

  // stats 先于 session 构造，以便登录回调自动注入 userId
  const stats = createStatsModule(client)

  // 统计自动初始化：deviceId 就绪后启动（session_start / 定时上报 / 错误监听）。
  // 接入方无需手动调 sdk.stats.init()，基础统计开箱即用。
  // 仅 Web 环境启用（init 内部依赖 window/document，非浏览器环境会自动跳过监听注册）。
  client.ready.then(() => {
    if (typeof window !== 'undefined') stats.init()
  })

  // session 先于 auth 构造，以便登录回调中按后台下发的 mode 自动启动
  const session = createSessionModule(client)
  const auth = createAuthModule(client, {
    onLoginSuccess: (resp) => {
      // 后台下发的 session_mode 覆盖本地默认值（缺省时保持现状）
      if (resp?.session_mode) {
        client.setSessionMode(resp.session_mode)
      }
      // 登录成功自动注入 user_id：写入 client.userId（后续事件带上用户维度，用户/留存统计才准确）。
      // 与 token 生命周期绑定：clearTokens（登出）自动清空，避免换用户登录后事件带旧 user_id。
      if (resp?.user?.id != null) {
        client.userId = String(resp.user.id)
      }
      session.start()
    },
  })

  return {
    client,
    auth,
    user: createUserModule(client),
    upgrade: createUpgradeModule(client),
    device: createDeviceModule(client),
    session,
    oauth: createOAuthModule(client),
    stats,
    captcha: createCaptchaModule(client),
    storage: createStorageModule(client),
    feedback: createFeedbackModule(client),
  }
}

export type SdkInstance = ReturnType<typeof createSdk>
