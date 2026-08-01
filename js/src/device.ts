import type { DeviceInfo, NativeDeviceInfo } from './types'

const DEVICE_ID_KEY = 'aohoyo_device_id'

/**
 * 获取原生设备信息（由 Wails/Tauri 注入到 window）
 */
function getNativeInfo(): NativeDeviceInfo {
  try {
    return (window as any).__NATIVE_DEVICE_INFO__ ?? {}
  } catch {
    return {}
  }
}

/**
 * 从 User-Agent 解析 OS 信息
 */
function parseOS(ua: string): { name: string; version: string } {
  if (ua.includes('Windows NT 10')) return { name: 'Windows', version: '10/11' }
  if (ua.includes('Windows NT 6.3')) return { name: 'Windows', version: '8.1' }
  if (ua.includes('Windows NT 6.1')) return { name: 'Windows', version: '7' }
  if (ua.includes('Mac OS X')) {
    const m = ua.match(/Mac OS X ([\d_]+)/)
    return { name: 'macOS', version: m ? m[1].replace(/_/g, '.') : '' }
  }
  if (ua.includes('Android')) {
    const m = ua.match(/Android ([\d.]+)/)
    return { name: 'Android', version: m ? m[1] : '' }
  }
  if (ua.includes('iPhone') || ua.includes('iPad')) {
    const m = ua.match(/OS ([\d_]+)/)
    return { name: 'iOS', version: m ? m[1].replace(/_/g, '.') : '' }
  }
  if (ua.includes('Linux')) return { name: 'Linux', version: '' }
  return { name: 'Unknown', version: '' }
}

/**
 * 检测系统主题
 */
function getTheme(): string {
  try {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
  } catch {
    return 'light'
  }
}

/**
 * 检测平台
 */
function detectPlatform(): string {
  const native = getNativeInfo()
  if (native.platform) return native.platform

  const ua = navigator.userAgent.toLowerCase()
  if (ua.includes('android')) return 'android'
  if (ua.includes('iphone') || ua.includes('ipad')) return 'ios'
  if (ua.includes('win')) return 'windows'
  if (ua.includes('mac')) return 'mac'
  if (ua.includes('linux')) return 'linux'
  return 'web'
}

/**
 * Canvas 指纹（多字体 + 多绘制操作，提高唯一性）
 */
function getCanvasFingerprint(): string {
  try {
    const canvas = document.createElement('canvas')
    canvas.width = 260
    canvas.height = 60
    const ctx = canvas.getContext('2d')
    if (!ctx) return ''
    // 文本渲染
    ctx.textBaseline = 'top'
    ctx.font = '14px Arial'
    ctx.fillStyle = '#f60'
    ctx.fillRect(50, 0, 100, 50)
    ctx.fillStyle = '#069'
    ctx.fillText('device_fp 🎵', 2, 15)
    ctx.font = '18px Georgia'
    ctx.fillStyle = 'rgba(102,204,0,0.7)'
    ctx.fillText('device_fp 🎵', 4, 35)
    // 弧线 + 阴影
    ctx.shadowColor = 'rgba(0,0,128,0.5)'
    ctx.shadowBlur = 5
    ctx.beginPath()
    ctx.arc(200, 30, 20, 0, Math.PI * 2)
    ctx.fill()
    return canvas.toDataURL()
  } catch {
    return ''
  }
}

/**
 * WebGL 指纹（vendor + renderer + 扩展列表）
 */
function getWebGLFingerprint(): string {
  try {
    const canvas = document.createElement('canvas')
    const gl = canvas.getContext('webgl') || canvas.getContext('experimental-webgl')
    if (!gl || !(gl instanceof WebGLRenderingContext)) return ''
    const parts: string[] = []
    const debugInfo = gl.getExtension('WEBGL_debug_renderer_info')
    if (debugInfo) {
      parts.push(gl.getParameter(debugInfo.UNMASKED_VENDOR_WEBGL))
      parts.push(gl.getParameter(debugInfo.UNMASKED_RENDERER_WEBGL))
    }
    // 扩展列表（不同驱动/硬件支持不同扩展）
    const extensions = gl.getSupportedExtensions()
    if (extensions) parts.push(extensions.sort().join(','))
    return parts.join('|')
  } catch {
    return ''
  }
}

/**
 * 字体探测（通过 Canvas 测量不同字体的渲染宽度差异）
 */
function detectFonts(): string {
  const testFonts = [
    'Arial', 'Helvetica', 'Times New Roman', 'Courier New', 'Georgia',
    'Verdana', 'Comic Sans MS', 'Impact', 'Trebuchet MS', 'Palatino',
    'Lucida Console', 'Tahoma', 'Segoe UI', 'Calibri', 'Cambria',
    'Consolas', 'Monaco', 'Menlo', 'PingFang SC', 'Microsoft YaHei',
    'SimSun', 'STHeiti', 'Hiragino Sans GB', 'WenQuanYi Micro Hei',
    'Noto Sans CJK SC', 'Source Han Sans SC', 'Droid Sans Fallback',
  ]
  const baseFont = 'monospace'
  const testStr = 'mmmmmmmmmmlli'
  try {
    const canvas = document.createElement('canvas')
    const ctx = canvas.getContext('2d')
    if (!ctx) return ''
    ctx.font = `72px ${baseFont}`
    const baseWidth = ctx.measureText(testStr).width
    const available: string[] = []
    for (const font of testFonts) {
      ctx.font = `72px "${font}", ${baseFont}`
      if (ctx.measureText(testStr).width !== baseWidth) {
        available.push(font)
      }
    }
    return available.join(',')
  } catch {
    return ''
  }
}

