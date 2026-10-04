-- 渠道协议类型新增 anthropic / claude-code，同步更新列注释。
COMMENT ON COLUMN channels.type IS '协议类型：openai 兼容 / gemini 原生 / anthropic 原生 / claude-code 网关';
