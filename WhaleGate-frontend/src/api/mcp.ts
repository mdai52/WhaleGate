import { request } from './http'
import type { ExposedTool, MCPServer, MCPServerInput } from './types'

export const mcpApi = {
  list() {
    return request<MCPServer[]>({ url: '/admin/mcp', method: 'GET' })
  },
  create(payload: MCPServerInput) {
    return request<MCPServer>({ url: '/admin/mcp', method: 'POST', data: payload })
  },
  update(id: number, payload: Partial<MCPServerInput>) {
    return request<MCPServer>({ url: `/admin/mcp/${id}`, method: 'PUT', data: payload })
  },
  remove(id: number) {
    return request<{ id: number; deleted: boolean }>({ url: `/admin/mcp/${id}`, method: 'DELETE' })
  },
  /** 连接并拉取工具列表 */
  connect(id: number) {
    return request<MCPServer>({ url: `/admin/mcp/${id}/connect`, method: 'POST' })
  },
  /** 连通性测试：连一次看状态与工具数，不改落库字段 */
  test(id: number) {
    return request<{ status: string; tool_count: number; latency_ms: number }>({
      url: `/admin/mcp/${id}/test`,
      method: 'POST',
    })
  },
  /** 启停：停用后不再参与工具发现与调用 */
  toggle(id: number, enabled: boolean) {
    return request<MCPServer>({
      url: `/admin/mcp/${id}/toggle`,
      method: 'PATCH',
      data: { enabled },
    })
  },
  /** 对外暴露的工具（含路由信息） */
  tools() {
    return request<ExposedTool[]>({ url: '/admin/mcp/tools', method: 'GET' })
  },
  /** 调试：直接调用工具 */
  call(name: string, arguments_: Record<string, unknown>) {
    return request<{ result: string }>({
      url: '/admin/mcp/tools/call',
      method: 'POST',
      data: { name, arguments: arguments_ },
    })
  },
}
