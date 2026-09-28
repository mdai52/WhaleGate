DROP INDEX IF EXISTS idx_call_logs_request_scheme;
ALTER TABLE call_logs DROP COLUMN IF EXISTS request_scheme;
