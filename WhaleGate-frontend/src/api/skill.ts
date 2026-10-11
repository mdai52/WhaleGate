import { request } from './http'
import type { Paged, Skill } from './types'

export const skillApi = {
  list(params: { page?: number; page_size?: number; keyword?: string } = {}) {
    return request<Paged<Skill>>({ url: '/admin/skills', method: 'GET', params })
  },
  /** 从压缩包导入 SKILL.md，上限由管理员在系统设置里配置 */
  upload(file: File) {
    const form = new FormData()
    form.append('file', file)
    return request<{ imported: number }>({
      url: '/admin/skills/upload',
      method: 'POST',
      data: form,
      headers: { 'Content-Type': 'multipart/form-data' },
      // 大包上传给足时间，别被默认的 60s 超时掐断
      timeout: 300000,
    })
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
  /** 切换注入模式：always 全文注入 / index 仅索引 */
  setMode(id: number, mode: 'always' | 'index') {
    return request<Skill>({
      url: `/admin/skills/${id}/mode`,
      method: 'PATCH',
      data: { mode },
    })
  },
  remove(id: number) {
    return request<{ id: number; deleted: boolean }>({ url: `/admin/skills/${id}`, method: 'DELETE' })
  },
}
