import type { SdkClient } from '../client'
import type { UserInfoResponse, MenuItem, PasswordPolicy } from '../types'

export function createUserModule(client: SdkClient) {
  return {
    /** 获取当前用户信息（含角色/权限）。
     *  每次请求后端，不缓存；接入方按需自行缓存。
     *  SDK-3：自动读取 session_mode 更新客户端配置（解决刷新页面回退默认值）。 */
    async getUserInfo(): Promise<UserInfoResponse> {
      const info = await client.get<UserInfoResponse>('/v1/auth/info')
      if (info.session_mode) {
        client.sessionMode = info.session_mode
      }
      return info
    },

    /** 修改个人资料 */
    updateProfile(data: Record<string, any>): Promise<void> {
      return client.put('/v1/profile/info', data)
    },

    /** 设置/修改密码。
     *  - 已有密码（user.has_password=true）：oldPassword 必填且须正确
     *  - 首次设置（user.has_password=false）：oldPassword 传空串 "" 或不传 */
    updatePassword(params: { newPassword: string; oldPassword?: string }): Promise<void> {
      return client.put('/v1/profile/password', {
        old_password: params.oldPassword ?? '',
        new_password: params.newPassword,
      })
    },

    /** 获取密码策略（公开接口，无需登录）。
     *  前端可据此做客户端预校验，减少无效请求。 */
    getPasswordPolicy(): Promise<PasswordPolicy> {
      return client.get('/v1/auth/password/policy')
    },

    /** 发送「绑定/换绑」验证码（登录场景，需登录态）。
     *  scene 仅限登录场景；注册/登录/找回密码请用 sdk.auth.sendCode（公开接口）。 */
    sendProfileCode(data: {
      scene: 'bind_phone' | 'change_phone' | 'bind_email' | 'change_email'
      type: 'phone' | 'email'
      target: string
      captcha_verify_param?: string
      captcha_id?: string
      captcha_code?: string
    }): Promise<void> {
      return client.post('/v1/profile/code/send', data)
    },

    /** 绑定手机（当前无手机时，scene=bind_phone） */
    bindPhone(data: { phone: string; code: string }): Promise<void> {
      return client.post('/v1/profile/phone/bind', data)
    },

    /** 换绑手机（当前已有手机时，scene=change_phone） */
    changePhone(data: { phone: string; code: string }): Promise<void> {
      return client.post('/v1/profile/phone/change', data)
    },

    /** 绑定邮箱（当前无邮箱时，scene=bind_email） */
    bindEmail(data: { email: string; code: string }): Promise<void> {
      return client.post('/v1/profile/email/bind', data)
    },

    /** 换绑邮箱（当前已有邮箱时，scene=change_email） */
    changeEmail(data: { email: string; code: string }): Promise<void> {
      return client.post('/v1/profile/email/change', data)
    },

    /** 发送邮箱验证码（需登录，验证码发到当前用户邮箱）。
     *  用户收到后调 verifyEmail(code) 校验，成功置 email_verified=1。 */
    sendEmailVerify(): Promise<void> {
      return client.post('/v1/profile/email/verify/send', { app_id: client.appId })
    },

    /** 校验邮箱验证码（需登录），成功后 email_verified 置 1 */
    verifyEmail(code: string): Promise<void> {
      return client.post('/v1/profile/email/verify', { code })
    },

    /** 获取当前用户菜单树，可按 app_id 过滤 */
    getMenuTree(appId?: string): Promise<MenuItem[]> {
      const params = appId ? { app_id: appId } : undefined
      return client.get('/v1/menus', params)
    },
  }
}
