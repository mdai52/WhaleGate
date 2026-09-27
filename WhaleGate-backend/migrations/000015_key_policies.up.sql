-- 密钥增强配置：分组、IP 白名单、额度限制、模型白名单

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS group_tag      TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS ip_whitelist   TEXT NOT NULL DEFAULT '[]';
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS quota_limit    BIGINT NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS used_points    BIGINT NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS allowed_models TEXT NOT NULL DEFAULT '[]';

COMMENT ON COLUMN api_keys.group_tag      IS '密钥分组标签，便于分类管理';
COMMENT ON COLUMN api_keys.ip_whitelist   IS 'IP 白名单（JSON 数组，支持 CIDR），空=不限制';
COMMENT ON COLUMN api_keys.quota_limit    IS '密钥级额度上限（点），0=不限';
COMMENT ON COLUMN api_keys.used_points    IS '该密钥累计消耗点数';
COMMENT ON COLUMN api_keys.allowed_models IS '模型白名单（JSON 数组），空=不限制';
