import type { SdkClient } from '../client'

/** POST /storage/avatar 返回 —— 头像上传结果 */
export interface AvatarUploadResult {
  /** 头像访问地址（七牛等云存储为 CDN 绝对 URL；本地存储为 baseURL 拼接地址） */
  url: string
  /** 原始文件名（服务端只保留扩展名，存储名用纳秒时间戳） */
  file_name: string
  /** 文件大小（字节） */
  file_size: number
}

/**
 * 创建存储模块
 *
 * 封装文件上传接口（需登录态，自动带 Token），统一走 `sdk.storage.*`。
 *
 * @example
 * ```ts
 * const res = await sdk.storage.uploadAvatar(file)
 * // res.url → https://cdn.../avatars/20260617/xxx.png
 * ```
 */
export function createStorageModule(client: SdkClient) {
  return {
    /**
     * 上传头像（路径服务端固定 avatars/，仅图片、≤2MB）。需登录态，未登录将返回 401。
     *
     * @param file 图片文件（浏览器 File/Blob）
     */
    uploadAvatar(file: File | Blob): Promise<AvatarUploadResult> {
      const form = new FormData()
      form.append('file', file)
      return client.upload<AvatarUploadResult>('/v1/storage/avatar', form)
    },
  }
}

export type StorageModule = ReturnType<typeof createStorageModule>
