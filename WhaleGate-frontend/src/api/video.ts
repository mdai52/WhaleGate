import http, { request } from './http'
import type {
  Paged,
  PromptLibrary,
  VideoModel,
  VideoScript,
  VideoSubmit,
  VideoWork,
} from './types'

/** 视频工作台：脚本创作与视频生成（异步任务） */
export const videoApi = {
  /** 可用模型：kind = video 视频生成 / text 脚本创作 */
  models() {
    return request<VideoModel[]>({ url: '/studio/video/models', method: 'GET' })
  },
  /** 视频脚本词库 */
  prompts(params: { category?: string; keyword?: string } = {}) {
    return request<PromptLibrary>({ url: '/studio/video/prompts', method: 'GET', params })
  },
  /** 创作并保存分镜脚本 */
  createScript(payload: {
    model: string
    topic: string
    style?: string
    audience?: string
    duration?: number
    scenes?: number
    notes?: string
    template_id?: number
  }) {
    return request<VideoScript>({ url: '/studio/video/scripts', method: 'POST', data: payload })
  },
  listScripts(params: { page?: number; page_size?: number } = {}) {
    return request<Paged<VideoScript>>({ url: '/studio/video/scripts', method: 'GET', params })
  },
  removeScript(id: number) {
    return request<{ id: number; deleted: boolean }>({
      url: `/studio/video/scripts/${id}`,
      method: 'DELETE',
    })
  },
  /** 提交视频生成：立即返回「已排队」，进度需轮询作品列表 */
  generate(payload: {
    model: string
    prompt: string
    negative_prompt?: string
    seconds?: number
    size?: string
    aspect_ratio?: string
    fps?: number
    image?: string
    script_id?: number
    scene_index?: number
    extra?: Record<string, unknown>
  }) {
    return request<VideoSubmit>({ url: '/studio/video/works', method: 'POST', data: payload })
  },
  listWorks(params: { page?: number; page_size?: number } = {}) {
    return request<Paged<VideoWork>>({ url: '/studio/video/works', method: 'GET', params })
  },
  work(id: number) {
    return request<VideoWork>({ url: `/studio/video/works/${id}`, method: 'GET' })
  },
  removeWork(id: number) {
    return request<{ id: number; deleted: boolean }>({
      url: `/studio/video/works/${id}`,
      method: 'DELETE',
    })
  },
  /**
   * 作品视频托管在服务端且需要登录态时，<video> 无法直接携带 Authorization。
   * 这里取回二进制后转成临时地址，由调用方在不用时 revoke。
   */
  async fetchWorkVideo(workId: number, index: number): Promise<string> {
    const res = await http.get(`/studio/video/works/${workId}/videos/${index}`, {
      responseType: 'blob',
    })
    return URL.createObjectURL(res.data as Blob)
  },
}
