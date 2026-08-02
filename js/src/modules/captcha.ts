import type { AliyunCaptchaInitConfig, AliyunCaptchaCallbacks } from '../types'
import type { SdkClient } from '../client'

/** 验证码场景（与 admin-server captcha provider 的 scene 对齐） */
export type CaptchaScene = 'login' | 'register' | 'reset_password' | 'send_code' | 'bind'

/** GET /captcha/config 返回 —— 前端初始化阿里云滑块所需配置（不含 AccessKey/Secret/ekey） */
export interface CaptchaConfig {
  mode: 'aliyun' | 'image'
  prefix?: string
  scene_id?: string
  region?: string
  encrypted_scene_id?: string
}

/** POST /captcha/create 返回 —— 图片验证码（mode=image 回退，或独立图形验证码 API） */
export interface ImageCaptcha {
  captcha_id: string
  captcha_base64: string
  /** 模式：char=字符 / math=数学（数学模式时前端需展示算式，让用户输入计算结果） */
  mode?: 'char' | 'math'
}

/** createImage 可选参数 */
export interface CreateImageOptions {
  /** 模式：char=字符 / math=数学；留空按服务商配置（默认 char） */
  mode?: 'char' | 'math'
  /** 应用 ID：按应用读取图形验证码服务商配置（模式/宽高等），留空用全局 */
  app_id?: string
  width?: number
  height?: number
  size?: number
  noise?: number
}

/** SDK 初始化参数（内部类型，与阿里云 JS SDK 对齐） */
interface AliyunCaptchaSDKParams {
  SceneId: string
  EncryptedSceneId?: string
  mode: 'embed' | 'popup'
  element?: string
  button?: string
  slideStyle?: { width?: number; height?: number }
  onBizSuccess?: () => void
  success: (captchaVerifyParam: string) => void | Promise<void>
  fail: (result: unknown) => void
  getInstance?: (instance: unknown) => void
}

/** Aliyun Captcha JS SDK CDN 地址 */
const ALIYUN_CAPTCHA_SDK_URL = 'https://o.alicdn.com/captcha-frontend/aliyunCaptcha/AliyunCaptcha.js'

/** SDK 加载 Promise（单例，全局只加载一次） */
let sdkLoadPromise: Promise<void> | null = null

declare global {
  interface Window {
    AliyunCaptchaConfig?: { region: string; prefix: string }
    initAliyunCaptcha?: (params: AliyunCaptchaSDKParams) => void
  }
}

/**
 * 创建验证码模块
 *
 * 封装阿里云验证码 JS SDK 的加载初始化 + 两个公开接口（取配置 / 取图片验证码），
 * 让客户端统一走 `sdk.captcha.*`，不再自己 fetch。
 *
 * @example
 * ```ts
 * const sdk = createSdk({ baseURL: 'https://api.aohoyo.com', app_id: '...' })
 *
 * const cfg = await sdk.captcha.getConfig('login')
 * if (cfg.mode === 'aliyun') {
 *   sdk.captcha.initAliyun(
 *     { sceneId: cfg.scene_id!, prefix: cfg.prefix!, region: cfg.region!, mode: 'embed', element: '#box' },
 *     { onSuccess: async (param) => {
 *         await sdk.auth.sendCode({ scene: 'login', type: 'phone', target, captcha_verify_param: param })
 *     } },
 *   )
 * } else {
 *   const img = await sdk.captcha.createImage()
 *   // 展示 img.captcha_base64，用户输入后 sendCode({ captcha_id: img.captcha_id, captcha_code })
 * }
 * ```
 */
