ALTER TABLE api_keys DROP COLUMN IF EXISTS allowed_models;
ALTER TABLE api_keys DROP COLUMN IF EXISTS used_points;
ALTER TABLE api_keys DROP COLUMN IF EXISTS quota_limit;
ALTER TABLE api_keys DROP COLUMN IF EXISTS ip_whitelist;
ALTER TABLE api_keys DROP COLUMN IF EXISTS group_tag;
