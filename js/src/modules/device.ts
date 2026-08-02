import type { SdkClient } from '../client'
import type { DeviceVerifyResponse } from '../types'

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
 * 构造设备签名请求头
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

export function createDeviceModule(client: SdkClient) {
  /** 确保 app_secret 已配置 */
  function requireSecret(): string {
    if (!client.appSecret) {
      throw new Error('app_secret is required for device operations (report/verify)')
    }
    return client.appSecret
  }

  return {
    /** 设备上报（使用初始化时缓存的设备信息） */
    async report(params?: { user_id?: number }): Promise<void> {
      await client.ready
      const info = client.deviceInfo
      const body: Record<string, any> = {
        app_id: client.appId,
        device_id: client.deviceId,
        user_id: params?.user_id,
        device_brand: info.vendor,
        device_model: info.model,
        os_version: `${info.os_name} ${info.os_version}`,
        app_version: client.versionCode?.toString() || '',
        channel_code: client.channelCode,
        ip: '',
        risk_flags: [],
      }
      const headers = await buildSignHeaders(client.deviceId, client.appId, requireSecret(), JSON.stringify(body))
      await client.postWithHeaders('/v1/devices/report', body, headers)
    },

    /** 设备验证（使用初始化时缓存的设备码） */
    async verify(): Promise<DeviceVerifyResponse> {
      const body: Record<string, any> = {
        app_id: client.appId,
        device_id: client.deviceId,
      }
      const headers = await buildSignHeaders(client.deviceId, client.appId, requireSecret(), JSON.stringify(body))
      return client.postWithHeaders('/v1/devices/verify', body, headers)
    },
  }
}
