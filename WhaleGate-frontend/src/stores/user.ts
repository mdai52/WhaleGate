import { defineStore } from 'pinia'
import { authApi } from '@/api/auth'
import { TOKEN_KEY } from '@/api/http'
import type { LoginPayload, UserProfile } from '@/api/types'

interface UserState {
  token: string
  profile: UserProfile | null
  loading: boolean
}

export const useUserStore = defineStore('user', {
  state: (): UserState => ({
    token: localStorage.getItem(TOKEN_KEY) ?? '',
    profile: null,
    loading: false,
  }),
  getters: {
    isLogin: (state) => Boolean(state.token),
    isAdmin: (state) => state.profile?.role === 'admin',
    displayName: (state) => state.profile?.nickname || state.profile?.username || '未登录',
  },
  actions: {
    async login(payload: LoginPayload) {
      this.loading = true
      try {
        const result = await authApi.login(payload)
        this.token = result.token
        this.profile = result.user
        localStorage.setItem(TOKEN_KEY, result.token)
        return result.user
      } finally {
        this.loading = false
      }
    },
    async fetchProfile() {
      if (!this.token) {
        return null
      }
      this.profile = await authApi.me()
      return this.profile
    },
    /** 改密后服务端会返回新令牌（旧令牌仍带 must_change 标记） */
    setToken(token: string) {
      this.token = token
      localStorage.setItem(TOKEN_KEY, token)
    },
    logout() {
      this.token = ''
      this.profile = null
      localStorage.removeItem(TOKEN_KEY)
    },
  },
})
