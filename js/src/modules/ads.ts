import type { SdkClient } from '../client'
import type { AdReportParams, ClientAdsResponse, GetAdsParams } from '../types/ad'

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
 * 构造设备签名请求头（与服务端 DeviceSign 中间件对齐；GET 空 body 参与签名）
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

export function createAdsModule(client: SdkClient) {
  /** 确保 app_secret 已配置（免登录模式需要） */
  function requireSecret(): string {
    if (!client.appSecret) {
      throw new Error('app_secret is required for ads operations without login')
    }
    return client.appSecret
  }

  return {
    /**
     * 拉取当前有效广告（按广告位分组）。
     * 登录用户走 JWT，免登录设备走 DeviceSign（GET 空 body 签名）。
     */
    async getAds(params?: GetAdsParams): Promise<ClientAdsResponse> {
      await client.ready
      const query = { app_id: client.appId, position: params?.position || undefined }

      if (client.isLoggedIn) {
        return client.get<ClientAdsResponse>('/as/v1/ads', query)
      }

      const headers = await buildSignHeaders(client.deviceId, client.appId, requireSecret(), '')
      return client.getWithHeaders<ClientAdsResponse>('/as/v1/ads', query, headers)
    },

    /**
     * 上报广告展示（公开接口，限流 100/min）。
     * 登录态下 axios 拦截器自动注入 JWT，服务端会记录 user_id。
     */
    async reportImpression(adId: number, params?: AdReportParams): Promise<void> {
      await client.ready
      await client.post('/as/v1/ads/impression', {
        ad_id: adId,
        device_id: params?.device_id || client.deviceId,
      })
    },

    /**
     * 上报广告点击（公开接口，限流 100/min）。
     */
    async reportClick(adId: number, params?: AdReportParams): Promise<void> {
      await client.ready
      await client.post('/as/v1/ads/click', {
        ad_id: adId,
        device_id: params?.device_id || client.deviceId,
      })
    },
  }
}
