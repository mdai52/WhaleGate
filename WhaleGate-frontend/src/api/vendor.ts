import { request } from './http'
import type { VendorConfig, VendorOptions, VendorProfile } from './types'

/** 厂商适配档案（管理端） */
export const vendorApi = {
  list() {
    return request<VendorProfile[]>({ url: '/admin/vendors', method: 'GET' })
  },
  options() {
    return request<VendorOptions>({ url: '/admin/vendors/options', method: 'GET' })
  },
  create(payload: { code: string; sort?: number; enabled?: boolean; config: VendorConfig }) {
    return request<VendorProfile>({ url: '/admin/vendors', method: 'POST', data: payload })
  },
  update(id: number, payload: { code: string; sort?: number; enabled?: boolean; config: VendorConfig }) {
    return request<VendorProfile>({ url: `/admin/vendors/${id}`, method: 'PUT', data: payload })
  },
  remove(id: number) {
    return request<{ id: number; deleted: boolean }>({ url: `/admin/vendors/${id}`, method: 'DELETE' })
  },
  /** 手动重灌内存档案；一般保存后会自动生效，这里作为兜底 */
  reload() {
    return request<{ count: number; reloaded: boolean }>({ url: '/admin/vendors/reload', method: 'POST' })
  },
}
