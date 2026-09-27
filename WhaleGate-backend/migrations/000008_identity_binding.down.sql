DROP INDEX IF EXISTS idx_webauthn_user_id;
DROP INDEX IF EXISTS idx_webauthn_credential_id;
DROP TABLE IF EXISTS webauthn_credentials;

DROP INDEX IF EXISTS idx_user_identities_user_id;
DROP INDEX IF EXISTS idx_user_identities_provider;
DROP TABLE IF EXISTS user_identities;
