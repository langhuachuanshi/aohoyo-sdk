import type { SdkClient } from '../client'
import type {
  FeedbackSubmitParams,
  FeedbackSubmitResponse,
  FeedbackMyListParams,
  FeedbackMyListResponse,
  UploadImageResponse,
} from '../types/feedback'

/**
 * HMAC-SHA256 签名（Web Crypto API）
 */
async function hmacSha256(key: string, data: string): Promise<string> {
  const encoder = new TextEncoder()
  const cryptoKey = await crypto.subtle.importKey(
    'raw',
    encoder.encode(key),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  )
  const sig = await crypto.subtle.sign('HMAC', cryptoKey, encoder.encode(data))
  return Array.from(new Uint8Array(sig))
    .map(b => b.toString(16).padStart(2, '0'))
    .join('')
}

/**
 * 构造设备签名请求头（与服务端 DeviceSign 中间件对齐）
 */
async function buildSignHeaders(
  deviceId: string,
  appId: string,
  signKey: string,
  body: string,
): Promise<Record<string, string>> {
  const timestamp = Math.floor(Date.now() / 1000).toString()
  const signData = `${deviceId}\n${timestamp}\n${body}`
  const sign = await hmacSha256(signKey, signData)
  return {
    'X-App-ID': appId,
    'X-Device-Sign': sign,
    'X-Device-ID': deviceId,
    'X-Timestamp': timestamp,
  }
}

export function createFeedbackModule(client: SdkClient) {
  /** 确保 app_secret 已配置（免登录模式需要） */
  function requireSecret(): string {
    if (!client.appSecret) {
      throw new Error('app_secret is required for feedback operations without login')
    }
    return client.appSecret
  }

  return {
    /**
     * 上传反馈图片。
     * 登录用户走 JWT，免登录设备走 DeviceSign HMAC。
     */
    async uploadImage(file: File): Promise<UploadImageResponse> {
      await client.ready
      const formData = new FormData()
      formData.append('file', file)

      if (client.isLoggedIn) {
        // JWT 模式：token 由 axios 拦截器自动注入
        return client.upload<UploadImageResponse>('/as/v1/feedback/upload-image', formData)
      }

      // DeviceSign 模式：构造签名头（multipart 不参与签名，仅验证设备身份）
      const headers = await buildSignHeaders(
        client.deviceId,
        client.appId,
        requireSecret(),
        '', // multipart 请求体不可预测，使用空串签名
      )
      return client.postWithHeaders<UploadImageResponse>(
        '/as/v1/feedback/upload-image',
        formData as any,
        headers,
      )
    },

    /**
     * 提交反馈。
     * 登录用户走 JWT，免登录设备走 DeviceSign HMAC。
     * 如果 images 包含 File 对象，会先上传再提交 URL。
     */
    async submit(params: {
      app_id?: string
      title: string
      content: string
      type: 1 | 2 | 3
      images?: (File | string)[]
    }): Promise<FeedbackSubmitResponse> {
      await client.ready

      // 处理图片：File 对象先上传，URL 字符串直接使用
      let imageUrls: string[] = []
      if (params.images && params.images.length > 0) {
        const uploadPromises = params.images.map(async (img) => {
          if (img instanceof File) {
            const result = await this.uploadImage(img)
            return result.url
          }
          return img // 已经是 URL 字符串
        })
        imageUrls = await Promise.all(uploadPromises)
      }

      const body: FeedbackSubmitParams = {
        app_id: params.app_id || client.appId,
        title: params.title,
        content: params.content,
        type: params.type,
        images: imageUrls.length > 0 ? imageUrls : undefined,
      }

      if (client.isLoggedIn) {
        return client.post<FeedbackSubmitResponse>('/as/v1/feedback', body)
      }

      const headers = await buildSignHeaders(
        client.deviceId,
        body.app_id,
        requireSecret(),
        JSON.stringify(body),
      )
      return client.postWithHeaders<FeedbackSubmitResponse>('/as/v1/feedback', body, headers)
    },

    /**
     * 查询我的反馈列表（仅 JWT 登录用户可用）。
     */
    async myList(params?: FeedbackMyListParams): Promise<FeedbackMyListResponse> {
      if (!client.isLoggedIn) {
        throw new Error('myList requires login (JWT token)')
      }
      return client.get<FeedbackMyListResponse>('/as/v1/feedback/my', {
        page: params?.page || 1,
        page_size: params?.page_size || 10,
        app_id: params?.app_id,
        status: params?.status,
      } as Record<string, any>)
    },
  }
}
