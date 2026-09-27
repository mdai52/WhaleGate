DROP INDEX IF EXISTS idx_call_logs_trace_id;
DROP INDEX IF EXISTS idx_call_logs_created_at;
DROP INDEX IF EXISTS idx_call_logs_model;
DROP INDEX IF EXISTS idx_call_logs_channel_id;
DROP INDEX IF EXISTS idx_call_logs_api_key_id;
DROP INDEX IF EXISTS idx_call_logs_user_id;
DROP TABLE IF EXISTS call_logs;

DROP INDEX IF EXISTS idx_model_ratios_enabled;
DROP INDEX IF EXISTS idx_model_ratios_model;
DROP TABLE IF EXISTS model_ratios;
