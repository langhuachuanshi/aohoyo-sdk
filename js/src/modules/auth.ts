import type { SdkClient } from '../client'
import type { LoginResponse, RegisterParams, RefreshResponse } from '../types'

export interface AuthModuleOptions {
  /**
   * 登录/注册/验证码登录成功后的回调。
   * 用于让 SDK 消费后台下发的 session_mode 等配置并自动启动会话模块。
   */
  onLoginSuccess?: (resp: LoginResponse) => void
}

export function createAuthModule(client: SdkClient, opts?: AuthModuleOptions) {
  /** 版本号字符串（无配置时为 undefined，不下发该字段） */
  const appVersion = client.versionCode != null ? String(client.versionCode) : undefined

  /** register/codeLogin 共用的来源字段 */
  const sourceFields = () => ({
    channel: client.channelCode,
    app_version: appVersion,
  })

  /** 统一在登录类成功后触发回调 */
  const afterLogin = (resp: LoginResponse): LoginResponse => {
    opts?.onLoginSuccess?.(resp)
    return resp
  }

  return {
    /** 用户登录（账号密码，自动附加 app_id 和设备信息） */
    async login(params: { username: string; password: string }): Promise<LoginResponse> {
      return afterLogin(
        await client.post('/uc/v1/auth/login', {
          ...params,
          app_id: client.appId,
          device: client.deviceInfo,
        })
      )
    },

    /**
     * 用户注册（自动附加 app_id 和设备信息）。
     * 支持手机号或邮箱注册（二选一）：
     * @example 邮箱注册
     * await sdk.auth.sendCode({ scene: 'register', type: 'email', target, captcha_verify_param })
     * const res = await sdk.auth.register({ username, password, email: target, code })
     */
    async register(params: Omit<RegisterParams, 'device' | 'app_id'>): Promise<LoginResponse> {
      return afterLogin(
        await client.post('/uc/v1/auth/register', {
          ...params,
          ...sourceFields(),
          app_id: client.appId,
          device: client.deviceInfo,
        })
      )
    },

    /**
     * 验证码登录（未注册自动注册，自动附加 app_id 和设备信息）。
     * 支持手机号或邮箱验证码登录。
     * @example 邮箱验证码登录
     * await sdk.auth.sendCode({ scene: 'login', type: 'email', target, captcha_verify_param })
     * const res = await sdk.auth.codeLogin({ type: 'email', target, code })
     */
    async codeLogin(params: {
      type: 'phone' | 'email'
      target: string
      code: string
    }): Promise<LoginResponse> {
      return afterLogin(
        await client.post('/uc/v1/auth/login/code', {
          ...params,
          ...sourceFields(),
          app_id: client.appId,
          device: client.deviceInfo,
        })
      )
    },

    /** 退出登录 */
    async logout(): Promise<void> {
      await client.post('/uc/v1/auth/logout')
      client.clearTokens()
    },

    /** 刷新 Token */
    async refreshToken(refresh_token: string): Promise<RefreshResponse> {
      return client.post('/uc/v1/auth/refresh', { refresh_token })
    },

    /** 发送验证码（公开场景：注册 / 登录 / 找回密码）。需先完成服务商验证码（图形/行为），传入凭证 */
    async sendCode(data: {
      scene: 'register' | 'login' | 'reset_password'
      type: 'phone' | 'email'
      target: string
      captcha_verify_param?: string
      captcha_id?: string
      captcha_code?: string
    }): Promise<void> {
      return client.post('/uc/v1/auth/code/send', data)
    },

    /** 重置密码（未登录） */
    async resetPassword(data: {
      type: 'phone' | 'email'
      target: string
      code: string
      password: string
    }): Promise<void> {
      return client.post('/uc/v1/auth/password/reset', data)
    },

    /**
     * 仅校验验证码（不消费）。校验通过后，同一个验证码仍可用于 resetPassword 等"消费"接口。
     * 适用于"先校验再重置"两步流程中的第一步。
     */
    verifyCode(data: {
      scene: 'register' | 'login' | 'reset_password'
      type: 'phone' | 'email'
      target: string
      code: string
    }): Promise<void> {
      return client.post('/uc/v1/auth/code/verify', data)
    },

    /** 校验邮箱验证链接 token（公开，验证落地页调用，成功后置 email_verified=1） */
    verifyEmail(token: string): Promise<void> {
      return client.post('/uc/v1/auth/email/verify', { token })
    },
  }
}
