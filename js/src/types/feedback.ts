/** 反馈提交请求 */
export interface FeedbackSubmitParams {
  /** 应用 ID */
  app_id: string
  /** 反馈标题 */
  title: string
  /** 反馈详情 */
  content: string
  /** 反馈类型：1=功能建议 2=缺陷报告 3=其他 */
  type: 1 | 2 | 3
  /** 图片 URL 列表（可通过 uploadImage 预先上传，或 submit 自动上传 File 对象） */
  images?: string[]
}

/** 反馈提交响应 */
export interface FeedbackSubmitResponse {
  id: number
  created_at: string
}

/** 反馈列表项 */
export interface FeedbackItem {
  id: number
  app_id: string
  user_id: number
  title: string
  content: string
  type: number
  status: number // 1=待处理 2=处理中 3=已解决 4=已关闭
  remark: string
  images: string[]
  created_at: string
  updated_at: string
}

/** 我的反馈列表查询参数 */
export interface FeedbackMyListParams {
  page?: number
  page_size?: number
  app_id?: string
  status?: number
}

/** 我的反馈列表响应 */
export interface FeedbackMyListResponse {
  list: FeedbackItem[]
  total: number
  page: number
  page_size: number
}

/** 图片上传响应 */
export interface UploadImageResponse {
  url: string
  file_name: string
  file_size: number
}
