import http, { request } from './http'
import type { ImageGenerateInput, ImageWork, Paged, PromptLibrary, StudioModel } from './types'

export const studioApi = {
  models() {
    return request<StudioModel[]>({ url: '/studio/models', method: 'GET' })
  },
  prompts(params: { category?: string; keyword?: string } = {}) {
    return request<PromptLibrary>({ url: '/studio/prompts', method: 'GET', params })
  },
  generate(payload: ImageGenerateInput) {
    return request<ImageWork>({ url: '/studio/images', method: 'POST', data: payload })
  },
  works(params: { page?: number; page_size?: number } = {}) {
    return request<Paged<ImageWork>>({ url: '/studio/works', method: 'GET', params })
  },
  removeWork(id: number) {
    return request<{ id: number; deleted: boolean }>({ url: `/studio/works/${id}`, method: 'DELETE' })
  },
  /**
   * 作品图片托管在服务端且需要登录态，浏览器无法直接用于 <img>。
   * 这里取回二进制后转成临时地址，由调用方在不用时 revoke。
   */
  async fetchWorkImage(workId: number, index: number): Promise<string> {
    const res = await http.get(`/studio/works/${workId}/images/${index}`, { responseType: 'blob' })
    return URL.createObjectURL(res.data as Blob)
  },
}
