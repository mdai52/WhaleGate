ALTER TABLE model_catalog ALTER COLUMN capabilities TYPE JSONB USING COALESCE(NULLIF(capabilities, '')::JSONB, '[]'::JSONB);
ALTER TABLE channels ALTER COLUMN capabilities TYPE JSONB USING COALESCE(NULLIF(capabilities, '')::JSONB, '[]'::JSONB);
