-- 密钥使用统计列

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS request_count BIGINT NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS total_tokens  BIGINT NOT NULL DEFAULT 0;

COMMENT ON COLUMN api_keys.request_count IS '累计请求次数，转发结算时累加';
COMMENT ON COLUMN api_keys.total_tokens  IS '累计 token 消耗，转发结算时累加';
