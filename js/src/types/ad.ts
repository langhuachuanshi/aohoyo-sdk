/**
 * 广告模块类型（与 admin-server /as/v1/ads 客户端接口对应，服务端已脱敏不含广告主信息）
 */

/** 广告类型（与 ad_positions.ad_type 对应）：1=文字 2=图片 3=弹窗 4=开屏 5=横幅 */
export type AdType = 1 | 2 | 3 | 4 | 5

/** 客户端广告项 */
export interface AdItem {
  id: number
  position_id: number
  /** 广告标题（文字广告即内容，其他类型作 alt） */
  title: string
  /** 文字广告内容 */
  content: string
  /** 图片地址（图片/开屏/横幅） */
  image_url: string
  /** 点击跳转链接 */
  link_url: string
  /** 广告类型（来自广告位） */
  ad_type: AdType
  /** 建议宽度（图片/开屏/横幅） */
  width: number
  /** 建议高度 */
  height: number
  start_time: string | null
  end_time: string | null
  /** 广告级扩展配置（展示上限/频控等，由后台投放时约定） */
  config: Record<string, any> | null
}

/** 拉取广告响应：按广告位 code 分组，key = position code（如 splash、home-banner） */
export interface ClientAdsResponse {
  positions: Record<string, AdItem[]>
}

/** 拉取广告参数 */
export interface GetAdsParams {
  /** 指定广告位 code，不传返回该应用全部启用广告位 */
  position?: string
}

/** 曝光/点击上报参数 */
export interface AdReportParams {
  /** 设备码，缺省用 client.deviceId（建议传机器指纹保证统计口径稳定） */
  device_id?: string
}
