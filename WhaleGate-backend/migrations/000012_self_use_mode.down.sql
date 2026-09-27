DROP INDEX IF EXISTS idx_call_logs_self_use;
ALTER TABLE call_logs DROP COLUMN IF EXISTS self_use;
DROP TABLE IF EXISTS app_settings;
