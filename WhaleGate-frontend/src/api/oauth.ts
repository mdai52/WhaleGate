import { request } from './http'
import type { Credential, OAuthProvider, OAuthStart } from './types'

export const oauthApi = {
  /** 可用的 OAuth 供应商列表 */
  providers() {
    return request<OAuthProvider[]>({ url: '/admin/oauth/providers', method: 'GET' })
  },
  /** 发起授权，返回授权链接或设备码信息 */
  start(provider: string, redirectURI?: string) {
    return request<OAuthStart>({
      url: `/admin/oauth/${provider}/start`,
      method: 'POST',
      data: { redirect_uri: redirectURI },
    })
  },
  /** 授权码流程：提交粘贴的授权码 */
  complete(provider: string, state: string, code: string, name?: string) {
    return request<Credential>({
      url: `/admin/oauth/${provider}/complete`,
      method: 'POST',
      data: { state, code, name },
    })
  },
  /** 设备码流程：轮询授权结果；等待中抛出 code=0 的提示 */
  poll(provider: string, state: string, name?: string) {
    return request<Credential | { pending: boolean }>({
      url: `/admin/oauth/${provider}/poll`,
      method: 'POST',
      data: { state, name },
    })
  },
}
