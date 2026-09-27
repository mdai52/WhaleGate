/** 后端模型对应的前端类型定义。 */

export interface UserProfile {
  id: number
  username: string
  email?: string
  nickname?: string
  role: 'user' | 'admin'
  status: number
  quota: number
  used_quota: number
  request_count: number
  last_login_at?: string
  created_at: string
  /** 首次登录（或管理员重置密码后）必须修改密码 */
  must_change_password: boolean
}

export interface LoginPayload {
  account: string
  password: string
}

export interface LoginResult {
  token: string
  expires_in: number
  user: UserProfile
}

export interface RegisterPayload {
  username: string
  password: string
  email?: string
  nickname?: string
  role?: 'user' | 'admin'
}

/** 自动探测到的上游模型 */
export interface DetectedModel {
  id: string
  display_name?: string
  capabilities?: string[]
  context_window?: number
  max_output?: number
}

/** 自动探测结果 */
export interface DetectResult {
  detected_type: string
  suggested_type: ChannelType
  base_url: string
  endpoint: string
  latency_ms: number
  model_count: number
  models: DetectedModel[]
  capabilities: string[]
  context_window: number
  max_output: number
  suggested_name: string
  warning?: string
}

/** 全局模型目录条目 */
export interface ModelCatalog {
  id: number
  model_id: string
  display_name: string
  provider_type: string
  capabilities: string
  context_window: number
  max_output: number
  source: string
}

export interface APIKeyItem {
  id: number
  user_id: number
  name: string
  prefix: string
  masked_key: string
  status: number
  qpm: number
  concurrency: number
  expires_at?: string | null
  revoked_at?: string | null
  last_used_at?: string | null
  request_count: number
  total_tokens: number
  created_at: string
}

export interface CreateKeyPayload {
  name: string
  expires_in_days?: number
  qpm?: number
  concurrency?: number
}

export interface CreateKeyResult extends APIKeyItem {
  /** 明文密钥，仅创建时返回一次。 */
  key: string
}

export interface Paged<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

// ---------------------------------------------------------------- 渠道

export type ChannelType = 'openai' | 'gemini'

/** 渠道状态：1 正常 / 2 人工禁用 / 3 自动熔断 */
export type ChannelStatus = 1 | 2 | 3

/** 参数能力声明：上游支持哪些统一参数名。 */
export interface ParamSchema {
  supported?: string[]
  exclude?: string[]
  defaults?: Record<string, unknown>
}

/** 模型别名：一个上游模型 fork 成多个对外模型名。 */
export interface ModelAliasConfig {
  model?: string
  override?: Record<string, unknown>
}

export interface Channel {
  id: number
  name: string
  type: ChannelType
  base_url: string
  /** JSON 字符串形式的模型列表 */
  models: string
  /** JSON 字符串形式的模型名映射 */
  model_mapping: string
  /** JSON 字符串形式的模型别名 */
  model_alias: string
  /** JSON 字符串形式的请求参数注入 */
  request_override: string
  /** JSON 字符串形式的参数能力声明 */
  param_schema: string
  weight: number
  priority: number
  status: ChannelStatus
  timeout_seconds: number
  max_retries: number
  last_error?: string
  fail_count: number
  disabled_at?: string | null
  created_at: string
  updated_at: string
  api_key_masked: string
}

export interface ChannelInput {
  name: string
  type: ChannelType
  base_url: string
  /** 上游密钥明文，留空表示不修改 */
  api_key?: string
  models?: string[]
  model_mapping?: Record<string, string>
  model_alias?: Record<string, ModelAliasConfig>
  request_override?: Record<string, unknown>
  param_schema?: ParamSchema
  weight?: number
  priority?: number
  status?: ChannelStatus
  timeout_seconds?: number
  max_retries?: number
  /** 自动探测得到的能力集合 */
  capabilities?: string[]
  /** 自动探测得到的上下文窗口 */
  context_window?: number
  /** 自动探测得到的输出上限 */
  max_output?: number
}

// ---------------------------------------------------------------- 倍率与日志

export interface ModelRatio {
  id: number
  model: string
  /** 点 / 1K prompt tokens */
  prompt_ratio: number
  /** 点 / 1K completion tokens（含思维链） */
  completion_ratio: number
  enabled: boolean
  created_at: string
  updated_at: string
}

/** 调用状态：1 成功 / 2 失败 */
export type CallStatus = 1 | 2

export interface CallLog {
  id: number
  user_id: number
  api_key_id: number
  channel_id: number
  channel_name?: string
  model: string
  upstream_model?: string
  protocol: string
  stream: boolean
  prompt_tokens: number
  completion_tokens: number
  reasoning_tokens: number
  total_tokens: number
  points: number
  latency_ms: number
  first_token_ms: number
  status: CallStatus
  http_status: number
  error_code?: number
  error_message?: string
  usage_confidence: 'reported' | 'estimated'
  params_applied?: string
  params_dropped?: string
  trace_id?: string
  client_ip?: string
  created_at: string
}

export interface UsagePoint {
  date: string
  requests: number
  prompt_tokens: number
  completion_tokens: number
  reasoning_tokens: number
  points: number
}
/** OAuth 凭证（认证文件）相关类型。 */

export type OAuthFlow = 'authorization_code' | 'device_code'

export interface OAuthProvider {
  id: string
  name: string
  description?: string
  flow: OAuthFlow
  scopes?: string[]
}

export interface OAuthStart {
  provider: string
  name?: string
  flow: string
  state: string
  authorize_url?: string
  verification_url?: string
  user_code?: string
  expires_in: number
}

/** 凭证状态：1 启用 / 2 停用 / 3 失效 */
export type CredentialStatus = 1 | 2 | 3

export interface Credential {
  id: number
  provider: string
  name: string
  account?: string
  auth_type: 'oauth' | 'file' | 'api_key'
  token_type?: string
  expires_at?: string | null
  status: CredentialStatus
  quota?: string
  last_refresh_at?: string | null
  last_error?: string
  file_name?: string
  created_at: string
  updated_at: string
}

// ---------------------------------------------------------------- 账号绑定

/** 第三方身份绑定 */
export interface UserIdentity {
  id: number
  user_id: number
  provider: string
  provider_uid: string
  email?: string
  display_name?: string
  created_at: string
}

/** 通行密钥凭证 */
export interface WebAuthnCredential {
  id: number
  user_id: number
  name: string
  credential_id: string
  aaguid?: string
  sign_count: number
  created_at: string
  last_used_at?: string | null
}

export interface BindingsResult {
  identities: UserIdentity[]
  passkeys: WebAuthnCredential[]
  github_enabled: boolean
  passkey_enabled: boolean
}
