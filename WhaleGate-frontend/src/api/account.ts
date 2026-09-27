import { request } from './http'
import type {
  BindingsResult,
  LoginResult,
  UserIdentity,
  WebAuthnCredential,
} from './types'

export const accountApi = {
  /** 第三方绑定与通行密钥列表 */
  bindings() {
    return request<BindingsResult>({ url: '/account/bindings', method: 'GET' })
  },
  unbindIdentity(id: number) {
    return request<{ id: number; unbound: boolean }>({
      url: `/account/bindings/identity/${id}`,
      method: 'DELETE',
    })
  },
  deletePasskey(id: number) {
    return request<{ id: number; deleted: boolean }>({
      url: `/account/bindings/passkey/${id}`,
      method: 'DELETE',
    })
  },
  /** 通行密钥注册第一步 */
  passkeyRegisterBegin(name?: string) {
    return request<{ session_id: string; options: Record<string, unknown>; name?: string }>({
      url: '/account/passkey/register/begin',
      method: 'POST',
      data: { name },
    })
  },
  /** 通行密钥注册第二步 */
  passkeyRegisterFinish(sessionId: string, credential: unknown, name?: string) {
    return request<WebAuthnCredential>({
      url: '/account/passkey/register/finish',
      method: 'POST',
      data: { session_id: sessionId, credential, name },
    })
  },
  /** GitHub 授权地址 */
  githubAuthorize(mode: 'bind' | 'login' = 'bind') {
    return request<{ authorize_url: string }>({
      url: '/account/github/authorize',
      method: 'GET',
      params: { mode },
    })
  },
  /** 用授权码完成绑定 */
  githubBind(code: string, state: string) {
    return request<UserIdentity>({
      url: '/account/github/callback',
      method: 'POST',
      data: { code, state },
    })
  },
  /** 通行密钥登录第一步 */
  passkeyLoginBegin() {
    return request<{ session_id: string; options: Record<string, unknown> }>({
      url: '/auth/passkey/login/begin',
      method: 'POST',
    })
  },
  /** 通行密钥登录第二步 */
  passkeyLoginFinish(sessionId: string, credential: unknown) {
    return request<LoginResult>({
      url: '/auth/passkey/login/finish',
      method: 'POST',
      data: { session_id: sessionId, credential },
    })
  },
  /** 一次性票据换取令牌 */
  exchangeTicket(ticket: string) {
    return request<{ token: string }>({
      url: '/auth/ticket',
      method: 'POST',
      data: { ticket },
    })
  },
}
