-- API Key 分组：把倍率与限流策略打包成预设，供密钥选择。
--
-- 此前 api_keys.group_tag 只是一个自由文本标签，既不参与计费也不参与限流。
-- 现在把它升级为可被管理员配置的实体：密钥记录分组标识，
-- 调用时按该分组叠加倍率系数与 QPM / 并发限制。
--
-- 历史遗留的自由文本标签不会匹配到任何分组，按"中性策略"运行
-- （倍率系数 1.0、不额外限流），因此本迁移无需回填 api_keys 数据。
CREATE TABLE IF NOT EXISTS key_groups (
  id               bigserial PRIMARY KEY,
  name             varchar(64)  NOT NULL,
  display_name     varchar(64)  NOT NULL DEFAULT '',
  description      varchar(255) NOT NULL DEFAULT '',
  -- 倍率系数：在模型倍率算出的点数上相乘，1.0 表示不加价
  ratio_multiplier double precision NOT NULL DEFAULT 1,
  -- 0 表示不设置分组级限制，继续沿用密钥配置与全局默认
  qpm              integer      NOT NULL DEFAULT 0,
  concurrency      integer      NOT NULL DEFAULT 0,
  enabled          boolean      NOT NULL DEFAULT true,
  created_at       timestamptz  NOT NULL DEFAULT now(),
  updated_at       timestamptz  NOT NULL DEFAULT now(),
  -- 软删除：分组被删后其下密钥回落中性策略，但保留记录便于排查
  deleted_at       timestamptz
);

-- 仅对未删除记录做标识唯一约束，避免删除后同名分组无法重新创建
CREATE UNIQUE INDEX IF NOT EXISTS idx_key_groups_name_active
  ON key_groups (name)
  WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_key_groups_deleted_at ON key_groups (deleted_at);

-- 预置一个默认分组，使密钥表单开箱即有可选项
INSERT INTO key_groups (name, display_name, description, ratio_multiplier, enabled)
VALUES ('default', '默认分组', '未单独指定分组的密钥按此策略运行：不加价、不额外限流', 1, true)
ON CONFLICT DO NOTHING;
