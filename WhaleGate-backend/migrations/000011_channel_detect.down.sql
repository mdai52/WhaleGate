DROP INDEX IF EXISTS idx_model_catalog_model_id;
DROP INDEX IF EXISTS idx_model_catalog_provider;
DROP TABLE IF EXISTS model_catalog;

ALTER TABLE channels DROP COLUMN IF EXISTS detected_at;
ALTER TABLE channels DROP COLUMN IF EXISTS max_output;
ALTER TABLE channels DROP COLUMN IF EXISTS context_window;
ALTER TABLE channels DROP COLUMN IF EXISTS capabilities;