/**
 * AudioContext 指纹（不同硬件/驱动音频处理有微小差异）
 */
function getAudioFingerprint(): string {
  try {
    const AudioCtx = window.OfflineAudioContext || (window as any).webkitOfflineAudioContext
    if (!AudioCtx) return ''
    const ctx = new AudioCtx(1, 44100, 44100)
    const oscillator = ctx.createOscillator()
    oscillator.type = 'triangle'
    oscillator.frequency.setValueAtTime(10000, ctx.currentTime)
    const compressor = ctx.createDynamicsCompressor()
    compressor.threshold.setValueAtTime(-50, ctx.currentTime)
    compressor.knee.setValueAtTime(40, ctx.currentTime)
    compressor.ratio.setValueAtTime(12, ctx.currentTime)
    compressor.attack.setValueAtTime(0, ctx.currentTime)
    compressor.release.setValueAtTime(0.25, ctx.currentTime)
    oscillator.connect(compressor)
    compressor.connect(ctx.destination)
    oscillator.start(0)
    // 同步方式无法等待渲染完成，取参数作为指纹
    const params = [
      compressor.threshold.value,
      compressor.knee.value,
      compressor.ratio.value,
      compressor.attack.value,
      compressor.release.value,
      oscillator.frequency.value,
    ]
    return params.join(',')
  } catch {
    return ''
  }
}

/**
 * 浏览器指纹生成设备码（Web 端兜底方案）
 * 采集多维度信息生成确定性 SHA256
 */
async function generateBrowserDeviceId(): Promise<string> {
  const components: string[] = []

  // 1. User-Agent
  components.push(navigator.userAgent)

  // 2. Canvas 指纹
  components.push(getCanvasFingerprint())

  // 3. WebGL 指纹
  components.push(getWebGLFingerprint())

  // 4. 字体探测
  components.push(detectFonts())

  // 5. 屏幕属性
  components.push(`${screen.width}x${screen.height}x${screen.colorDepth}`)
  components.push(`${screen.availWidth}x${screen.availHeight}`)

  // 6. 硬件信息
  components.push(String(navigator.hardwareConcurrency || 0))
  const nav = navigator as any
  if (nav.deviceMemory) components.push(String(nav.deviceMemory))

  // 7. 触摸支持
  components.push(String(navigator.maxTouchPoints || 0))

  // 8. 时区 + 语言
  components.push(Intl.DateTimeFormat().resolvedOptions().timeZone)
  components.push(navigator.language)
  components.push(navigator.languages?.join(',') || '')

  // 9. 平台
  components.push(navigator.platform)

  // 10. AudioContext 指纹
  components.push(getAudioFingerprint())

  // SHA256 哈希（非 HTTPS 环境下 crypto.subtle 不可用，走简单 hash 兜底）
  const data = components.join('|')
  try {
    const hash = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(data))
    const hex = Array.from(new Uint8Array(hash))
      .map(b => b.toString(16).padStart(2, '0'))
      .join('')
    return `dev_${hex.substring(0, 32)}`
  } catch {
    // crypto.subtle 不可用（HTTP 页面），用简单 DJB2 哈希兜底
    let h = 5381
    for (let i = 0; i < data.length; i++) {
      h = ((h << 5) + h) ^ data.charCodeAt(i)
    }
    const hex = (h >>> 0).toString(16).padStart(8, '0')
    return `dev_${hex.repeat(4).substring(0, 32)}`
  }
}

/** 缓存的设备码 */
let cachedDeviceId = ''

/**
 * 异步初始化设备码（SDK 构造时调用一次）
 * PC/移动端：读取原生注入
 * Web 端：生成浏览器指纹
 */
export async function initDeviceId(): Promise<string> {
  const native = getNativeInfo()
  if (native.device_id) {
    cachedDeviceId = native.device_id
    return cachedDeviceId
  }

  // 尝试从 localStorage 缓存读取
  try {
    const cached = localStorage.getItem(DEVICE_ID_KEY)
    if (cached) {
      cachedDeviceId = cached
      return cachedDeviceId
    }
  } catch { /* ignore */ }

  // 生成浏览器指纹
  cachedDeviceId = await generateBrowserDeviceId()
  try {
    localStorage.setItem(DEVICE_ID_KEY, cachedDeviceId)
  } catch { /* ignore */ }
  return cachedDeviceId
}

/**
 * 采集设备信息（原生注入优先，浏览器 API 兜底）
 */
export function getDeviceInfo(): DeviceInfo {
  const native = getNativeInfo()
  const ua = navigator.userAgent
  const os = parseOS(ua)

  return {
    ua: native.ua ?? ua,
    device_id: native.device_id ?? cachedDeviceId,
    os_name: native.os_name ?? os.name,
    os_version: native.os_version ?? os.version,
    os_language: native.os_language ?? navigator.language,
    os_theme: native.os_theme ?? getTheme(),
    vendor: native.vendor ?? '',
    model: native.model ?? '',
    platform: detectPlatform(),
  }
}
