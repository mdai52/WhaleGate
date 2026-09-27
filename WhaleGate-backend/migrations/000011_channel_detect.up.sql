-- 渠道自动探测：能力/上下文/探测时间 + 全局模型目录

ALTER TABLE channels ADD COLUMN IF NOT EXISTS capabilities   TEXT NOT NULL DEFAULT '[]';
ALTER TABLE channels ADD COLUMN IF NOT EXISTS context_window INT NOT NULL DEFAULT 0;
ALTER TABLE channels ADD COLUMN IF NOT EXISTS max_output     INT NOT NULL DEFAULT 0;
ALTER TABLE channels ADD COLUMN IF NOT EXISTS detected_at    TIMESTAMPTZ;

COMMENT ON COLUMN channels.capabilities   IS '探测得到的渠道能力集合';
COMMENT ON COLUMN channels.context_window IS '上游声明的最大上下文窗口';
COMMENT ON COLUMN channels.max_output     IS '上游声明的最大输出 token';
COMMENT ON COLUMN channels.detected_at    IS '最近一次自动探测时间';

CREATE TABLE IF NOT EXISTS model_catalog (
  id             BIGSERIAL PRIMARY KEY,
  model_id       TEXT NOT NULL UNIQUE,
  display_name   TEXT NOT NULL DEFAULT '',
  provider_type  TEXT NOT NULL DEFAULT '',
  capabilities   TEXT NOT NULL DEFAULT '[]',
  context_window INT NOT NULL DEFAULT 0,
  max_output     INT NOT NULL DEFAULT 0,
  source         TEXT NOT NULL DEFAULT 'detect',
  created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_model_catalog_provider ON model_catalog (provider_type);
CREATE INDEX IF NOT EXISTS idx_model_catalog_model_id ON model_catalog (model_id text_pattern_ops);

COMMENT ON TABLE model_catalog IS '全局模型目录：自动探测与手动登记的模型元数据，供渠道/倍率配置下拉选择';