export function createCaptchaModule(client: SdkClient) {
  return {
    /**
     * 加载阿里云验证码 JS SDK（自动去重，全局只加载一次）
     */
    async loadSDK(): Promise<void> {
      if (sdkLoadPromise) return sdkLoadPromise

      if (window.initAliyunCaptcha) return

      sdkLoadPromise = new Promise<void>((resolve, reject) => {
        const script = document.createElement('script')
        script.src = ALIYUN_CAPTCHA_SDK_URL
        script.onload = () => resolve()
        script.onerror = () => {
          sdkLoadPromise = null
          reject(new Error('加载阿里云验证码 SDK 失败'))
        }
        document.head.appendChild(script)
      })

      return sdkLoadPromise
    },

    /**
     * 初始化阿里云验证码
     *
     * 自动加载 SDK（如果还没加载），设置全局配置，调用 initAliyunCaptcha。
     *
     * @param config 初始化配置（场景 ID、身份标、区域等）
     * @param callbacks 回调（验证成功/失败）
     */
    async initAliyun(config: AliyunCaptchaInitConfig, callbacks: AliyunCaptchaCallbacks): Promise<void> {
      await this.loadSDK()

      if (!window.initAliyunCaptcha) {
        throw new Error('阿里云验证码 SDK 加载失败')
      }

      // 设置全局配置
      const regionShort = config.region === 'ap-southeast-1' ? 'sgp' : 'cn'
      window.AliyunCaptchaConfig = { region: regionShort, prefix: config.prefix }

      window.initAliyunCaptcha({
        SceneId: config.sceneId,
        EncryptedSceneId: config.encryptedSceneId || undefined,
        mode: config.mode || 'embed',
        element: config.element,
        button: config.button,
        slideStyle: config.slideStyle || { width: 360, height: 40 },
        onBizSuccess: () => {},
        success: callbacks.onSuccess,
        fail: callbacks.onFail || (() => {}),
        getInstance: (instance) => callbacks.onReady?.(instance),
      })
    },

    /**
     * 清理全局配置（组件销毁时调用）
     */
    cleanup(): void {
      if (window.AliyunCaptchaConfig) {
        delete (window as any).AliyunCaptchaConfig
      }
    },

    /**
     * 取验证码前端配置（公开接口，无需 Token）。
     * 配了阿里云 → { mode:'aliyun', prefix, scene_id, region, encrypted_scene_id? }；
     * 未配/非阿里云 → { mode:'image' }（改调 createImage）。
     */
    getConfig(scene: CaptchaScene): Promise<CaptchaConfig> {
      return client.get<CaptchaConfig>('/v1/captcha/config', { scene })
    },

    /**
     * 取图片验证码（图形验证码）。
     *
     * 两种用法：
     * 1. mode=image 回退：auth 流程里 aliyun 不可用时取图形码（不传参）
     * 2. 独立图形验证码 API：自定义业务场景（提现/邀请等）直接取码，可选按应用读配置
     *
     * 后端配置优先级：代码默认 < type=image 服务商配置 < 请求传参。
     * 返回的 mode 决定前端输入框语义（char 直接输字符，math 输入算式结果）。
     */
    createImage(opts?: CreateImageOptions): Promise<ImageCaptcha> {
      const query = new URLSearchParams()
      if (opts?.app_id) query.set('app_id', opts.app_id)
      const qs = query.toString() ? `?${query.toString()}` : ''
      return client.post<ImageCaptcha>(`/captcha/create${qs}`, {
        mode: opts?.mode,
        width: opts?.width,
        height: opts?.height,
        size: opts?.size,
        noise: opts?.noise,
      })
    },

    /**
     * 校验图形验证码（独立 API，不分场景）。
     *
     * 适用于非 auth 的自定义业务场景：先 createImage 取码 → 用户输入 → verifyImage 校验。
     * 校验一次性（成功后该 captcha_id 失效），失败累计达上限锁定。
     *
     * 注意：本接口只返回校验结果，不签发"已验证凭证"。业务接口若需独立核验用户确实验过，
     * 请结合一次性 token / 业务态自行设计（见 docs/plans/captcha-image-provider.md 闭环讨论）。
     */
    verifyImage(captcha_id: string, code: string): Promise<void> {
      return client.post('/v1/captcha/verify', { captcha_id, code })
    },
  }
}

export type CaptchaModule = ReturnType<typeof createCaptchaModule>
