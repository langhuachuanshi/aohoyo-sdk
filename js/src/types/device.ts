/** 设备信息（对齐后端 DeviceRegisterParams） */
export interface DeviceInfo {
  /** User-Agent */
  ua: string
  /** 设备码（硬件指纹/原生注入，唯一标识物理设备） */
  device_id: string
  /** 操作系统名称 */
  os_name: string
  /** 操作系统版本 */
  os_version: string
  /** 系统语言 */
  os_language: string
  /** 系统主题 light / dark */
  os_theme: string
  /** 设备厂商 */
  vendor: string
  /** 推送客户端 ID */
  push_clientid?: string
  /** IMEI（移动端） */
  imei?: string
  /** OAID（Android 广告 ID） */
  oaid?: string
  /** IDFA（iOS 广告 ID） */
  idfa?: string
  /** 设备型号 */
  model: string
  /** 平台：web / windows / mac / linux / ios / android */
  platform: string
}

/** 原生设备信息（Wails/Tauri 注入） */
export interface NativeDeviceInfo {
  os_name?: string
  os_version?: string
  os_language?: string
  os_theme?: string
  vendor?: string
  model?: string
  platform?: string
  ua?: string
  /** 设备码（由原生客户端注入） */
  device_id?: string
}

/** 设备上报请求参数 */
export interface DeviceReportParams {
  /** 应用 ID */
  app_id: string
  /** 应用密钥（由调用方从后端获取） */
  app_secret: string
  /** 用户 ID（可选） */
  user_id?: number
  /** 渠道代码（可选） */
  channel_code?: string
}

/** 设备验证请求参数 */
export interface DeviceVerifyParams {
  /** 应用 ID */
  app_id: string
  /** 应用密钥（由调用方从后端获取） */
  app_secret: string
}

/** 设备验证响应 */
export interface DeviceVerifyResponse {
  device_id: string
  /** 1=正常 2=封禁 3=观察 */
  status: number
  /** 0=正常 1=模拟器 2=多开 3=Root 4=Hook 5=异常签名 */
  risk_type: number
  is_safe: boolean
}

/**
 * 设备风险标志（devices/report 的 risk_flags，服务端 risk.Analyze 接受的枚举）。
 * sign 为服务端判定项（异常签名），客户端检测不产生。
 */
export type RiskFlag = 'emulator' | 'multiopen' | 'root' | 'hook' | 'sign'

/** 桌面端安全策略下发（POST /as/v1/app/security/config 响应，DeviceSign 鉴权） */
export interface SecurityConfig {
  app_id: string
  /** 1=启用 */
  status: number
  anti_debug: boolean
  anti_multi_open: boolean
  emulator_detect: boolean
  hook_detect: boolean
  root_detect: boolean
  replay_protection: boolean
  integrity_check: boolean
  max_devices: number
  cert_pin: string
  license_mode: string
  offline_grace_days: number
  /** report=只上报（默认） observe=上报观察 block=检测到风险时回调阻断 */
  risk_policy: 'report' | 'observe' | 'block'
  upgrade_signature: string
  config_json: string
}
