-- 渠道：模型别名（fork）与请求参数注入（override-raw）

ALTER TABLE channels ADD COLUMN IF NOT EXISTS model_alias      TEXT NOT NULL DEFAULT '{}';
ALTER TABLE channels ADD COLUMN IF NOT EXISTS request_override TEXT NOT NULL DEFAULT '{}';
