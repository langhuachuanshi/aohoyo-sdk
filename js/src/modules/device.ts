import type { SdkClient } from '../client'
import type { DeviceVerifyResponse, RiskFlag, SecurityConfig } from '../types'

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

/**
 * 服务端 risk.Analyze 接受的 risk_flags 枚举（debug 对应 RiskDebug=6，主仓库 022c5a4 起支持；
 * sign 为服务端判定项，客户端检测不产生）。
 */
const RISK_FLAG_ENUM: ReadonlySet<string> = new Set(['debug', 'emulator', 'multiopen', 'root', 'hook'])

/**
 * 从桌面原生桥检测风险（window.__AOHOYO_NATIVE__.DetectRisks，go/native 暴露给 Wails 前端的绑定）。
 * 能力探测：桥缺失或未提供 DetectRisks 时静默降级为空数组（纯浏览器无检测能力，属预期），
 * 任何异常不得抛出影响 device.report 主流程。
 */
async function detectNativeRisks(): Promise<string[]> {
  try {
    const bridge = (typeof window !== 'undefined' ? (window as any).__AOHOYO_NATIVE__ : undefined)
    if (typeof bridge?.DetectRisks !== 'function') return []
    const res = await bridge.DetectRisks()
    const flags: unknown = res?.flags
    if (!Array.isArray(flags)) return []
    return flags.filter((f): f is string => typeof f === 'string')
  } catch {
    return []
  }
}

/** 把原生桥的原始 flags 映射到服务端接受枚举（未知值丢弃） */
function mapRiskFlags(flags: string[]): RiskFlag[] {
  return flags.filter(f => RISK_FLAG_ENUM.has(f)) as RiskFlag[]
}

/** 按安全策略开关裁剪检测项（如 emulator_detect=false 则该项不参与上报/阻断判定） */
function pruneRiskFlagsByPolicy(flags: RiskFlag[], cfg: SecurityConfig | null): RiskFlag[] {
  if (!cfg) return flags
  return flags.filter(f => {
    switch (f) {
      case 'debug': return cfg.anti_debug !== false
      case 'emulator': return cfg.emulator_detect !== false
      case 'root': return cfg.root_detect !== false
      case 'hook': return cfg.hook_detect !== false
      case 'multiopen': return cfg.anti_multi_open !== false
      default: return true
    }
  })
}

export function createDeviceModule(client: SdkClient) {
  /** 确保 app_secret 已配置 */
  function requireSecret(): string {
    if (!client.appSecret) {
      throw new Error('app_secret is required for device operations (report/verify)')
    }
    return client.appSecret
  }

  // 安全策略缓存：首次上报前拉取一次（SEC-1），之后复用
  let securityConfig: SecurityConfig | null = null
  let securityConfigPromise: Promise<SecurityConfig | null> | null = null

  /** 拉取桌面端安全策略（DeviceSign 鉴权，空请求体）。失败静默返回 null，按默认策略（只上报）降级 */
  function fetchSecurityConfig(): Promise<SecurityConfig | null> {
    if (securityConfig) return Promise.resolve(securityConfig)
    if (!securityConfigPromise) {
      securityConfigPromise = (async () => {
        try {
          const headers = await buildSignHeaders(client.deviceId, client.appId, requireSecret(), '')
          const cfg = await client.postWithHeaders<SecurityConfig>('/as/v1/app/security/config', undefined, headers)
          securityConfig = cfg
          return cfg
        } catch {
          return null
        } finally {
          securityConfigPromise = null
        }
      })()
    }
    return securityConfigPromise
  }

  /** 检测风险并按策略裁剪（策略拉取失败时不裁剪） */
  async function detectRisksPruned(): Promise<RiskFlag[]> {
    const [cfg, flags] = await Promise.all([fetchSecurityConfig(), detectNativeRisks()])
    return pruneRiskFlagsByPolicy(mapRiskFlags(flags), cfg)
  }

  return {
    /**
     * 设备上报（使用初始化时缓存的设备信息）。
     * SEC-1：risk_flags 优先取桌面原生桥 DetectRisks 结果（映射服务端枚举、按策略开关裁剪），
     * 纯浏览器环境为空数组。若策略 risk_policy=block 且检测到风险，上报完成后触发
     * onRiskBlocked 回调（由宿主应用决定阻断 UI），上报本身不中断。
     */
    async report(params?: { user_id?: number }): Promise<void> {
      await client.ready
      const info = client.deviceInfo
      const riskFlags = await detectRisksPruned()
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
        risk_flags: riskFlags,
      }
      const headers = await buildSignHeaders(client.deviceId, client.appId, requireSecret(), JSON.stringify(body))
      await client.postWithHeaders('/as/v1/devices/report', body, headers)

      const cfg = securityConfig
      if (cfg?.risk_policy === 'block' && riskFlags.length > 0) {
        client.onRiskBlocked?.(riskFlags)
      }
    },

    /** 设备验证（使用初始化时缓存的设备码） */
    async verify(): Promise<DeviceVerifyResponse> {
      const body: Record<string, any> = {
        app_id: client.appId,
        device_id: client.deviceId,
      }
      const headers = await buildSignHeaders(client.deviceId, client.appId, requireSecret(), JSON.stringify(body))
      return client.postWithHeaders('/as/v1/devices/verify', body, headers)
    },

    /**
     * 桌面端风险检测（原生桥 DetectRisks → 服务端枚举映射 → 策略开关裁剪）。
     * 纯浏览器环境返回空数组；桥调用失败静默降级不抛错。
     */
    async detectRisks(): Promise<RiskFlag[]> {
      await client.ready
      return detectRisksPruned()
    },

    /**
     * 拉取桌面端安全策略（DeviceSign 鉴权，首次调用后缓存）。
     * force=true 强制刷新缓存。失败返回 null（调用方按默认策略 report 降级）。
     */
    async getSecurityConfig(force = false): Promise<SecurityConfig | null> {
      await client.ready
      if (force) {
        securityConfig = null
        securityConfigPromise = null
      }
      return fetchSecurityConfig()
    },
  }
}
