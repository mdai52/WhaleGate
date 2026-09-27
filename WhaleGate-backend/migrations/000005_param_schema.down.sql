ALTER TABLE call_logs DROP COLUMN IF EXISTS params_dropped;
ALTER TABLE call_logs DROP COLUMN IF EXISTS params_applied;
ALTER TABLE channels DROP COLUMN IF EXISTS param_schema;
