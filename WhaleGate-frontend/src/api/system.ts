import { request } from './http'
import type { LoginResult } from './types'

export interface SystemStatus {
  /** 是否已完成初始化（已有账号） */
  initialized: boolean
  allow_registration: boolean
  version: string
}

export interface InstallPayload {
  username: string
  password: string
  email?: string
  nickname?: string
}

export const systemApi = {
  /** 系统初始化状态（公开接口） */
  status() {
    return request<SystemStatus>({ url: '/system/status', method: 'GET' })
  },
  /** 首次安装：创建管理员并直接登录（公开接口，仅未初始化时可用） */
  install(payload: InstallPayload) {
    return request<LoginResult>({ url: '/system/install', method: 'POST', data: payload })
  },
}
