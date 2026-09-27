# 鲸闸 WhaleGate 数据库字典

> 注释随迁移 `000009_audit_and_comments` 写入 PostgreSQL，可用以下语句核对：
>
> ```sql
> SELECT obj_description('public.users'::regclass) AS table_comment;
> SELECT column_name, col_description('public.users'::regclass, ordinal_position) AS column_comment
> FROM information_schema.columns WHERE table_name = 'users' ORDER BY ordinal_position;
> ```

## 总览

| 表 | 说明 |
| --- | --- |
| `users` | 用户账号表：门户用户与管理员，密码仅存 bcrypt 摘要 |
| `api_keys` | API Key 表：明文仅创建时返回一次，库中只存 SHA-256 摘要 |
| `channels` | 上游渠道表：上游密钥与注入参数加密存储 |
| `credentials` | OAuth 凭证表（认证文件）：令牌加密存储 |
| `model_ratios` | 模型倍率表：点 / 1K tokens，`*` 为兜底 |
| `call_logs` | 调用日志表：模型、渠道、耗时、token、点数、置信度 |
| `user_identities` | 第三方身份绑定表（GitHub 等） |
| `webauthn_credentials` | 通行密钥（WebAuthn）凭证表 |
| `audit_logs` | 审计日志表：安全相关操作留痕 |

金额统一以「点」为单位：**1 点 = 0.001 元**（见 `constant.PointsPerYuan`）。
倍率单位为 **点 / 1K tokens**。

---

## users · 用户账号

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial | 主键 |
| username | varchar(64) | 登录用户名，3-64 位字母数字下划线点连字符，唯一 |
| email | varchar(128) | 邮箱，建议唯一；**列表接口脱敏返回** |
| password_hash | varchar(128) | bcrypt 摘要；永不返回客户端、永不写日志 |
| nickname | varchar(64) | 昵称 |
| role | varchar(32) | `user` 普通用户 / `admin` 管理员 |
| status | smallint | 1 正常 / 2 禁用 |
| quota | bigint | 剩余额度（点） |
| used_quota | bigint | 累计已消耗额度（点） |
| request_count | bigint | 累计调用次数 |
| must_change_password | boolean | true 时除改密外拒绝其它已登录接口 |
| last_login_at | timestamptz | 最近登录时间 |
| created_at / updated_at | timestamptz | 创建 / 更新时间 |
| deleted_at | timestamptz | 软删除标记 |

## api_keys · API Key

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial | 主键 |
| user_id | bigint | 归属用户 ID |
| name | varchar(64) | 密钥名称 |
| key_hash | varchar(128) | 明文 SHA-256 十六进制摘要，唯一索引，用于反查 |
| prefix | varchar(16) | 密钥前缀，默认 `sk-` |
| masked_key | varchar(64) | 脱敏展示，仅保留末 4 位 |
| status | smallint | 1 正常 / 2 禁用 |
| qpm | int | 每分钟请求上限，0 表示继承全局默认 |
| concurrency | int | 并发上限，0 表示继承全局默认 |
| expires_at | timestamptz | 过期时间，NULL 表示永不过期 |
| revoked_at | timestamptz | 吊销时间，非 NULL 即不可恢复 |
| last_used_at | timestamptz | 最近使用时间，按分钟节流写入 |
| created_at / updated_at / deleted_at | timestamptz | 时间戳与软删除 |

## channels · 上游渠道

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial | 主键 |
| name | varchar(128) | 渠道名称 |
| type | varchar(32) | `openai` 兼容 / `gemini` 原生 |
| base_url | varchar(512) | 上游地址 |
| api_key | text | 上游静态密钥 **AES-256-GCM 密文**；绑定凭证时不生效 |
| models | text | 支持的模型名 JSON 数组，空数组表示通配 |
| model_mapping | text | 模型名映射 JSON：对外名 → 上游名 |
| model_alias | text | 模型别名 JSON：一个上游模型 fork 成多个对外名，可带独立 override |
| request_override | text | 渠道级参数注入 JSON（override-raw），强制合并到上游请求体 |
| param_schema | text | 参数能力声明 JSON：`supported` / `exclude` / `defaults` |
| credential_id | bigint | 绑定的 OAuth 凭证 ID，优先于 api_key |
| weight | int | 负载均衡权重 |
| priority | int | 优先级，仅最高优先级参与选路 |
| status | smallint | 1 正常 / 2 人工禁用 / 3 自动熔断 |
| timeout_seconds | int | 上游超时，0 用全局默认 |
| max_retries | int | 失败重试次数，0 用全局默认 |
| last_error | text | 最近失败原因（截断） |
| fail_count | int | 滑动窗口内连续失败次数 |
| disabled_at | timestamptz | 自动熔断时间 |
| created_at / updated_at / deleted_at | timestamptz | 时间戳与软删除 |

