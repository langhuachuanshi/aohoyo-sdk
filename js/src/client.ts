import axios, { type AxiosInstance, type AxiosRequestConfig } from 'axios'
import { getDeviceInfo, initDeviceId } from './device'
import type { DeviceInfo, SessionMode } from './types'

export interface SdkConfig {
  /** 后端 API 基础地址，如 https://api.example.com */
  baseURL: string
  /** 应用 ID */
  app_id: string
  /** 应用密钥（管理后台获取） */
  app_secret?: string
  /** 渠道代码，如 huawei、xiaomi（device/upgrade 模块共用） */
  channel_code?: string
  /** 当前版本号（upgrade 模块使用） */
  current_version_code?: number
  /** 当前平台（upgrade 模块使用），如 android/ios/windows/macos/linux */
  platform?: string
  /** Token 过期回调，各端自行处理跳转逻辑 */
  onTokenExpired?: () => void
  /** Token 存储抽象，默认使用 localStorage */
  storage?: {
    getItem: (key: string) => string | null
    setItem: (key: string, value: string) => void
    removeItem: (key: string) => void
  }
  /** 请求超时时间（毫秒），不传则使用 axios 默认值 */
  timeout?: number
  /** 会话模式：heartbeat=心跳保活（默认）login=仅登录计数 open=打开即计 */
  session_mode?: SessionMode
}

const TOKEN_KEY = 'aohoyo_access_token'
const REFRESH_KEY = 'aohoyo_refresh_token'

export class SdkClient {
  private http: AxiosInstance
  private config: SdkConfig
  private refreshing: Promise<string | null> | null = null
  /** 设备信息缓存（初始化时采集一次） */
  readonly deviceInfo: DeviceInfo
  /** 设备码（硬件指纹），initDeviceId 完成后可用 */
  deviceId: string
  /** 设备码初始化完成后的 resolve，模块可 await 确保拿到值 */
  readonly ready: Promise<void>
  /**
   * 统一会话 ID（stats 模块与 session 模块共享）。
   *
   * 两个模块原本各自生成 session_id，导致 admin-server 按 session_id 聚合的
   * 跳出率等指标口径错乱（page_view 的 session_id 与 session_end 的对不上）。
   * 统一到这里，由 stats 模块 startSession 时生成并写入，session 模块读取复用。
   */
  sessionId = ''
  /**
   * 当前登录用户 ID（stats 模块上报事件时带上，用户/留存统计才准确）。
   *
   * 登录成功回调写入；clearTokens（登出）时自动清空，与 token 生命周期绑定，
   * 避免同一设备换用户登录后，前端事件仍带上前一个 user_id。
   */
  userId = ''

  constructor(config: SdkConfig) {
    this.config = config

    // 初始化时采集并缓存设备信息，后续 report 等模块直接使用
    this.deviceInfo = getDeviceInfo()
    this.deviceId = this.deviceInfo.device_id

    // 异步初始化设备码（Web 端生成浏览器指纹，原生端直接从注入读取）
    this.ready = initDeviceId().then((id) => {
      this.deviceId = id
      this.deviceInfo.device_id = id
    })

    this.http = axios.create({ baseURL: config.baseURL, timeout: config.timeout })

    // 请求拦截：自动附带 Token
    this.http.interceptors.request.use((req) => {
      const token = this.getStorage()?.getItem(TOKEN_KEY)
      if (token) {
        req.headers.Authorization = `Bearer ${token}`
      }
      return req
    })

    // 响应拦截：401 自动刷新
    this.http.interceptors.response.use(
      (res) => res,
      async (error) => {
        const original = error.config as AxiosRequestConfig & { _retry?: boolean }
        if (error.response?.status === 401 && !original._retry) {
          original._retry = true
          const token = await this.refreshToken()
          if (token) {
            original.headers = { ...original.headers, Authorization: `Bearer ${token}` }
            return this.http(original)
          }
          this.config.onTokenExpired?.()
        }
        return Promise.reject(error)
      }
    )
  }

  /** 获取存储实例 */
  private getStorage() {
    return this.config.storage ?? (typeof localStorage !== 'undefined' ? localStorage : undefined)
  }

