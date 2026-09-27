import { request } from './http'
import type { LoginPayload, LoginResult, RegisterPayload, UserProfile } from './types'

export const authApi = {
  login(payload: LoginPayload) {
    return request<LoginResult>({ url: '/auth/login', method: 'POST', data: payload })
  },
  register(payload: RegisterPayload) {
    return request<UserProfile>({ url: '/auth/register', method: 'POST', data: payload })
  },
  me() {
    return request<UserProfile>({ url: '/me', method: 'GET' })
  },
  logout() {
    return request<null>({ url: '/auth/logout', method: 'POST' })
  },
  changePassword(oldPassword: string, newPassword: string) {
    return request<{ changed: boolean; token: string }>({
      url: '/auth/change-password',
      method: 'POST',
      data: { old_password: oldPassword, new_password: newPassword },
    })
  },
}
