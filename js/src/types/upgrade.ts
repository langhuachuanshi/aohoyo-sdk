/** 版本升级检查请求参数 */
export interface UpgradeCheckParams {
  app_id: string
  current_version_code: number
  platform?: string
  channel_code?: string
  device_id?: string
}

/** 版本升级检查响应 */
export interface UpgradeCheckResponse {
  has_update: boolean
  force_update: boolean
  latest_version: string
  latest_version_code: number
  platform: string
  download_url: string
  update_log: string
  file_size: number
  md5: string
  sha256: string
}

/** 升级策略（应用最新已发布版本） */
export interface UpgradeStrategy {
  id: number
  app_id: string
  version_code: number
  version_name: string
  download_url: string
  file_size: number
  md5: string
  sha256: string
  update_log: string
  force_update: boolean
  gray_ratio: number
  status: number
}

/** 自动检查更新配置 */
export interface AutoCheckConfig {
  app_id: string
  current_version_code: number
  channel_code?: string
  /** 检查间隔毫秒，默认 30 分钟 */
  intervalMs?: number
  /** 发现可更新版本时回调 */
  onUpdate?: (resp: UpgradeCheckResponse) => void
  /** 发现强制更新时回调 */
  onForceUpdate?: (resp: UpgradeCheckResponse) => void
}
