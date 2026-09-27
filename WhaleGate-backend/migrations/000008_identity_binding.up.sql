-- 第三方身份绑定与通行密钥（WebAuthn）

CREATE TABLE IF NOT EXISTS user_identities (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT       NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider        VARCHAR(64)  NOT NULL,
    provider_uid    VARCHAR(128) NOT NULL,
    email           VARCHAR(256) NOT NULL DEFAULT '',
    display_name    VARCHAR(256) NOT NULL DEFAULT '',
    -- 第三方访问令牌，AES-256-GCM 密文
    access_token    TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at      TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_user_identities_provider ON user_identities (provider, provider_uid) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_user_identities_user_id ON user_identities (user_id);

CREATE TABLE IF NOT EXISTS webauthn_credentials (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT       NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name          VARCHAR(128) NOT NULL DEFAULT '通行密钥',
    -- WebAuthn 凭证 ID（base64url）
    credential_id TEXT         NOT NULL,
    -- COSE 公钥（JSON 序列化后加密存储）
    public_key    TEXT         NOT NULL,
    aaguid        VARCHAR(64)  NOT NULL DEFAULT '',
    sign_count    BIGINT       NOT NULL DEFAULT 0,
    transports    TEXT         NOT NULL DEFAULT '[]',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_webauthn_credential_id ON webauthn_credentials (credential_id);
CREATE INDEX IF NOT EXISTS idx_webauthn_user_id ON webauthn_credentials (user_id);
