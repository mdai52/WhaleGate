-- OAuth 凭证（认证文件）管理

CREATE TABLE IF NOT EXISTS credentials (
    id              BIGSERIAL PRIMARY KEY,
    provider        VARCHAR(64)  NOT NULL,
    name            VARCHAR(128) NOT NULL,
    account         VARCHAR(256) NOT NULL DEFAULT '',
    auth_type       VARCHAR(32)  NOT NULL DEFAULT 'oauth',
    access_token    TEXT         NOT NULL DEFAULT '',
    refresh_token   TEXT         NOT NULL DEFAULT '',
    token_type      VARCHAR(32)  NOT NULL DEFAULT '',
    expires_at      TIMESTAMPTZ,
    status          SMALLINT     NOT NULL DEFAULT 1,
    quota           TEXT         NOT NULL DEFAULT '{}',
    last_refresh_at TIMESTAMPTZ,
    last_error      TEXT         NOT NULL DEFAULT '',
    file_name       VARCHAR(256) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_credentials_provider ON credentials (provider);
CREATE INDEX IF NOT EXISTS idx_credentials_status ON credentials (status);
CREATE INDEX IF NOT EXISTS idx_credentials_deleted_at ON credentials (deleted_at);

-- 渠道可引用凭证（凭证优先于静态 api_key）
ALTER TABLE channels ADD COLUMN IF NOT EXISTS credential_id BIGINT;
CREATE INDEX IF NOT EXISTS idx_channels_credential ON channels (credential_id);
