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
