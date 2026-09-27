import { request } from './http'
import type { Paged, Skill } from './types'

export const skillApi = {
  list(params: { page?: number; page_size?: number; keyword?: string } = {}) {
    return request<Paged<Skill>>({ url: '/admin/skills', method: 'GET', params })
  },
  /** 扫描目录中的 SKILL.md 并导入 */
  scan(dir?: string) {
    return request<{ imported: number }>({
      url: '/admin/skills/scan',
      method: 'POST',
      data: { dir: dir ?? '' },
    })
  },
  toggle(id: number, enabled: boolean) {
    return request<Skill>({
      url: `/admin/skills/${id}/toggle`,
      method: 'PATCH',
      data: { enabled },
    })
  },
  remove(id: number) {
    return request<{ id: number; deleted: boolean }>({ url: `/admin/skills/${id}`, method: 'DELETE' })
  },
}
