package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/mcp"
)

// marshalJSON 序列化为 JSON 文本（失败时返回原值）。
func marshalJSON(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// 工具名分段符：对外暴露名形如 mcp__server__tool
const toolNameSep = "__"

// 连接缓存：server ID -> 客户端
var (
	mcpMu      sync.Mutex
	mcpClients = map[uint]*mcp.Client{}
)

// ErrMCPDisabled MCP 未启用。
var ErrMCPDisabled = apierr.New(apierr.ErrForbidden, "MCP 未启用")

// ---------------------------------------------------------------- 配置 CRUD

// MCPServerInput MCP Server 入参。
type MCPServerInput struct {
	Name        string            `json:"name"`
	Transport   string            `json:"transport"`
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Enabled     *bool             `json:"enabled"`
	AutoExecute *bool             `json:"auto_execute"`
}

// secretMask 敏感配置回显占位符。
const secretMask = "***"

// decodeSecretMap 解析敏感键值文本。
func decodeSecretMap(raw string) map[string]string {
	if raw == "" {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// mcpEnvMap 解密并解析 stdio 环境变量。
//
// 环境变量里常放 token 与 API Key，库里存的是密文；
// 解密失败时按早期版本的明文兼容。
func (c *Container) mcpEnvMap(s *model.MCPServer) map[string]string {
	if s == nil {
		return nil
	}
	return decodeSecretMap(c.decryptSetting(s.Env))
}

// mcpHeaderMap 解密并解析 HTTP 附加请求头（常承载 Authorization）。
func (c *Container) mcpHeaderMap(s *model.MCPServer) map[string]string {
	if s == nil {
		return nil
	}
	return decodeSecretMap(c.decryptSetting(s.Headers))
}

// mergeSecretMap 合并敏感键值后加密落库。
//
// 值为占位符时沿用库里已有的明文：管理端表单往往整份提交，
// 不这样处理的话"只改名称、原样保存"会把真实 token 覆盖成 ***。
func (c *Container) mergeSecretMap(stored string, in map[string]string) string {
	current := decodeSecretMap(c.decryptSetting(stored))
	if current == nil {
		current = map[string]string{}
	}
	for k, v := range in {
		if v == secretMask {
			if old, ok := current[k]; ok {
				current[k] = old
			} else {
				delete(current, k)
			}
			continue
		}
		current[k] = v
	}
	data, err := json.Marshal(current)
	if err != nil {
		return stored
	}
	return c.encryptSetting(string(data))
}

// MaskMCPServer 返回一份可安全回显给管理端的副本：
// 环境变量与请求头只保留键名，值统一替换为占位符。
func (c *Container) MaskMCPServer(s *model.MCPServer) *model.MCPServer {
	if s == nil {
		return nil
	}
	out := *s
	out.Env = c.maskSecretMap(s.Env)
	out.Headers = c.maskSecretMap(s.Headers)
	return &out
}

// maskSecretMap 解密后只保留键名，值替换为占位符。
func (c *Container) maskSecretMap(stored string) string {
	m := decodeSecretMap(c.decryptSetting(stored))
	if len(m) == 0 {
		return "{}"
	}
	for k := range m {
		m[k] = secretMask
	}
	data, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// ListMCPServers 列出全部 Server。
func (c *Container) ListMCPServers(ctx context.Context) ([]model.MCPServer, error) {
	var list []model.MCPServer
	if err := c.DB.WithContext(ctx).Order("id ASC").Find(&list).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return list, nil
}

// GetMCPServer 读取单个 Server。
func (c *Container) GetMCPServer(ctx context.Context, id uint) (*model.MCPServer, error) {
	var s model.MCPServer
	if err := c.DB.WithContext(ctx).First(&s, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	return &s, nil
}

// CreateMCPServer 新增 Server。
func (c *Container) CreateMCPServer(ctx context.Context, in MCPServerInput) (*model.MCPServer, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "名称不能为空")
	}
	s := &model.MCPServer{
		Name:      strings.TrimSpace(in.Name),
		Transport: normalizeTransport(in.Transport),
		Command:   strings.TrimSpace(in.Command),
		Args:      "[]",
		Env:       "{}",
		Headers:   "{}",
		URL:       strings.TrimSpace(in.URL),
		Enabled:   true,
		Status:    model.MCPStatusIdle,
		Tools:     "[]",
	}
	if err := s.SetToolList(nil); err != nil {
		return nil, err
	}
	if len(in.Args) > 0 {
		if data, err := marshalJSON(in.Args); err == nil {
			s.Args = data
		}
	}
	if len(in.Env) > 0 {
		if data, err := marshalJSON(in.Env); err == nil {
			s.Env = c.encryptSetting(data)
		}
	}
	if len(in.Headers) > 0 {
		if data, err := marshalJSON(in.Headers); err == nil {
			s.Headers = c.encryptSetting(data)
		}
	}
	if in.Enabled != nil {
		s.Enabled = *in.Enabled
	}
	if in.AutoExecute != nil {
		s.AutoExecute = *in.AutoExecute
	}
	if err := c.DB.WithContext(ctx).Create(s).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return s, nil
}

// UpdateMCPServer 更新 Server 配置；变更连接参数时断开旧连接。
func (c *Container) UpdateMCPServer(ctx context.Context, id uint, in MCPServerInput) (*model.MCPServer, error) {
	s, err := c.GetMCPServer(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.Name) != "" {
		s.Name = strings.TrimSpace(in.Name)
	}
	if in.Transport != "" {
		s.Transport = normalizeTransport(in.Transport)
	}
	if in.Command != "" {
		s.Command = strings.TrimSpace(in.Command)
	}
	if in.Args != nil {
		if data, err := marshalJSON(in.Args); err == nil {
			s.Args = data
		}
	}
	if in.Env != nil {
		s.Env = c.mergeSecretMap(s.Env, in.Env)
	}
	if in.URL != "" {
		s.URL = strings.TrimSpace(in.URL)
	}
	if in.Headers != nil {
		s.Headers = c.mergeSecretMap(s.Headers, in.Headers)
	}
	if in.Enabled != nil {
		s.Enabled = *in.Enabled
	}
	if in.AutoExecute != nil {
		s.AutoExecute = *in.AutoExecute
	}
	if err := c.DB.WithContext(ctx).Save(s).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	// 连接参数变更：断开缓存连接，下次按需重连
	c.DisconnectMCP(id)
	return s, nil
}

// DeleteMCPServer 删除 Server 并断开连接。
func (c *Container) DeleteMCPServer(ctx context.Context, id uint) error {
	c.DisconnectMCP(id)
	if err := c.DB.WithContext(ctx).Delete(&model.MCPServer{}, id).Error; err != nil {
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	return nil
}

func normalizeTransport(t string) string {
	if strings.EqualFold(t, model.MCPTransportHTTP) {
		return model.MCPTransportHTTP
	}
	return model.MCPTransportStdio
}

// ---------------------------------------------------------------- 连接与工具

// ConnectMCP 建立连接并拉取工具列表，成功后写入状态与工具缓存。
func (c *Container) ConnectMCP(ctx context.Context, id uint) (*model.MCPServer, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s, err := c.GetMCPServer(ctx, id)
	if err != nil {
		return nil, err
	}
	if !s.Enabled {
		return nil, apierr.New(apierr.ErrForbidden, "该 MCP 服务已禁用")
	}

	client, err := c.dialMCP(ctx, s)
	if err != nil {
		c.markMCPError(ctx, s, err)
		return nil, apierr.Errorf(apierr.ErrUpstream, "连接失败：%s", err.Error())
	}

	tools, err := client.ListTools(ctx)
	if err != nil {
		_ = client.Close()
		c.markMCPError(ctx, s, err)
		return nil, apierr.Errorf(apierr.ErrUpstream, "拉取工具失败：%s", err.Error())
	}

	list := make([]model.MCPTool, 0, len(tools))
	for _, t := range tools {
		list = append(list, model.MCPTool{
			Name:        t.Name,
			Description: t.Description,
			Schema:      t.InputSchema,
			Server:      s.Name,
		})
	}
	if err := s.SetToolList(list); err != nil {
		return nil, apierr.Wrap(apierr.ErrInternal, err)
	}
	s.Status = model.MCPStatusConnected
	s.LastError = ""
	if err := c.DB.WithContext(ctx).Save(s).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}

	mcpMu.Lock()
	if old, ok := mcpClients[id]; ok {
		_ = old.Close()
	}
	mcpClients[id] = client
	mcpMu.Unlock()

	return s, nil
}

// dialMCP 按传输方式建立连接。
func (c *Container) dialMCP(ctx context.Context, s *model.MCPServer) (*mcp.Client, error) {
	if s.Transport == model.MCPTransportHTTP {
		if s.URL == "" {
			return nil, errors.New("HTTP 传输缺少服务地址")
		}
		return mcp.NewClient(ctx, s.Name, mcp.NewHTTPTransport(s.URL, c.mcpHeaderMap(s), 30*time.Second))
	}
	transport, err := mcp.NewStdioTransport(ctx, s.Command, s.ArgList(), c.mcpEnvMap(s))
	if err != nil {
		return nil, err
	}
	return mcp.NewClient(ctx, s.Name, transport)
}

// markMCPError 记录连接失败状态。
func (c *Container) markMCPError(ctx context.Context, s *model.MCPServer, err error) {
	s.Status = model.MCPStatusError
	s.LastError = truncate(err.Error(), 500)
	_ = c.DB.WithContext(ctx).Save(s).Error
}

// DisconnectMCP 断开并移除缓存连接。
func (c *Container) DisconnectMCP(id uint) {
	mcpMu.Lock()
	if client, ok := mcpClients[id]; ok {
		_ = client.Close()
		delete(mcpClients, id)
	}
	mcpMu.Unlock()
}

// mcpClient 获取可用连接，必要时自动重连。
func (c *Container) mcpClient(ctx context.Context, s *model.MCPServer) (*mcp.Client, error) {
	mcpMu.Lock()
	client, ok := mcpClients[s.ID]
	mcpMu.Unlock()
	if ok && client != nil {
		return client, nil
	}
	if _, err := c.ConnectMCP(ctx, s.ID); err != nil {
		return nil, err
	}
	mcpMu.Lock()
	client, ok = mcpClients[s.ID]
	mcpMu.Unlock()
	if !ok || client == nil {
		return nil, mcp.ErrNotConnected
	}
	return client, nil
}

// ExposedTool 对外暴露的工具（含路由信息）。
type ExposedTool struct {
	// Name 对外工具名（已规范化）。
	Name string `json:"name"`
	// Description 描述。
	Description string `json:"description"`
	// Schema 入参 JSON Schema。
	Schema map[string]interface{} `json:"schema"`
	// ServerID 来源 Server。
	ServerID uint `json:"server_id"`
	// ServerName 来源 Server 名。
	ServerName string `json:"server_name"`
	// ToolName MCP 原始工具名。
	ToolName string `json:"tool_name"`
	// AutoExecute 该 Server 是否由网关代执行。
	AutoExecute bool `json:"auto_execute"`
}

// sanitizeToolName 规范化工具名以符合 OpenAI 命名约束。
func sanitizeToolName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	if out == "" {
		out = "tool"
	}
	return out
}

// ListMCPTools 汇总所有启用 Server 的工具，返回可供注入的列表。
func (c *Container) ListMCPTools(ctx context.Context) ([]ExposedTool, error) {
	servers, err := c.ListMCPServers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ExposedTool, 0, 16)
	for i := range servers {
		s := servers[i]
		if !s.Enabled {
			continue
		}
		for _, t := range s.ToolList() {
			exposed := "mcp" + toolNameSep + sanitizeToolName(s.Name) + toolNameSep + sanitizeToolName(t.Name)
			out = append(out, ExposedTool{
				Name:        exposed,
				Description: t.Description,
				Schema:      t.Schema,
				ServerID:    s.ID,
				ServerName:  s.Name,
				ToolName:    t.Name,
				AutoExecute: s.AutoExecute,
			})
		}
	}
	return out, nil
}

// CallMCPTool 按对外工具名路由到具体 Server 执行。
func (c *Container) CallMCPTool(ctx context.Context, exposedName string, args map[string]interface{}) (string, bool, error) {
	tools, err := c.ListMCPTools(ctx)
	if err != nil {
		return "", false, err
	}
	var target *ExposedTool
	for i := range tools {
		if tools[i].Name == exposedName {
			target = &tools[i]
			break
		}
	}
	if target == nil {
		return "", false, apierr.Errorf(apierr.ErrNotFound, "未找到工具 %s", exposedName)
	}

	server, err := c.GetMCPServer(ctx, target.ServerID)
	if err != nil {
		return "", false, err
	}
	client, err := c.mcpClient(ctx, server)
	if err != nil {
		return "", false, err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	result, err := client.CallTool(timeoutCtx, target.ToolName, args)
	if err != nil {
		return "", target.AutoExecute, apierr.Errorf(apierr.ErrUpstream, "工具 %s 调用失败：%s", target.ToolName, err.Error())
	}
	if result.IsError {
		return result.Text(), target.AutoExecute, errors.New(result.Text())
	}
	return result.Text(), target.AutoExecute, nil
}

// TestMCPServer 连通性测试：连接并拉取工具数量。
// SetMCPServerEnabled 启停单个 MCP Server。
//
// 停用的服务不会被工具发现与调用命中——管理员常只想临时摘掉一个出问题的服务，
// 而不是删掉它（删了就得重新填命令与环境变量）。
func (c *Container) SetMCPServerEnabled(ctx context.Context, id uint, enabled bool) (*model.MCPServer, error) {
	s, err := c.GetMCPServer(ctx, id)
	if err != nil {
		return nil, err
	}
	if enabled {
		// 启用时先回到 idle：是否真的连通要等下次连接/测试才知道。
		s.Status = model.MCPStatusIdle
		s.LastError = ""
	} else {
		s.Status = model.MCPStatusDisabled
	}
	// Enabled 是权威开关（连接与工具发现都看它），Status 是运行时状态，
	// 两者必须一起改，否则界面开关与实际是否注入会不一致。
	if err := c.DB.WithContext(ctx).Model(s).
		Updates(map[string]interface{}{
			"enabled":    enabled,
			"status":     s.Status,
			"last_error": s.LastError,
		}).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	s.Enabled = enabled
	// 停用后立刻断开连接，避免残留客户端继续被工具调用命中。
	if !enabled {
		c.DisconnectMCP(id)
	}
	return s, nil
}

func (c *Container) TestMCPServer(ctx context.Context, id uint) (map[string]interface{}, error) {
	start := time.Now()
	s, err := c.ConnectMCP(ctx, id)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"status":     s.Status,
		"tool_count": s.ToolCount,
		"latency_ms": time.Since(start).Milliseconds(),
	}, nil
}

// ShutdownMCP 关闭全部连接（进程退出时调用）。
func ShutdownMCP() {
	mcpMu.Lock()
	defer mcpMu.Unlock()
	for id, client := range mcpClients {
		_ = client.Close()
		delete(mcpClients, id)
	}
}
