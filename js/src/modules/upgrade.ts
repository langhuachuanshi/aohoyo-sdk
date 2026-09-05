import type { SdkClient } from '../client'
import type { UpgradeCheckResponse, UpgradeStrategy } from '../types/upgrade'

export function createUpgradeModule(client: SdkClient) {
  let timer: ReturnType<typeof setTimeout> | null = null

  return {
    /**
     * 检测是否需要升级（POST /as/v1/upgrade/check，公开接口）。
     *
     * - platform 取自 SdkConfig.platform——强烈建议配置：服务端按平台取该平台最新可用版本（平台独立节奏）
     * - Web 应用场景：has_update=true 时提示刷新页面即可（Web 无安装包概念），
     *   下载/校验/安装由桌面端原生 SDK（rust/go native）完成
     * - 响应 signature 字段仅透传，验签由宿主原生层做（app_secret 不进 WebView）
     */
    checkUpgrade(): Promise<UpgradeCheckResponse> {
      return client.post('/as/v1/upgrade/check', {
        app_id: client.appId,
        current_version_code: client.versionCode,
        platform: client.platform,
        channel_code: client.channelCode,
        device_id: client.deviceId,
      })
    },

    /** 获取升级策略（应用最新已发布版本，调试用；跳过灰度） */
    getStrategy(): Promise<UpgradeStrategy | null> {
      return client.get(`/as/v1/upgrade/strategy/${client.appId}`)
    },

    /** 启动自动检查更新 */
    startAutoCheck(config?: {
      intervalMs?: number
      onUpdate?: (resp: UpgradeCheckResponse) => void
      onForceUpdate?: (resp: UpgradeCheckResponse) => void
    }) {
      this.stopAutoCheck()
      const intervalMs = config?.intervalMs ?? 1_800_000 // 30 分钟
      const check = async () => {
        try {
          const resp = await this.checkUpgrade()
          if (resp.has_update) {
            if (resp.force_update) {
              config?.onForceUpdate?.(resp)
            } else {
              config?.onUpdate?.(resp)
            }
          }
        } catch { /* 忽略网络错误 */ }
        const jitter = Math.random() * 60_000 // ±1 分钟抖动
        timer = setTimeout(check, intervalMs + jitter)
      }
      check()
    },

    /** 停止自动检查更新 */
    stopAutoCheck() {
      if (timer) { clearTimeout(timer); timer = null }
    },
  }
}
