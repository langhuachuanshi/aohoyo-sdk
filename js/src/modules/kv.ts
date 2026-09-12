import type { SdkClient } from '../client'

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

export type KvModule = ReturnType<typeof createKvModule>

export function createKvModule(client: SdkClient) {
  /** 确保 app_secret 已配置（设备签名需要） */
  function requireSecret(): string {
    if (!client.appSecret) {
      throw new Error('app_secret is required for kv operations')
    }
    return client.appSecret
  }

  return {
    /**
     * 拉取应用可见的 KV 配置（DeviceSign 鉴权，GET 空 body 签名）。
     * 服务端返回本应用 + 公共区的合并结果（本应用同 key 优先），值已按类型解析：
     * string → string，number → number，boolean → boolean，json → 解析后的对象/数组。
     *
     * @param keys 可选。不传 → 返回全部；传单个 key 或 key 数组 → 只返回指定的键
     *  （不存在的键直接缺席，不报错）。注意：按需过滤仅节省传输，不是访问控制。
     */
    async get(keys?: string | string[]): Promise<Record<string, unknown>> {
      await client.ready
      const headers = await buildSignHeaders(client.deviceId, client.appId, requireSecret(), '')
      const params =
        keys === undefined ? undefined : { key: Array.isArray(keys) ? keys.join(',') : keys }
      return client.getWithHeaders<Record<string, unknown>>('/as/v1/app/kv', params, headers)
    },
  }
}
