import { request } from './http'
import type { APIKeyItem, CreateKeyPayload, CreateKeyResult, Paged, UpdateKeyPayload } from './types'

export const apiKeyApi = {
  list(params: { page?: number; page_size?: number; user_id?: number } = {}) {
    return request<Paged<APIKeyItem>>({ url: '/keys', method: 'GET', params })
  },
  create(payload: CreateKeyPayload) {
    return request<CreateKeyResult>({ url: '/keys', method: 'POST', data: payload })
  },
  update(id: number, payload: UpdateKeyPayload) {
    return request<APIKeyItem>({ url: `/keys/${id}`, method: 'PUT', data: payload })
  },
  revoke(id: number) {
    return request<{ id: number; revoked: boolean }>({ url: `/keys/${id}/revoke`, method: 'POST' })
  },
  remove(id: number) {
    return request<{ id: number; deleted: boolean }>({ url: `/keys/${id}`, method: 'DELETE' })
  },
}
