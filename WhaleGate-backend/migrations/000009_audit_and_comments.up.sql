-- 审计日志表 + 全库字典注释

CREATE TABLE IF NOT EXISTS audit_logs (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT       NOT NULL DEFAULT 0,
    username     VARCHAR(64)  NOT NULL DEFAULT '',
    action       VARCHAR(64)  NOT NULL,
    target_type  VARCHAR(64)  NOT NULL DEFAULT '',
    target_id    VARCHAR(64)  NOT NULL DEFAULT '',
    status       SMALLINT     NOT NULL DEFAULT 1,
    detail       TEXT         NOT NULL DEFAULT '',
    ip           VARCHAR(64)  NOT NULL DEFAULT '',
    user_agent   VARCHAR(512) NOT NULL DEFAULT '',
    trace_id     VARCHAR(64)  NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs (user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs (action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs (created_at DESC);

-- ---------------------------------------------------------------- 表注释

COMMENT ON TABLE users IS '用户账号表：门户用户与管理员，密码仅存 bcrypt 摘要';
COMMENT ON TABLE api_keys IS 'API Key 表：网关调用凭证，明文仅创建时返回一次，库中只存 SHA-256 摘要';
COMMENT ON TABLE channels IS '上游渠道表：模型服务的接入配置，上游密钥与注入参数加密存储';
COMMENT ON TABLE credentials IS 'OAuth 凭证表（认证文件）：上游服务令牌，access_token/refresh_token 加密存储';
COMMENT ON TABLE model_ratios IS '模型倍率表：单位「点 / 1K tokens」，1 点 = 0.001 元，* 为兜底';
COMMENT ON TABLE call_logs IS '调用日志表：每次转发的模型、渠道、耗时、token、点数与计费置信度';
COMMENT ON TABLE user_identities IS '第三方身份绑定表：GitHub 等外部账号与本系统账号的关联';
COMMENT ON TABLE webauthn_credentials IS '通行密钥（WebAuthn）凭证表：公钥加密存储，凭据仅保存在用户设备';
COMMENT ON TABLE audit_logs IS '审计日志表：登录、改密、密钥与渠道变更等安全相关操作留痕';

-- ---------------------------------------------------------------- users

COMMENT ON COLUMN users.id IS '主键';
COMMENT ON COLUMN users.username IS '登录用户名，3-64 位字母数字下划线点连字符，唯一';
COMMENT ON COLUMN users.email IS '邮箱，可空，建议唯一；列表接口脱敏返回';
COMMENT ON COLUMN users.password_hash IS 'bcrypt 密码摘要，永不返回给客户端、永不写日志';
COMMENT ON COLUMN users.nickname IS '昵称';
COMMENT ON COLUMN users.role IS '角色：user 普通用户 / admin 管理员';
COMMENT ON COLUMN users.status IS '状态：1 正常 / 2 禁用';
COMMENT ON COLUMN users.quota IS '剩余额度，单位「点」，1 点 = 0.001 元';
COMMENT ON COLUMN users.used_quota IS '累计已消耗额度，单位「点」';
COMMENT ON COLUMN users.request_count IS '累计调用次数';
COMMENT ON COLUMN users.must_change_password IS '是否需要修改初始密码，true 时除改密外拒绝其它已登录接口';
COMMENT ON COLUMN users.last_login_at IS '最近登录时间';
COMMENT ON COLUMN users.created_at IS '创建时间';
COMMENT ON COLUMN users.updated_at IS '更新时间';
COMMENT ON COLUMN users.deleted_at IS '软删除标记';

-- ---------------------------------------------------------------- api_keys

COMMENT ON COLUMN api_keys.id IS '主键';
COMMENT ON COLUMN api_keys.user_id IS '归属用户 ID';
COMMENT ON COLUMN api_keys.name IS '密钥名称，便于用户区分用途';
COMMENT ON COLUMN api_keys.key_hash IS '明文密钥的 SHA-256 十六进制摘要，唯一索引，用于反查';
COMMENT ON COLUMN api_keys.prefix IS '密钥前缀，默认 sk-';
COMMENT ON COLUMN api_keys.masked_key IS '脱敏展示密钥，仅保留末 4 位';
COMMENT ON COLUMN api_keys.status IS '状态：1 正常 / 2 禁用';
COMMENT ON COLUMN api_keys.qpm IS '每分钟请求上限，0 表示继承全局默认';
COMMENT ON COLUMN api_keys.concurrency IS '并发上限，0 表示继承全局默认';
COMMENT ON COLUMN api_keys.expires_at IS '过期时间，NULL 表示永不过期';
COMMENT ON COLUMN api_keys.revoked_at IS '吊销时间，非 NULL 表示已吊销且不可恢复';
COMMENT ON COLUMN api_keys.last_used_at IS '最近使用时间，按分钟节流写入';
COMMENT ON COLUMN api_keys.created_at IS '创建时间';
COMMENT ON COLUMN api_keys.updated_at IS '更新时间';
COMMENT ON COLUMN api_keys.deleted_at IS '软删除标记';

-- ---------------------------------------------------------------- channels

COMMENT ON COLUMN channels.id IS '主键';
COMMENT ON COLUMN channels.name IS '渠道名称';
COMMENT ON COLUMN channels.type IS '协议类型：openai 兼容 / gemini 原生';
COMMENT ON COLUMN channels.base_url IS '上游地址，如 https://api.openai.com/v1';
COMMENT ON COLUMN channels.api_key IS '上游静态密钥，AES-256-GCM 密文；绑定凭证时该值不生效';
COMMENT ON COLUMN channels.models IS '支持的模型名 JSON 数组，空数组表示通配';
COMMENT ON COLUMN channels.model_mapping IS '模型名映射 JSON，键为对外模型名，值为上游模型名';
COMMENT ON COLUMN channels.model_alias IS '模型别名 JSON：一个上游模型 fork 成多个对外模型名，可带独立 override';
COMMENT ON COLUMN channels.request_override IS '渠道级请求参数注入 JSON，强制合并到上游请求体（override-raw）';
COMMENT ON COLUMN channels.param_schema IS '参数能力声明 JSON：supported 支持列表、exclude 排除、defaults 默认值';
COMMENT ON COLUMN channels.credential_id IS '绑定的 OAuth 凭证 ID，优先于 api_key';
COMMENT ON COLUMN channels.weight IS '负载均衡权重，越大被选中概率越高';
COMMENT ON COLUMN channels.priority IS '优先级，仅最高优先级的渠道参与选路';
COMMENT ON COLUMN channels.status IS '状态：1 正常 / 2 人工禁用 / 3 连续失败自动熔断';
COMMENT ON COLUMN channels.timeout_seconds IS '上游超时秒数，0 表示使用全局默认';
COMMENT ON COLUMN channels.max_retries IS '失败重试次数，0 表示使用全局默认';
COMMENT ON COLUMN channels.last_error IS '最近一次失败原因，截断存储';
COMMENT ON COLUMN channels.fail_count IS '滑动窗口内连续失败次数，达到阈值自动熔断';
COMMENT ON COLUMN channels.disabled_at IS '自动熔断时间';
COMMENT ON COLUMN channels.created_at IS '创建时间';
COMMENT ON COLUMN channels.updated_at IS '更新时间';
COMMENT ON COLUMN channels.deleted_at IS '软删除标记';

-- ---------------------------------------------------------------- credentials

COMMENT ON COLUMN credentials.id IS '主键';
COMMENT ON COLUMN credentials.provider IS '供应商标识，如 anthropic / codex';
COMMENT ON COLUMN credentials.name IS '凭证名称';
COMMENT ON COLUMN credentials.account IS '第三方账号标识（邮箱或账户 ID）';
COMMENT ON COLUMN credentials.auth_type IS '来源类型：oauth 授权 / file 上传认证文件 / api_key';
COMMENT ON COLUMN credentials.access_token IS '访问令牌，AES-256-GCM 密文';
COMMENT ON COLUMN credentials.refresh_token IS '刷新令牌，AES-256-GCM 密文，用于自动续期';
COMMENT ON COLUMN credentials.token_type IS '令牌类型，如 bearer';
COMMENT ON COLUMN credentials.expires_at IS '过期时间';
COMMENT ON COLUMN credentials.status IS '状态：1 启用 / 2 停用 / 3 失效';
COMMENT ON COLUMN credentials.quota IS '配额快照 JSON';
COMMENT ON COLUMN credentials.last_refresh_at IS '最近续期时间';
COMMENT ON COLUMN credentials.last_error IS '最近错误';
COMMENT ON COLUMN credentials.file_name IS '导入的认证文件原始文件名';
COMMENT ON COLUMN credentials.created_at IS '创建时间';
COMMENT ON COLUMN credentials.updated_at IS '更新时间';
COMMENT ON COLUMN credentials.deleted_at IS '软删除标记';

-- ---------------------------------------------------------------- model_ratios

COMMENT ON COLUMN model_ratios.id IS '主键';
COMMENT ON COLUMN model_ratios.model IS '模型名，* 表示兜底倍率';
COMMENT ON COLUMN model_ratios.prompt_ratio IS '输入倍率：点 / 1K prompt tokens';
COMMENT ON COLUMN model_ratios.completion_ratio IS '输出倍率：点 / 1K completion tokens（含思维链）';
COMMENT ON COLUMN model_ratios.enabled IS '是否启用';
COMMENT ON COLUMN model_ratios.created_at IS '创建时间';
COMMENT ON COLUMN model_ratios.updated_at IS '更新时间';
COMMENT ON COLUMN model_ratios.deleted_at IS '软删除标记';

-- ---------------------------------------------------------------- call_logs

COMMENT ON COLUMN call_logs.id IS '主键';
COMMENT ON COLUMN call_logs.user_id IS '调用用户 ID';
COMMENT ON COLUMN call_logs.api_key_id IS '使用的 API Key ID';
COMMENT ON COLUMN call_logs.channel_id IS '命中的渠道 ID';
COMMENT ON COLUMN call_logs.channel_name IS '渠道名称快照';
COMMENT ON COLUMN call_logs.model IS '对外模型名';
COMMENT ON COLUMN call_logs.upstream_model IS '经别名/映射改写后的上游模型名';
COMMENT ON COLUMN call_logs.protocol IS '客户端协议：openai / gemini';
COMMENT ON COLUMN call_logs.stream IS '是否流式';
COMMENT ON COLUMN call_logs.prompt_tokens IS '输入 token';
COMMENT ON COLUMN call_logs.completion_tokens IS '输出 token（含思维链）';
COMMENT ON COLUMN call_logs.reasoning_tokens IS '思维链 token，已包含在 completion_tokens 内';
COMMENT ON COLUMN call_logs.total_tokens IS '总 token';
COMMENT ON COLUMN call_logs.points IS '实际扣减点数';
COMMENT ON COLUMN call_logs.latency_ms IS '端到端耗时毫秒';
COMMENT ON COLUMN call_logs.first_token_ms IS '首字延迟毫秒，非流式或缺失时为 0';
COMMENT ON COLUMN call_logs.status IS '状态：1 成功 / 2 失败';
COMMENT ON COLUMN call_logs.http_status IS 'HTTP 状态码';
COMMENT ON COLUMN call_logs.error_code IS '失败时的业务错误码';
COMMENT ON COLUMN call_logs.error_message IS '失败原因';
COMMENT ON COLUMN call_logs.usage_confidence IS '用量置信度：reported 上游上报 / estimated 按请求参数估算';
COMMENT ON COLUMN call_logs.params_applied IS '已写入上游的自定义参数（逗号分隔统一参数名）';
COMMENT ON COLUMN call_logs.params_dropped IS '因上游不支持而丢弃的自定义参数';
COMMENT ON COLUMN call_logs.trace_id IS '链路追踪 ID';
COMMENT ON COLUMN call_logs.client_ip IS '客户端 IP';
COMMENT ON COLUMN call_logs.created_at IS '创建时间';

-- ---------------------------------------------------------------- user_identities

COMMENT ON COLUMN user_identities.id IS '主键';
COMMENT ON COLUMN user_identities.user_id IS '本系统用户 ID';
COMMENT ON COLUMN user_identities.provider IS '第三方标识，如 github';
COMMENT ON COLUMN user_identities.provider_uid IS '第三方账号唯一 ID';
COMMENT ON COLUMN user_identities.email IS '第三方返回的邮箱';
COMMENT ON COLUMN user_identities.display_name IS '第三方展示名';
COMMENT ON COLUMN user_identities.access_token IS '第三方访问令牌，AES-256-GCM 密文';
COMMENT ON COLUMN user_identities.created_at IS '绑定时间';
COMMENT ON COLUMN user_identities.updated_at IS '更新时间';
COMMENT ON COLUMN user_identities.deleted_at IS '解绑软删除标记';

-- ---------------------------------------------------------------- webauthn_credentials

COMMENT ON COLUMN webauthn_credentials.id IS '主键';
COMMENT ON COLUMN webauthn_credentials.user_id IS '归属用户 ID';
COMMENT ON COLUMN webauthn_credentials.name IS '用户自定义的凭据名称';
COMMENT ON COLUMN webauthn_credentials.credential_id IS 'WebAuthn 凭证 ID（base64url），唯一';
COMMENT ON COLUMN webauthn_credentials.public_key IS 'COSE 公钥 JSON 的 AES-256-GCM 密文';
COMMENT ON COLUMN webauthn_credentials.aaguid IS '认证器 AAGUID（base64url）';
COMMENT ON COLUMN webauthn_credentials.sign_count IS '签名计数，用于克隆检测';
COMMENT ON COLUMN webauthn_credentials.transports IS '支持的传输方式 JSON 数组';
COMMENT ON COLUMN webauthn_credentials.created_at IS '注册时间';
COMMENT ON COLUMN webauthn_credentials.last_used_at IS '最近登录使用时间';

-- ---------------------------------------------------------------- audit_logs

COMMENT ON COLUMN audit_logs.id IS '主键';
COMMENT ON COLUMN audit_logs.user_id IS '操作者用户 ID，未登录为 0';
COMMENT ON COLUMN audit_logs.username IS '操作者用户名快照';
COMMENT ON COLUMN audit_logs.action IS '动作，如 login.success / key.create / channel.delete';
COMMENT ON COLUMN audit_logs.target_type IS '目标类型，如 user / api_key / channel';
COMMENT ON COLUMN audit_logs.target_id IS '目标标识';
COMMENT ON COLUMN audit_logs.status IS '状态：1 成功 / 2 失败';
COMMENT ON COLUMN audit_logs.detail IS '补充说明，已脱敏且截断';
COMMENT ON COLUMN audit_logs.ip IS '操作来源 IP';
COMMENT ON COLUMN audit_logs.user_agent IS '操作来源 User-Agent，截断存储';
COMMENT ON COLUMN audit_logs.trace_id IS '链路追踪 ID';
COMMENT ON COLUMN audit_logs.created_at IS '发生时间';