  /** 解开后端统一响应 { code, data, message }，返回 data */
  private unwrap<T>(res: { data: any }): T {
    const body = res.data
    if (body && typeof body === 'object' && 'code' in body && 'data' in body) {
      return body.data as T
    }
    return body as T
  }

  /** GET 请求 */
  get<T>(url: string, params?: Record<string, any>): Promise<T> {
    return this.http.get(url, { params }).then(r => this.unwrap<T>(r))
  }

  /** POST 请求 */
  post<T>(url: string, data?: Record<string, any>): Promise<T> {
    return this.http.post(url, data).then(r => this.unwrap<T>(r))
  }

  /** PUT 请求 */
  put<T>(url: string, data?: Record<string, any>): Promise<T> {
    return this.http.put(url, data).then(r => this.unwrap<T>(r))
  }

  /** DELETE 请求 */
  delete<T>(url: string, params?: Record<string, any>): Promise<T> {
    return this.http.delete(url, { params }).then(r => this.unwrap<T>(r))
  }

  /** POST 请求（带自定义请求头，用于设备签名等场景） */
  postWithHeaders<T>(url: string, data?: Record<string, any>, headers?: Record<string, string>): Promise<T> {
    return this.http.post(url, data, { headers }).then(r => this.unwrap<T>(r))
  }

  /** 上传文件（multipart/form-data，由 axios 自动设置 boundary） */
  upload<T>(url: string, formData: FormData): Promise<T> {
    return this.http.post(url, formData).then(r => this.unwrap<T>(r))
  }

  /** 设置 Token（登录成功后调用） */
  setTokens(access: string, refresh: string) {
    const storage = this.getStorage()
    storage?.setItem(TOKEN_KEY, access)
    storage?.setItem(REFRESH_KEY, refresh)
  }

  /** 清除 Token（登出时调用）——同时清空 userId，避免换用户登录后事件带旧 user_id */
  clearTokens() {
    const storage = this.getStorage()
    storage?.removeItem(TOKEN_KEY)
    storage?.removeItem(REFRESH_KEY)
    this.userId = ''
  }

  /** 获取当前 access_token（未登录返回 null） */
  getAccessToken(): string | null {
    return this.getStorage()?.getItem(TOKEN_KEY) ?? null
  }

  /** 是否已登录（access_token 存在） */
  get isLoggedIn(): boolean {
    return !!this.getAccessToken()
  }

  /** 获取应用 ID */
  get appId() {
    return this.config.app_id
  }

  /** API 基础地址（sendBeacon 等场景需要拼完整 URL） */
  get baseURL(): string {
    return this.config.baseURL
  }

  /** 获取应用密钥 */
  get appSecret() {
    return this.config.app_secret
  }

  /** 获取渠道代码 */
  get channelCode() {
    return this.config.channel_code
  }

  /** 获取当前版本号 */
  get versionCode() {
    return this.config.current_version_code
  }

  /** 获取当前平台 */
  get platform() {
    return this.config.platform
  }

  /** 获取会话模式 */
  get sessionMode() {
    return this.config.session_mode ?? 'heartbeat'
  }

  /** 更新会话模式（登录后由后台下发配置覆盖本地默认值时调用） */
  setSessionMode(mode: SessionMode) {
    this.config.session_mode = mode
  }

  /** 刷新 Token（防并发，只发一次） */
  private refreshToken(): Promise<string | null> {
    if (this.refreshing) return this.refreshing
    const storage = this.getStorage()
    const refresh = storage?.getItem(REFRESH_KEY)
    if (!refresh) return Promise.resolve(null)

    this.refreshing = axios
      .post(`${this.config.baseURL}/v1/auth/refresh`, { refresh_token: refresh })
      .then((res) => {
        const { access_token, refresh_token } = res.data?.data ?? res.data
        if (access_token) {
          this.setTokens(access_token, refresh_token)
          return access_token
        }
        return null
      })
      .catch(() => null)
      .finally(() => { this.refreshing = null })

    return this.refreshing
  }
}
