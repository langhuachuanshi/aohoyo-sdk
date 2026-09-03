/** 统计事件类型（对应后端 StatsEventReport.event_type） */
export type StatsEventType = 'page_view' | 'session_start' | 'session_end' | 'error' | 'custom'

/** 单条统计事件 */
export interface StatsEvent {
  event_type: StatsEventType
  app_id?: string
  user_id?: string
  session_id?: string
  platform?: string
  device_id?: string
  /** 应用版本号（与 device.report 的 app_version 取值对齐；服务端加列前静默忽略） */
  app_version?: string
  os?: string
  browser?: string
  screen_w?: number
  screen_h?: number
  path?: string
  title?: string
  referrer?: string
  error_msg?: string
  error_stack?: string
  duration?: number
  extra?: Record<string, any>
  client_ts?: number
}

/** stats 模块配置 */
export interface StatsConfig {
  /** 上报间隔（毫秒），默认 10000 */
  reportInterval?: number
  /** 单次最大上报条数，默认 50（与后端 StatsEventBatch 一致） */
  batchSize?: number
  /** @deprecated SDK-4：错误自动捕获已下线，此选项不再生效（保留字段仅为兼容） */
  autoError?: boolean
  /**
   * session_end 覆盖式 checkpoint 周期（毫秒），默认 60000。
   * 会话进行中周期性上报累计时长（同一 session_id，后到更大值覆盖先到的），
   * 兜底移动端/崩溃/杀进程导致的 session_end 丢失；0 = 关闭。
   */
  checkpointInterval?: number
}
