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
  /** 是否自动采集 JS 错误，默认 true */
  autoError?: boolean
}
