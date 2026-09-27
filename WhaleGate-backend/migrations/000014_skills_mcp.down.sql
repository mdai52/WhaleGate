ALTER TABLE call_logs DROP COLUMN IF EXISTS tool_rounds;

DELETE FROM app_settings WHERE setting_key IN ('mcp_enabled', 'mcp_max_rounds', 'skills_enabled');

DROP TABLE IF EXISTS mcp_servers;
DROP TABLE IF EXISTS skills;
