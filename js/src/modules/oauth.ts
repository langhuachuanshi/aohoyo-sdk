import type { SdkClient } from '../client'

// SDK-2 TODO: OAuth loginByUserID 需传 app_id，否则 session_mode 无法按应用下发
import type { OAuthProvider, OAuthAuthURL, OAuthCallbackResult, OAuthBinding, OAuthBindParams, OAuthUnboundInfo } from '../types/oauth'

export function createOAuthModule(client: SdkClient) {
  return {
    /** 获取已启用的第三方登录渠道 */
    getProviders(): Promise<OAuthProvider[]> {
      return client.get('/uc/v1/auth/oauth/providers')
    },

    /** 获取第三方授权 URL */
    getAuthURL(provider: string): Promise<OAuthAuthURL> {
      return client.get(`/uc/v1/auth/oauth/${provider}/url`)
    },

    /** 第三方登录回调（已绑定自动登录，未绑定返回临时信息） */
    callback(provider: string, code: string, state?: string): Promise<OAuthCallbackResult> {
      return client.post(`/uc/v1/auth/oauth/${provider}/callback`, { code, state })
    },

    /** 绑定第三方账号到当前用户 */
    bind(params: OAuthBindParams): Promise<void> {
      return client.post('/uc/v1/auth/oauth/bind', params)
    },

    /** 解绑第三方账号 */
    unbind(provider: string): Promise<void> {
      return client.delete(`/uc/v1/auth/oauth/${provider}/unbind`)
    },

    /** 查询当前用户已绑定的第三方账号 */
    getBindings(): Promise<OAuthBinding[]> {
      return client.get('/uc/v1/auth/oauth/bindings')
    },
  }
}
