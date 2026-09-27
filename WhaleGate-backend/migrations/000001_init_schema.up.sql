-- 初始结构：用户与 API Key

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS users (
    id             BIGSERIAL PRIMARY KEY,
    username       VARCHAR(64)  NOT NULL,
    email          VARCHAR(128) DEFAULT '',
    password_hash  VARCHAR(128) NOT NULL,
    nickname       VARCHAR(64)  DEFAULT '',
    role           VARCHAR(32)  NOT NULL DEFAULT 'user',
    status         SMALLINT     NOT NULL DEFAULT 1,
    quota          BIGINT       NOT NULL DEFAULT 0,
    used_quota     BIGINT       NOT NULL DEFAULT 0,
    request_count  BIGINT       NOT NULL DEFAULT 0,
    last_login_at  TIMESTAMPTZ,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users (username) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users (email) WHERE deleted_at IS NULL AND email <> '';
CREATE INDEX IF NOT EXISTS idx_users_role ON users (role);
CREATE INDEX IF NOT EXISTS idx_users_deleted_at ON users (deleted_at);

CREATE TABLE IF NOT EXISTS api_keys (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT       NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name         VARCHAR(64)  NOT NULL,
    key_hash     VARCHAR(128) NOT NULL,
    prefix       VARCHAR(16)  NOT NULL DEFAULT 'sk-',
    masked_key   VARCHAR(64)  NOT NULL,
    status       SMALLINT     NOT NULL DEFAULT 1,
    qpm          INTEGER      NOT NULL DEFAULT 0,
    concurrency  INTEGER      NOT NULL DEFAULT 0,
    expires_at   TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys (key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys (user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_status ON api_keys (status);
CREATE INDEX IF NOT EXISTS idx_api_keys_deleted_at ON api_keys (deleted_at);
