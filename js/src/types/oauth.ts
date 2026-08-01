/** OAuth 服务商 */
export interface OAuthProvider {
  provider: string
  name: string
}

/** OAuth 授权 URL */
export interface OAuthAuthURL {
  url: string
  state: string
}

/** OAuth 未绑定用户信息（回调返回 is_bind=false） */
export interface OAuthUnboundInfo {
  is_bind: false
  provider: string
  open_id: string
  union_id: string
  nickname: string
  avatar: string
  temp_token: string
}

/** OAuth 回调结果：已绑定直接返回登录信息，未绑定返回临时信息 */
export type OAuthCallbackResult = {
  is_bind: true
  access_token: string
  refresh_token: string
  expires_in: number
  user: Record<string, any>
} | OAuthUnboundInfo

/** OAuth 已绑定账号 */
export interface OAuthBinding {
  provider: string
  open_id: string
  nickname: string
  avatar: string
}

/** OAuth 绑定请求参数 */
export interface OAuthBindParams {
  provider: string
  temp_token: string
  open_id: string
  union_id?: string
  nickname?: string
  avatar?: string
}
