import { request } from './http'

export interface Settings {
  /** 自用模式：开启后所有用户调用不计费 */
  self_use_mode: boolean
  /** 是否启用技能注入 */
  skills_enabled: boolean
  /** 是否启用 MCP 工具 */
  mcp_enabled: boolean
  /** 网关代执行的最大工具轮次 */
  mcp_max_rounds: number
  /** 是否以 HTTPS 监听（由服务端直接提供 TLS） */
  tls_enabled: boolean
  /** TLS 证书（PEM），响应回显以便下载 */
  tls_cert: string
  /** TLS 私钥：仅更新时传入，响应永远为空 */
  tls_key: string
  /** 是否已配置私钥 */
  tls_key_set: boolean
}

export const settingsApi = {
  get() {
    return request<Settings>({ url: '/admin/settings', method: 'GET' })
  },
  update(payload: Partial<Settings>) {
    return request<Settings>({ url: '/admin/settings', method: 'PUT', data: payload })
  },
}
