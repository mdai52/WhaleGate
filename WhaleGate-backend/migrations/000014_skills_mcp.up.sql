-- Skills 与 MCP 工具注册表

CREATE TABLE IF NOT EXISTS skills (
  id           BIGSERIAL PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL DEFAULT '',
  description  TEXT NOT NULL DEFAULT '',
  content      TEXT NOT NULL DEFAULT '',
  mode         TEXT NOT NULL DEFAULT 'always',
  enabled      BOOLEAN NOT NULL DEFAULT true,
  source       TEXT NOT NULL DEFAULT 'dir',
  file_path    TEXT NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE skills IS 'Skills：SKILL.md 解析后的技能正文，可注入 system 提示词';
COMMENT ON COLUMN skills.mode IS 'always=正文全量注入 system；index=仅注入名称与描述索引';

CREATE TABLE IF NOT EXISTS mcp_servers (
  id           BIGSERIAL PRIMARY KEY,
  name         TEXT NOT NULL UNIQUE,
  transport    TEXT NOT NULL DEFAULT 'stdio',
  command      TEXT NOT NULL DEFAULT '',
  args         TEXT NOT NULL DEFAULT '[]',
  env          TEXT NOT NULL DEFAULT '{}',
  url          TEXT NOT NULL DEFAULT '',
  headers      TEXT NOT NULL DEFAULT '{}',
  enabled      BOOLEAN NOT NULL DEFAULT true,
  auto_execute BOOLEAN NOT NULL DEFAULT false,
  status       TEXT NOT NULL DEFAULT 'idle',
  last_error   TEXT NOT NULL DEFAULT '',
  tools        TEXT NOT NULL DEFAULT '[]',
  tool_count   INT NOT NULL DEFAULT 0,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMENT ON TABLE mcp_servers IS 'MCP Server 配置：连接参数、工具缓存与健康状态';
COMMENT ON COLUMN mcp_servers.auto_execute IS 'true=网关代执行多轮工具循环；false=仅透传工具定义给客户端';

-- 全局设置默认值
INSERT INTO app_settings (setting_key, value) VALUES
  ('mcp_enabled', 'false'),
  ('mcp_max_rounds', '5'),
  ('skills_enabled', 'false')
  ON CONFLICT (setting_key) DO NOTHING;

-- 多轮工具循环的轮次信息
ALTER TABLE call_logs ADD COLUMN IF NOT EXISTS tool_rounds INT NOT NULL DEFAULT 0;
COMMENT ON COLUMN call_logs.tool_rounds IS '多轮工具执行轮次，0 表示未使用工具';
