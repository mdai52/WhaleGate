import { request } from './http'
import type { CallLog, Paged, UsagePoint } from './types'

export interface LogQuery {
  page?: number
  page_size?: number
  model?: string
  status?: number
  start?: string
  end?: string
  user_id?: number
}

export const usageApi = {
  /** 当前用户（管理员可传 user_id）的调用历史 */
  logs(params: LogQuery = {}) {
    return request<Paged<CallLog>>({ url: '/usage/logs', method: 'GET', params })
  },
  /** 按天聚合的用量，供门户图表使用 */
  summary(days = 7, userId?: number) {
    return request<UsagePoint[]>({
      url: '/usage/summary',
      method: 'GET',
      params: { days, user_id: userId },
    })
  },
}
