-- 将 JSONB 能力列统一为 TEXT（避免 GORM string 写入 jsonb 的类型转换问题）

ALTER TABLE channels ALTER COLUMN capabilities TYPE TEXT USING COALESCE(capabilities::TEXT, '[]');
ALTER TABLE model_catalog ALTER COLUMN capabilities TYPE TEXT USING COALESCE(capabilities::TEXT, '[]');

UPDATE channels SET capabilities = '[]' WHERE capabilities IS NULL OR capabilities = '';
UPDATE model_catalog SET capabilities = '[]' WHERE capabilities IS NULL OR capabilities = '';