## credentials · OAuth 凭证

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial | 主键 |
| provider | varchar(64) | 供应商标识，如 `anthropic` / `codex` |
| name | varchar(128) | 凭证名称 |
| account | varchar(256) | 第三方账号标识 |
| auth_type | varchar(32) | `oauth` / `file` / `api_key` |
| access_token | text | 访问令牌 **密文** |
| refresh_token | text | 刷新令牌 **密文**，用于自动续期 |
| token_type | varchar(32) | 令牌类型 |
| expires_at | timestamptz | 过期时间 |
| status | smallint | 1 启用 / 2 停用 / 3 失效 |
| quota | text | 配额快照 JSON |
| last_refresh_at | timestamptz | 最近续期时间 |
| last_error | text | 最近错误 |
| file_name | varchar(256) | 导入的认证文件原始文件名 |
| created_at / updated_at / deleted_at | timestamptz | 时间戳与软删除 |

## model_ratios · 模型倍率

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial | 主键 |
| model | varchar(128) | 模型名，`*` 表示兜底 |
| prompt_ratio | double | 点 / 1K prompt tokens |
| completion_ratio | double | 点 / 1K completion tokens（**含思维链**） |
| enabled | boolean | 是否启用 |
| created_at / updated_at / deleted_at | timestamptz | 时间戳与软删除 |

## call_logs · 调用日志

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial | 主键 |
| user_id / api_key_id / channel_id | bigint | 调用方与命中的渠道 |
| channel_name | varchar(128) | 渠道名称快照 |
| model | varchar(128) | 对外模型名 |
| upstream_model | varchar(128) | 经别名/映射改写后的上游模型名 |
| protocol | varchar(32) | `openai` / `gemini` |
| stream | boolean | 是否流式 |
| prompt_tokens | int | 输入 token |
| completion_tokens | int | 输出 token（含思维链） |
| reasoning_tokens | int | 思维链 token，已计入 completion_tokens |
| total_tokens | int | 总 token |
| points | bigint | 实际扣减点数 |
| latency_ms / first_token_ms | bigint | 端到端耗时 / 首字延迟 |
| status | smallint | 1 成功 / 2 失败 |
| http_status | int | HTTP 状态码 |
| error_code / error_message | int / varchar | 失败原因 |
| usage_confidence | varchar(16) | `reported` 上游上报 / `estimated` 按请求参数估算 |
| params_applied / params_dropped | text | 自定义参数已应用 / 已丢弃（逗号分隔统一名） |
| trace_id / client_ip | varchar | 链路追踪与来源 IP |
| created_at | timestamptz | 发生时间 |

## user_identities · 第三方绑定

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial | 主键 |
| user_id | bigint | 本系统用户 ID |
| provider | varchar(64) | 第三方标识，如 `github` |
| provider_uid | varchar(128) | 第三方账号唯一 ID |
| email | varchar(256) | 第三方返回邮箱 |
| display_name | varchar(256) | 第三方展示名 |
| access_token | text | 第三方访问令牌 **密文** |
| created_at / updated_at / deleted_at | timestamptz | 时间戳与解绑软删除 |

## webauthn_credentials · 通行密钥

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial | 主键 |
| user_id | bigint | 归属用户 ID |
| name | varchar(128) | 用户自定义的凭据名称 |
| credential_id | text | WebAuthn 凭证 ID（base64url），唯一 |
| public_key | text | COSE 公钥 JSON 的 **密文** |
| aaguid | varchar(64) | 认证器 AAGUID（base64url） |
| sign_count | bigint | 签名计数，用于克隆检测 |
| transports | text | 支持的传输方式 JSON 数组 |
| created_at | timestamptz | 注册时间 |
| last_used_at | timestamptz | 最近登录使用时间 |

## audit_logs · 审计日志

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| id | bigserial | 主键 |
| user_id | bigint | 操作者，未登录为 0 |
| username | varchar(64) | 操作者用户名快照 |
| action | varchar(64) | 动作，见下表 |
| target_type / target_id | varchar(64) | 目标类型与标识 |
| status | smallint | 1 成功 / 2 失败 |
| detail | text | 补充说明（已脱敏并截断） |
| ip / user_agent | varchar | 来源 IP 与 UA |
| trace_id | varchar(64) | 链路追踪 ID |
| created_at | timestamptz | 发生时间 |

### 审计动作一览

| action | 含义 |
| --- | --- |
| `login` / `register` / `logout` | 登录 / 注册 / 登出 |
| `change_password` | 修改密码 |
| `key.create` / `key.revoke` / `key.delete` | API Key 生命周期 |
| `channel.create` / `channel.update` / `channel.delete` / `channel.test` | 渠道变更 |
| `quota.adjust` | 调整用户额度 |
| `user.create` / `user.status` | 用户创建与启停 |
| `ratio.upsert` / `ratio.delete` | 倍率变更 |
| `credential.import` / `credential.bind` / `credential.delete` | OAuth 凭证 |
| `identity.bind` / `identity.unbind` | 第三方账号绑定 |
| `passkey.register` / `passkey.delete` | 通行密钥 |
