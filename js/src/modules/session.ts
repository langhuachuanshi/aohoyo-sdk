import type { SdkClient } from '../client'

/** 生成随机间隔（1~5 分钟） */
function randomInterval(): number {
  return 60_000 + Math.random() * 240_000
}

export function createSessionModule(client: SdkClient) {
  let timer: ReturnType<typeof setTimeout> | null = null
  /** 会话开始时间戳，用于计算 duration */
  let sessionStart: number | null = null
  /** 是否已注册卸载监听 */
  let unloadRegistered = false

  /** 生成简单 session ID */
  function newSessionId(): string {
    return `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
  }

  /** 上报会话结束（计算时长，单位：秒） */
  async function reportSessionEnd(useBeacon = false) {
    if (!sessionStart || !client.sessionId) return
    // 单位统一为秒：与 stats 模块（stats.ts）的 session_end duration 口径一致，
    // 避免 avg_duration 聚合时毫秒/秒混淆污染。
    const duration = Math.round((Date.now() - sessionStart) / 1000)
    const payload = {
      app_id: client.appId,
      session_id: client.sessionId,
      duration,
    }
    try {
      if (useBeacon) {
        const url = `${client.baseURL}/uc/v1/auth/track/end`
        if (typeof navigator !== 'undefined' && navigator.sendBeacon) {
          navigator.sendBeacon(url, JSON.stringify(payload))
        }
      } else {
        await client.post('/uc/v1/auth/track/end', payload)
      }
    } catch {
      /* 静默失败，不影响退出 */
    }
    sessionStart = null
    client.sessionId = ''
  }

  /** heartbeat 模式：心跳保活 */
  const scheduleNext = () => {
    timer = setTimeout(async () => {
      try {
        await client.post('/uc/v1/auth/heartbeat', { app_id: client.appId })
        scheduleNext()
      } catch {
        timer = null
      }
    }, randomInterval())
  }

  function startHeartbeat() {
    stopHeartbeat()
    client.post('/uc/v1/auth/heartbeat', { app_id: client.appId })
      .then(() => scheduleNext())
      .catch(() => { /* 401 已被 client 拦截器处理 */ })
  }

  /** login 模式：记录会话开始时间，页面关闭时上报 */
  function markSessionStart() {
    sessionStart = Date.now()
    // 复用 stats 模块可能已生成的 client.sessionId；若尚未生成（stats 未 init），
    // 此处生成并写入，后续 stats 模块 startSession 时会沿用同一个 ID（session_id 统一）。
    if (!client.sessionId) {
      client.sessionId = newSessionId()
    }
  }

  function stopHeartbeat() {
    if (timer) {
      clearTimeout(timer)
      timer = null
    }
  }

  /** 监听页面关闭，上报时长（login / open 模式共用） */
  function setupUnloadHandler() {
    if (unloadRegistered || typeof window === 'undefined') return
    unloadRegistered = true
    window.addEventListener('beforeunload', () => {
      reportSessionEnd(true)
    })
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'hidden') {
        reportSessionEnd()
      }
    })
  }

  return {
    /**
     * 根据会话模式启动对应行为。登录成功后或 SDK 初始化时调用。
     * - heartbeat：定时心跳保活
     * - login / open：记录会话开始，页面关闭时上报时长
     *   （使用次数/日活等统计由 stats 模块经 /stats/events 上报 session_start/session_end）
     */
    start() {
      this.stop()

      switch (client.sessionMode) {
        case 'heartbeat':
          startHeartbeat()
          break
        case 'login':
        case 'open':
          markSessionStart()
          setupUnloadHandler()
          break
      }
    },

    /** 停止所有行为。登出时调用 */
    stop() {
      stopHeartbeat()
    },

    /** 上报会话结束并清理。主动退出时调用 */
    async end() {
      await reportSessionEnd()
      this.stop()
    },
  }
}
