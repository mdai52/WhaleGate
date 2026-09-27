import { request } from './http'
import type { CallLog, ModelRatio, Paged, RegisterPayload, UserProfile } from './types'

export const adminApi = {
  listUsers(params: { page?: number; page_size?: number; keyword?: string } = {}) {
    return request<Paged<UserProfile>>({ url: '/admin/users', method: 'GET', params })
  },
  createUser(payload: RegisterPayload) {
    return request<UserProfile>({ url: '/admin/users', method: 'POST', data: payload })
  },
  setUserStatus(id: number, status: number) {
    return request<{ id: number; status: number }>({
      url: `/admin/users/${id}/status`,
      method: 'PATCH',
      data: { status },
    })
  },
  changeQuota(id: number, delta: number) {
    return request<UserProfile>({
      url: `/admin/users/${id}/quota`,
      method: 'POST',
      data: { delta },
    })
  },
  listRatios(params: { page?: number; page_size?: number } = {}) {
    return request<Paged<ModelRatio>>({ url: '/admin/ratios', method: 'GET', params })
  },
  upsertRatio(payload: Partial<ModelRatio> & { model: string }) {
    return request<ModelRatio>({ url: '/admin/ratios', method: 'POST', data: payload })
  },
  deleteRatio(id: number) {
    return request<{ id: number; deleted: boolean }>({ url: `/admin/ratios/${id}`, method: 'DELETE' })
  },
  listLogs(params: Record<string, unknown> = {}) {
    return request<Paged<CallLog>>({ url: '/admin/logs', method: 'GET', params })
  },
}
