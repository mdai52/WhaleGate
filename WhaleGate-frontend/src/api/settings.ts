import { request } from './http'

export interface Settings {
  /** 自用模式：开启后所有用户调用不计费 */
  self_use_mode: boolean
}

export const settingsApi = {
  get() {
    return request<Settings>({ url: '/admin/settings', method: 'GET' })
  },
  update(payload: Partial<Settings>) {
    return request<Settings>({ url: '/admin/settings', method: 'PUT', data: payload })
  },
}
