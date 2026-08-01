/** 用户信息响应 */
export interface UserInfoResponse {
  user: {
    id: number
    username: string
    nickname: string
    email: string
    phone: string
    avatar: string
    gender: number
    birthday: string
    bio: string
    user_code: string
    status: number
    last_login_time: string
    created_at: string
    updated_at: string
    /** 是否已设置登录密码（验证码注册用户首次为 false，设过密码后 true） */
    has_password: boolean
  }
  roles: { id: number; name: string }[]
  permissions: string[]
  app_ids: string[]
  /** 服务端下发的会话模式（SDK-3：覆盖本地默认值，刷新页面不回退） */
  session_mode: string
}

/** 密码策略（GET /v1/auth/password/policy 返回） */
export interface PasswordPolicy {
  min_length: number
  max_length: number
  require_upper: boolean
  require_lower: boolean
  require_digit: boolean
  require_special: boolean
  min_types: number
  special_chars: string
}

/** 菜单项 */
export interface MenuItem {
  id: number
  parent_id: number
  name: string
  icon: string
  url: string
  sort: number
  permission: string
  app_id: string
  is_system: boolean
  visible: boolean
  status: number
  created_at: string
  children?: MenuItem[]
}
