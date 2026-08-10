import type { SdkClient } from '../client'
import type { StatsEvent, StatsConfig } from '../types/stats'

/** 从 User-Agent 提取浏览器名称 */
function parseBrowser(ua: string): string {
  if (ua.includes('Edg/')) return 'Edge'
  if (ua.includes('Chrome/') && !ua.includes('Edg/')) return 'Chrome'
  if (ua.includes('Firefox/')) return 'Firefox'
  if (ua.includes('Safari/') && !ua.includes('Chrome/')) return 'Safari'
  if (ua.includes('Opera') || ua.includes('OPR/')) return 'Opera'
  return 'Unknown'
}

/** 生成 session ID */
function generateSessionId(): string {
  return 'ss_' + Date.now().toString(36) + '_' + Math.random().toString(36).slice(2, 8)
}

export function createStatsModule(client: SdkClient, config?: StatsConfig) {
  const reportInterval = config?.reportInterval ?? 10000
  const batchSize = config?.batchSize ?? 50
  const autoError = config?.autoError !== false

  const queue: StatsEvent[] = []
  let sessionStart = 0
  let timer: ReturnType<typeof setInterval> | null = null
  let initialized = false

  // 缓存设备信息（init 时填充）
  let cachedPlatform = 'web'
  let cachedOs = ''
  let cachedBrowser = ''
  let cachedScreenW = 0
  let cachedScreenH = 0
  let cachedDeviceId = ''

  // 事件监听器引用（destroy 时移除）
  let onErrorHandler: EventListener | null = null
  let onRejectionHandler: ((e: PromiseRejectionEvent) => void) | null = null
  let onVisibilityHandler: (() => void) | null = null
  let onBeforeUnloadHandler: (() => void) | null = null

  /** 采集设备公共字段 */
  function commonFields(): Partial<StatsEvent> {
    return {
      app_id: client.appId,
      platform: cachedPlatform,
      device_id: cachedDeviceId,
      os: cachedOs,
      browser: cachedBrowser,
      screen_w: cachedScreenW,
      screen_h: cachedScreenH,
    }
  }

  /** 推入事件队列 */
  function enqueue(event: StatsEvent): void {
    queue.push({
      ...commonFields(),
      session_id: client.sessionId || undefined,
      user_id: client.userId || undefined,
      client_ts: Date.now(),
      ...event,
    })
    if (queue.length >= batchSize) flush()
  }

  /** 批量上报 */
  async function flush(): Promise<void> {
    if (!queue.length) return
    const batch = queue.splice(0, batchSize)
    try {
      await client.post('/as/v1/stats/events', { events: batch })
    } catch {
      // 静默失败，不影响业务
    }
  }

  /** 使用 sendBeacon 同步发送（beforeunload 场景，浏览器不保证 fetch 完成） */
  function flushBeacon(): void {
    if (!queue.length) return
    const batch = queue.splice(0, batchSize)
    try {
      const payload = JSON.stringify({ events: batch })
      const url = `${client.baseURL}/as/v1/stats/events`
      if (typeof navigator !== 'undefined' && navigator.sendBeacon) {
        navigator.sendBeacon(url, payload)
      }
    } catch {
      // 静默失败
    }
  }

  /** 开始会话 */
  function startSession(): void {
    // 统一写入 client.sessionId，session 模块复用同一 ID（见错位4：session_id 统一）
    client.sessionId = generateSessionId()
    sessionStart = Date.now()
    enqueue({ event_type: 'session_start' })
  }

  /** 结束会话 */
  function endSession(useBeacon = false): void {
    if (!client.sessionId) return
    const duration = Math.round((Date.now() - sessionStart) / 1000)
    enqueue({ event_type: 'session_end', duration })
    client.sessionId = ''
    sessionStart = 0
    if (useBeacon) flushBeacon()
    else flush()
  }

  return {
    /** 初始化：缓存设备信息、启动定时上报、注册全局监听 */
    init(): void {
      if (initialized) return

      const info = client.deviceInfo
      const ua = info.ua || (typeof navigator !== 'undefined' ? navigator.userAgent : '')
      cachedDeviceId = client.deviceId
      cachedPlatform = info.platform || 'web'
      cachedOs = info.os_name || 'Unknown'
      cachedBrowser = parseBrowser(ua)
      if (typeof screen !== 'undefined') {
        cachedScreenW = screen.width
        cachedScreenH = screen.height
      }

      startSession()

      // 定时 flush
      timer = setInterval(() => flush(), reportInterval)

      // 页面隐藏时 flush
      if (typeof document !== 'undefined') {
        onVisibilityHandler = () => {
          if (document.visibilityState === 'hidden') flush()
        }
        document.addEventListener('visibilitychange', onVisibilityHandler)
      }

      // 页面关闭前结束会话
      if (typeof window !== 'undefined') {
        onBeforeUnloadHandler = () => endSession(true)
        window.addEventListener('beforeunload', onBeforeUnloadHandler)
      }

      // 自动采集 JS 错误
      if (autoError && typeof window !== 'undefined') {
        onErrorHandler = ((event: ErrorEvent) => {
          enqueue({
            event_type: 'error',
            error_msg: event.message || String(event),
            error_stack: event.error?.stack || '',
            path: typeof location !== 'undefined' ? location.pathname : '',
          })
        }) as EventListener
        window.addEventListener('error', onErrorHandler)

        onRejectionHandler = (e: PromiseRejectionEvent) => {
          const reason = e.reason
          enqueue({
            event_type: 'error',
            error_msg: reason?.message || String(reason),
            error_stack: reason?.stack || '',
            path: typeof location !== 'undefined' ? location.pathname : '',
          })
        }
        window.addEventListener('unhandledrejection', onRejectionHandler)
      }

      initialized = true
    },

    /** 销毁 */
    destroy(): void {
      if (!initialized) return
      endSession()
      if (timer) { clearInterval(timer); timer = null }
      if (onVisibilityHandler) document.removeEventListener('visibilitychange', onVisibilityHandler)
      if (onBeforeUnloadHandler) window.removeEventListener('beforeunload', onBeforeUnloadHandler)
      if (onErrorHandler) window.removeEventListener('error', onErrorHandler)
      if (onRejectionHandler) window.removeEventListener('unhandledrejection', onRejectionHandler)
      initialized = false
    },

    /** 上报页面浏览（在 Vue Router afterEach 中调用） */
    trackPageView(path: string, title?: string): void {
      enqueue({
        event_type: 'page_view',
        path,
        title: title || (typeof document !== 'undefined' ? document.title : ''),
        referrer: typeof document !== 'undefined' ? document.referrer : '',
      })
    },

    /** 上报自定义事件 */
    trackEvent(name: string, params?: Record<string, any>): void {
      enqueue({
        event_type: 'custom',
        extra: { name, ...params },
        path: typeof location !== 'undefined' ? location.pathname : '',
      })
    },

    /** 上报错误 */
    trackError(error: Error | string, stack?: string): void {
      enqueue({
        event_type: 'error',
        error_msg: typeof error === 'string' ? error : error.message,
        error_stack: stack || (error instanceof Error ? error.stack : ''),
        path: typeof location !== 'undefined' ? location.pathname : '',
      })
    },

    /** 设置用户 ID（登录后调用）。实际写入 client.userId，与 token 生命周期绑定 */
    setUserId(id: string): void {
      client.userId = id
    },

    /** 清除用户 ID（登出时调用）。client.clearTokens() 也会自动清除 */
    clearUserId(): void {
      client.userId = ''
    },
  }
}
