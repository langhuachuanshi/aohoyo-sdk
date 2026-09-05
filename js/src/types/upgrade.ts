/** 版本升级检查请求参数 */
export interface UpgradeCheckParams {
  app_id: string
  current_version_code: number
  /**
   * 平台（windows/macos/linux/android/ios）。强烈建议必传：
   * 服务端按平台取「该平台有包的最新版本」（平台独立节奏，Windows 与安卓/iOS 各自发版互不干扰），
   * 不传回退全局最新 + 第一个包（仅兼容旧版本，多平台会拿错包）。
   */
  platform?: string
  /** 渠道码（灰度渠道白名单） */
  channel_code?: string
  /** 设备码——参与确定性灰度分配（crc32 % 100），接入灰度必须传 */
  device_id?: string
}

/** 版本升级检查响应 */
export interface UpgradeCheckResponse {
  has_update: boolean
  /** 强制更新：true 时宿主必须阻断用户操作 */
  force_update: boolean
  latest_version: string
  latest_version_code: number
  platform: string
  /** 空字符串 = 该平台暂无安装包（提示有新版但不可下载），≠ 无更新 */
  download_url: string
  update_log: string
  file_size: number
  md5: string
  sha256: string
  /**
   * 清单 HMAC-SHA256 签名（应用开启 upgrade_signature 时返回）。
   * JS 层仅透传——app_secret 不下发 WebView，验签由宿主原生层（rust verify_upgrade / go VerifyUpgrade）完成。
   */
  signature?: string
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
