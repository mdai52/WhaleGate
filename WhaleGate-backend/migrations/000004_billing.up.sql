-- 计量计费：模型倍率表与调用日志表

CREATE TABLE IF NOT EXISTS model_ratios (
    id               BIGSERIAL PRIMARY KEY,
    model            VARCHAR(128) NOT NULL,
    prompt_ratio     DOUBLE PRECISION NOT NULL DEFAULT 0,
    completion_ratio DOUBLE PRECISION NOT NULL DEFAULT 0,
    enabled          BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_model_ratios_model ON model_ratios (model) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_model_ratios_enabled ON model_ratios (enabled);

-- 预置常见模型倍率（点 / 1K tokens，1 点 = 0.001 元）
INSERT INTO model_ratios (model, prompt_ratio, completion_ratio) VALUES
    ('*', 15, 60),
    ('gpt-4o', 25, 100),
    ('gpt-4o-mini', 1.5, 6),
    ('gemini-2.0-flash', 1, 4),
    ('gemini-1.5-pro', 12, 48)
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS call_logs (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT       NOT NULL DEFAULT 0,
    api_key_id       BIGINT       NOT NULL DEFAULT 0,
    channel_id       BIGINT       NOT NULL DEFAULT 0,
    channel_name     VARCHAR(128) NOT NULL DEFAULT '',
    model            VARCHAR(128) NOT NULL DEFAULT '',
    upstream_model   VARCHAR(128) NOT NULL DEFAULT '',
    protocol         VARCHAR(32)  NOT NULL DEFAULT '',
    stream           BOOLEAN      NOT NULL DEFAULT FALSE,
    prompt_tokens    INTEGER      NOT NULL DEFAULT 0,
    completion_tokens INTEGER     NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER      NOT NULL DEFAULT 0,
    total_tokens     INTEGER      NOT NULL DEFAULT 0,
    points           BIGINT       NOT NULL DEFAULT 0,
    latency_ms       BIGINT       NOT NULL DEFAULT 0,
    first_token_ms   BIGINT       NOT NULL DEFAULT 0,
    status           SMALLINT     NOT NULL DEFAULT 1,
    http_status      INTEGER      NOT NULL DEFAULT 0,
    error_code       INTEGER      NOT NULL DEFAULT 0,
    error_message    VARCHAR(512) NOT NULL DEFAULT '',
    usage_confidence VARCHAR(16)  NOT NULL DEFAULT 'reported',
    trace_id         VARCHAR(64)  NOT NULL DEFAULT '',
    client_ip        VARCHAR(64)  NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_call_logs_user_id ON call_logs (user_id);
CREATE INDEX IF NOT EXISTS idx_call_logs_api_key_id ON call_logs (api_key_id);
CREATE INDEX IF NOT EXISTS idx_call_logs_channel_id ON call_logs (channel_id);
CREATE INDEX IF NOT EXISTS idx_call_logs_model ON call_logs (model);
CREATE INDEX IF NOT EXISTS idx_call_logs_created_at ON call_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_call_logs_trace_id ON call_logs (trace_id);
