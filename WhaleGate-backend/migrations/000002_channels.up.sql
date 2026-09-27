-- 渠道表：上游模型服务配置（密钥以密文存储）

CREATE TABLE IF NOT EXISTS channels (
    id              BIGSERIAL PRIMARY KEY,
    name            VARCHAR(128) NOT NULL,
    type            VARCHAR(32)  NOT NULL DEFAULT 'openai',
    base_url        VARCHAR(512) NOT NULL,
    api_key         TEXT         NOT NULL DEFAULT '',
    models          TEXT         NOT NULL DEFAULT '[]',
    model_mapping   TEXT         NOT NULL DEFAULT '{}',
    weight          INTEGER      NOT NULL DEFAULT 1,
    priority        INTEGER      NOT NULL DEFAULT 0,
    status          SMALLINT     NOT NULL DEFAULT 1,
    timeout_seconds INTEGER      NOT NULL DEFAULT 0,
    max_retries     INTEGER      NOT NULL DEFAULT 0,
    last_error      TEXT         NOT NULL DEFAULT '',
    fail_count      INTEGER      NOT NULL DEFAULT 0,
    disabled_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_channels_status ON channels (status);
CREATE INDEX IF NOT EXISTS idx_channels_type ON channels (type);
CREATE INDEX IF NOT EXISTS idx_channels_priority ON channels (priority DESC);
CREATE INDEX IF NOT EXISTS idx_channels_deleted_at ON channels (deleted_at);
