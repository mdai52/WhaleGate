DROP INDEX IF EXISTS idx_channels_credential;
ALTER TABLE channels DROP COLUMN IF EXISTS credential_id;

DROP INDEX IF EXISTS idx_credentials_deleted_at;
DROP INDEX IF EXISTS idx_credentials_status;
DROP INDEX IF EXISTS idx_credentials_provider;
DROP TABLE IF EXISTS credentials;
