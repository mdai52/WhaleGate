import axios, { AxiosError, type AxiosInstance, type AxiosRequestConfig } from 'axios'
import { message } from 'ant-design-vue'

/** 与后端 internal/pkg/response.Body 严格对应的统一响应信封。 */
export interface ApiResponse<T = unknown> {
  code: number
  message: string
  data?: T
  trace_id?: string
  timestamp: number
}

/** 业务错误，携带后端错误码与提示。 */
export class ApiError extends Error {
  readonly code: number
  readonly traceId?: string

  constructor(code: number, msg: string, traceId?: string) {
    super(msg)
    this.name = 'ApiError'
    this.code = code
    this.traceId = traceId
  }
}

export const TOKEN_KEY = 'wg_token'

const http: AxiosInstance = axios.create({
  baseURL: '/api/v1',
  timeout: 60000,
  headers: { 'Content-Type': 'application/json' },
})

http.interceptors.request.use((config) => {
  const token = localStorage.getItem(TOKEN_KEY)
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

http.interceptors.response.use(
  (response) => response,
  (error: AxiosError<ApiResponse>) => {
    const status = error.response?.status
    const payload = error.response?.data
    if (status === 401) {
      localStorage.removeItem(TOKEN_KEY)
      message.error(payload?.message ?? '登录已过期，请重新登录')
    } else {
      message.error(payload?.message ?? error.message ?? '网络异常，请稍后重试')
    }
    return Promise.reject(
      new ApiError(payload?.code ?? status ?? -1, payload?.message ?? error.message, payload?.trace_id),
    )
  },
)

/** 发起请求并解包统一响应，失败时弹出中文提示。 */
export async function request<T = unknown>(config: AxiosRequestConfig): Promise<T> {
  const response = await http.request<ApiResponse<T>>(config)
  const body = response.data
  if (body.code !== 0) {
    message.error(body.message)
    throw new ApiError(body.code, body.message, body.trace_id)
  }
  return body.data as T
}

export default http
