import { request } from './http'

/**
 * API Key 分组：把倍率与限流策略打包成预设，供密钥选用。
 *
 * 密钥上的 group_tag 记录分组标识；调用时按该分组叠加倍率系数与 QPM / 并发限制。
 * 未匹配到分组（历史自由文本标签、分组被删除或禁用）时按中性策略运行。
 */
export interface KeyGroup {
  id: number
  /** 分组标识，密钥上记录的就是它 */
  name: string
  display_name: string
  description: string
  /** 倍率系数：在模型倍率算出的点数上相乘，1 表示不加价 */
  ratio_multiplier: number
  /** 分组级每分钟请求上限，0 表示该层不设限 */
  qpm: number
  /** 分组级并发上限，0 表示该层不设限 */
  concurrency: number
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface GroupPayload {
  name: string
  display_name?: string
  description?: string
  ratio_multiplier: number
  qpm: number
  concurrency: number
  enabled: boolean
}

export const groupApi = {
  /** 登录用户可读：启用中的分组，供密钥表单选择 */
  options() {
    return request<KeyGroup[]>({ url: '/groups', method: 'GET' })
  },
  /** 管理员：全部分组（含已禁用） */
  list() {
    return request<KeyGroup[]>({ url: '/admin/groups', method: 'GET' })
  },
  create(payload: GroupPayload) {
    return request<KeyGroup>({ url: '/admin/groups', method: 'POST', data: payload })
  },
  update(id: number, payload: GroupPayload) {
    return request<KeyGroup>({ url: `/admin/groups/${id}`, method: 'PUT', data: payload })
  },
  remove(id: number) {
    return request<{ id: number; deleted: boolean }>({ url: `/admin/groups/${id}`, method: 'DELETE' })
  },
}
