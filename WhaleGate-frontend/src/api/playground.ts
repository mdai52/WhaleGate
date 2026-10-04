import { request } from './http'

/** 调试页可选的模型 */
export interface PlaygroundModel {
  model: string
  channel: string
  channel_type: string
  upstream_model: string
}

/** 参数预览入参 */
export interface ParamPreviewInput {
  model: string
  prompt?: string
  params: Record<string, unknown>
}

/** 参数分配预览结果 */
export interface ParamPreview {
  model: string
  upstream_model: string
  channel: { id: number; name: string; type: string }
  /** 渠道是否显式声明了参数能力；false 表示按协议默认集判定 */
  declared: boolean
  /** 该渠道最终生效的支持参数集 */
  supported: string[]
  /** 渠道明确排除的参数 */
  excluded: string[]
  /** 渠道配置的默认值，仅在用户未传该参数时生效 */
  defaults?: Record<string, unknown>
  /** 会被写入上游请求的参数 */
  applied: string[]
  /** 会被丢弃的参数 */
  dropped: string[]
  /** 既非标准参数、也无法归一的自定义参数，原样透传给上游 */
  unknown: string[]
  /** 渠道级与别名级的强制注入，优先级最高 */
  injected?: Record<string, unknown>
  /** 最终发往上游的请求体 */
  payload: Record<string, unknown>
}

export const playgroundApi = {
  models() {
    return request<PlaygroundModel[]>({ url: '/playground/models', method: 'GET' })
  },
  preview(payload: ParamPreviewInput) {
    return request<ParamPreview>({ url: '/playground/params/preview', method: 'POST', data: payload })
  },
}
