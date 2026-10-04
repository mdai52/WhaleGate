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
  /** 是否启用通行密钥 */
  passkey_enabled: boolean
  /** 通行密钥依赖方 ID */
  passkey_rp_id: string
  /** 通行密钥允许的源地址，多个以英文逗号分隔 */
  passkey_rp_origins: string

  // ---------------------------------------------------------------- 绘图工作台存储

  /** 存储后端：local 本地磁盘 / s3 对象存储 */
  studio_storage_driver: 'local' | 's3'
  /** 本地存储根目录，留空回退到配置文件的 studio.data_dir */
  studio_local_root: string
  /** 本地目录已由外部 Web 服务器直接服务时的公开访问前缀 */
  studio_local_base_url: string
  /** 对象存储服务地址，需带协议 */
  studio_s3_endpoint: string
  /** 对象存储区域 */
  studio_s3_region: string
  /** 桶名 */
  studio_s3_bucket: string
  /** 桶内对象键前缀 */
  studio_s3_prefix: string
  /** 访问密钥 ID */
  studio_s3_access_key: string
  /** 访问密钥：仅提交时传入，响应恒为空 */
  studio_s3_secret_key: string
  /** 是否已配置访问密钥 */
  studio_s3_secret_key_set: boolean
  /** 公开访问前缀（CDN 或桶公开域名） */
  studio_s3_public_base_url: string
  /** 是否使用路径风格寻址（自建 MinIO 与部分国内厂商需要） */
  studio_s3_force_path_style: boolean
}

export const settingsApi = {
  get() {
    return request<Settings>({ url: '/admin/settings', method: 'GET' })
  },
  update(payload: Partial<Settings>) {
    return request<Settings>({ url: '/admin/settings', method: 'PUT', data: payload })
  },
}
