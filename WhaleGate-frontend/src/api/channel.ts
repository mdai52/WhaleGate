import { request } from './http'
import type {
  Channel,
  ChannelInput,
  DetectResult,
  ModelCatalog,
  Paged,
} from './types'

export const channelApi = {
  list(params: { page?: number; page_size?: number } = {}) {
    return request<Paged<Channel>>({ url: '/admin/channels', method: 'GET', params })
  },
  get(id: number) {
    return request<Channel>({ url: `/admin/channels/${id}`, method: 'GET' })
  },
  create(payload: ChannelInput) {
    return request<Channel>({ url: '/admin/channels', method: 'POST', data: payload })
  },
  update(id: number, payload: ChannelInput) {
    return request<Channel>({ url: `/admin/channels/${id}`, method: 'PUT', data: payload })
  },
  remove(id: number) {
    return request<{ id: number; deleted: boolean }>({ url: `/admin/channels/${id}`, method: 'DELETE' })
  },
  /** 自动探测：只需上游地址与密钥，自动识别协议与模型列表。
   * 编辑态可只传 channel_id，后端会读取该渠道已保存的密钥。 */
  detect(payload: { base_url: string; api_key: string; type?: string; channel_id?: number }) {
    return request<DetectResult>({ url: '/admin/channels/detect', method: 'POST', data: payload })
  },
  /** 全局模型目录：探测发现过的模型，供下拉选择 */
  catalog(
    params: { page?: number; page_size?: number; keyword?: string; provider_type?: string } = {},
  ) {
    return request<Paged<ModelCatalog>>({ url: '/admin/channels/model-catalog', method: 'GET', params })
  },
  /** 连通性测试：拉取上游模型列表，不消耗 token */
  test(id: number) {
    return request<{ status: number; latency_ms: number }>({
      url: `/admin/channels/${id}/test`,
      method: 'POST',
    })
  },
}
