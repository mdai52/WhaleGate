import http, { request } from './http'
import type { CanvasExport, CanvasPayload, CanvasRecord, Paged } from './types'

/** 画布工作台：画布 CRUD 与服务端异步导出 */
export const canvasApi = {
  list(params: { page?: number; page_size?: number } = {}) {
    return request<Paged<CanvasRecord>>({ url: '/studio/canvas', method: 'GET', params })
  },
  create(payload: { name?: string; width?: number; height?: number; background?: string; payload?: CanvasPayload }) {
    return request<CanvasRecord>({ url: '/studio/canvas', method: 'POST', data: payload })
  },
  get(id: number) {
    return request<CanvasRecord>({ url: `/studio/canvas/${id}`, method: 'GET' })
  },
  update(
    id: number,
    payload: { name?: string; width?: number; height?: number; background?: string; payload?: CanvasPayload },
  ) {
    return request<CanvasRecord>({ url: `/studio/canvas/${id}`, method: 'PUT', data: payload })
  },
  remove(id: number) {
    return request<{ id: number; deleted: boolean }>({ url: `/studio/canvas/${id}`, method: 'DELETE' })
  },
  /** 提交导出；队列满时后端返回限流错误，由调用方提示稍后重试 */
  submitExport(id: number, format: 'png' | 'jpeg' = 'png', scale = 1) {
    return request<CanvasExport>({
      url: `/studio/canvas/${id}/exports`,
      method: 'POST',
      data: { format, scale },
    })
  },
  exports(params: { page?: number; page_size?: number } = {}) {
    return request<Paged<CanvasExport>>({ url: '/studio/canvas-exports', method: 'GET', params })
  },
  getExport(id: number) {
    return request<CanvasExport>({ url: `/studio/canvas-exports/${id}`, method: 'GET' })
  },
  /** 导出结果需要登录态，取回二进制后转临时地址，由调用方 revoke */
  async fetchExport(id: number): Promise<string> {
    const res = await http.get(`/studio/canvas-exports/${id}/content`, { responseType: 'blob' })
    return URL.createObjectURL(res.data as Blob)
  },
}

/** 导出任务状态文本 */
export function exportStatusText(status: number): string {
  switch (status) {
    case 0:
      return '排队中'
    case 1:
      return '渲染中'
    case 2:
      return '已完成'
    default:
      return '失败'
  }
}
