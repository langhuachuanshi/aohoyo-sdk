/** 阿里云验证码区域 */
export type AliyunCaptchaRegion = 'cn-shanghai' | 'ap-southeast-1'

/** 阿里云验证码初始化配置 */
export interface AliyunCaptchaInitConfig {
  /** 场景 ID */
  sceneId: string
  /** 身份标（prefix） */
  prefix: string
  /** 区域 */
  region?: AliyunCaptchaRegion
  /** 加密场景 ID（可选，有则用加密模式） */
  encryptedSceneId?: string
  /** 渲染模式：embed 内嵌 / popup 弹窗 */
  mode?: 'embed' | 'popup'
  /** 内嵌模式的容器选择器（embed 模式必填） */
  element?: string
  /** 弹窗模式的触发按钮选择器（popup 模式必填） */
  button?: string
  /** 滑块样式 */
  slideStyle?: { width?: number; height?: number }
}

/** 验证码回调 */
export interface AliyunCaptchaCallbacks {
  /** 验证成功，返回 captchaVerifyParam */
  onSuccess: (captchaVerifyParam: string) => void | Promise<void>
  /** 验证失败 */
  onFail?: (result: unknown) => void
  /** 阿里云验证码实例就绪回调（initAliyunCaptcha 内部初始化完成后触发），
   *  拿到实例后可调用 refresh()，或作为 popup 自动触发的就绪信号 */
  onReady?: (instance: unknown) => void
}
