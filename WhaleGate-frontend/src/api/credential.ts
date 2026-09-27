import http, { request } from './http'
import type { Credential, Paged } from './types'

export const credentialApi = {
  list(
    params: {
      page?: number
      page_size?: number
      provider?: string
      keyword?: string
      status?: number
    } = {},
  ) {
    return request<Paged<Credential>>({ url: '/admin/credentials', method: 'GET', params })
  },
  rename(id: number, name: string) {
    return request<Credential>({
      url: `/admin/credentials/${id}/name`,
      method: 'PATCH',
      data: { name },
    })
  },
  /** 导出认证文件（返回 JSON 文本与文件名） */
  async export(id: number): Promise<{ name: string; content: string }> {
    const response = await http.get<Blob>(`/admin/credentials/${id}/export`, {
      responseType: 'blob',
    })
    const disposition = String(response.headers['content-disposition'] ?? '')
    const match = /filename="?([^";]+)"?/.exec(disposition)
    const content = await response.data.text()
    return { name: match?.[1] ?? `credential-${id}.json`, content }
  },
  /** 上传认证文件导入凭证 */
  import(file: File, name?: string) {
    const form = new FormData()
    form.append('file', file)
    if (name) {
      form.append('name', name)
    }
    return request<Credential>({
      url: '/admin/credentials',
      method: 'POST',
      data: form,
      headers: { 'Content-Type': 'multipart/form-data' },
    })
  },
  setStatus(id: number, status: number) {
    return request<{ id: number; status: number }>({
      url: `/admin/credentials/${id}/status`,
      method: 'PATCH',
      data: { status },
    })
  },
  refresh(id: number) {
    return request<Credential>({ url: `/admin/credentials/${id}/refresh`, method: 'POST' })
  },
  remove(id: number) {
    return request<{ id: number; deleted: boolean }>({
      url: `/admin/credentials/${id}`,
      method: 'DELETE',
    })
  },
}
