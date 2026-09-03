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
  // SDK-4：错误自动捕获已下线（autoError 保留为兼容字段，不再生效）
  // AW-2：session_end 覆盖式 checkpoint 周期，0 = 关闭
  const checkpointInterval = config?.checkpointInterval ?? 60000

  const queue: StatsEvent[] = []
  let sessionStart = 0
  let timer: ReturnType<typeof setInterval> | null = null
  let checkpointTimer: ReturnType<typeof setInterval> | null = null
  /** 同一时刻只允许一条在途 checkpoint，避免 duration 重复计入 */
  let checkpointInFlight = false
  let initialized = false

  // 缓存设备信息（init 时填充）
  let cachedPlatform = 'web'
  let cachedOs = ''
  let cachedBrowser = ''
  let cachedScreenW = 0
  let cachedScreenH = 0
  let cachedDeviceId = ''

  // 事件监听器引用（destroy 时移除）
  let onVisibilityHandler: (() => void) | null = null
  let onBeforeUnloadHandler: (() => void) | null = null

  /** 采集设备公共字段 */
  function commonFields(): Partial<StatsEvent> {
    return {
      app_id: client.appId,
      platform: cachedPlatform,
      device_id: cachedDeviceId,
      app_version: client.versionCode ? String(client.versionCode) : undefined,
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
    restartCheckpointTimer()
  }

  /**
   * 覆盖式时长 checkpoint（AW-2）：会话进行中周期性发一条 session_end（同一 session_id、
   * duration = 自会话起的累计秒数）。移动端/崩溃/杀进程时 beforeunload 不可靠，
   * 服务端按 session_id 聚合取后到的更大值，兜底时长不丢。
   */
  function sendCheckpoint(): void {
    if (!client.sessionId || !sessionStart) return
    if (checkpointInFlight) return
    checkpointInFlight = true
    const duration = Math.round((Date.now() - sessionStart) / 1000)
    enqueue({ event_type: 'session_end', duration })
    flush().finally(() => { checkpointInFlight = false })
  }

  /** 重置 checkpoint 周期（会话启动/页面重新可见时对齐） */
  function restartCheckpointTimer(): void {
    if (checkpointTimer) { clearInterval(checkpointTimer); checkpointTimer = null }
    if (!checkpointInterval) return
    checkpointTimer = setInterval(() => sendCheckpoint(), checkpointInterval)
  }

  /** 停止 checkpoint 周期（会话结束/模块销毁时） */
  function stopCheckpointTimer(): void {
    if (checkpointTimer) { clearInterval(checkpointTimer); checkpointTimer = null }
    checkpointInFlight = false
  }

  /** 结束会话（最终一条 session_end，覆盖所有 checkpoint） */
  function endSession(useBeacon = false): void {
    if (!client.sessionId) return
    stopCheckpointTimer()
    const duration = Math.round((Date.now() - sessionStart) / 1000)
    enqueue({ event_type: 'session_end', duration })
    client.sessionId = ''
    sessionStart = 0
    if (useBeacon) flushBeacon()
    else flush()
  }

  return {
    /** 初始化：缓存设备信息、启动定时上报与时长 checkpoint、注册页面生命周期监听 */
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

      // 页面隐藏时：先落一条 checkpoint（移动端可能直接杀进程），再 flush 队列；
      // 重新可见时对齐 checkpoint 周期（后台定时器被节流，重计 60s 更准）
      if (typeof document !== 'undefined') {
        onVisibilityHandler = () => {
          if (document.visibilityState === 'hidden') {
            sendCheckpoint()
            flush()
          } else {
            restartCheckpointTimer()
          }
        }
        document.addEventListener('visibilitychange', onVisibilityHandler)
      }

      // 页面关闭前结束会话（最终时长）
      if (typeof window !== 'undefined') {
        onBeforeUnloadHandler = () => endSession(true)
        window.addEventListener('beforeunload', onBeforeUnloadHandler)
      }

      initialized = true
    },

    /** 销毁 */
    destroy(): void {
      if (!initialized) return
      endSession()
      if (timer) { clearInterval(timer); timer = null }
      stopCheckpointTimer()
      if (onVisibilityHandler) document.removeEventListener('visibilitychange', onVisibilityHandler)
      if (onBeforeUnloadHandler) window.removeEventListener('beforeunload', onBeforeUnloadHandler)
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

    /**
     * 手动上报错误。
     * @deprecated SDK-4：全局错误自动捕获已下线（window.onerror/unhandledrejection 不再监听），
     * error 事件类型仅为协议兼容保留；如需错误收集请业务侧自建。
     */
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
