import type { DeviceInfo } from './device'

/** 登录参数 */
export interface LoginParams {
  username: string
  password: string
  app_id: string
  device?: DeviceInfo
}

/** 注册参数 */
export interface RegisterParams {
  username: string
  password: string
  phone?: string
  email?: string
  code: string
  app_id: string
  device?: DeviceInfo
}

/** 登录/注册/验证码登录返回的精简用户信息（完整字段需调用 getUserInfo） */
export interface LoginUser {
  id: number
  username: string
  user_code: string
  nickname: string
  avatar: string
}

/** 会话模式：heartbeat=心跳保活 login=仅登录计数 open=打开即计 */
export type SessionMode = 'heartbeat' | 'login' | 'open'

/** 登录响应 */
export interface LoginResponse {
  access_token: string
  refresh_token: string
  expires_in: number
  /** 后台下发的会话模式，SDK 据此自动驱动心跳/计数（缺省时回退到构造函数配置） */
  session_mode?: SessionMode
  user: LoginUser
}

/** Token 刷新响应 */
export interface RefreshResponse {
  access_token: string
  refresh_token: string
  expires_in: number
}
