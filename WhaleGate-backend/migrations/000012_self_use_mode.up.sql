-- 自用模式（全局设置）与调用日志的自用标记

CREATE TABLE IF NOT EXISTS app_settings (
  setting_key TEXT PRIMARY KEY,
  value       TEXT NOT NULL DEFAULT '',
  updated_by  BIGINT,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE app_settings IS '全局运行时设置（管理员可改），key-value 形式';

-- 默认值：自用模式关闭
INSERT INTO app_settings (setting_key, value) VALUES ('self_use_mode', 'false')
  ON CONFLICT (setting_key) DO NOTHING;

ALTER TABLE call_logs ADD COLUMN IF NOT EXISTS self_use BOOLEAN NOT NULL DEFAULT false;
COMMENT ON COLUMN call_logs.self_use IS '该次调用发生在自用模式下（记录用量但不扣费）';

CREATE INDEX IF NOT EXISTS idx_call_logs_self_use ON call_logs (self_use) WHERE self_use;
