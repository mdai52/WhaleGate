-- 调用日志增加请求协议字段，用于区分 HTTP/HTTPS 入站请求。
ALTER TABLE call_logs ADD COLUMN IF NOT EXISTS request_scheme VARCHAR(8) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_call_logs_request_scheme ON call_logs (request_scheme);
