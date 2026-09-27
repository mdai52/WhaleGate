package model

import (
	"encoding/json"
	"time"
)

// MCP 传输方式。
const (
	MCPTransportStdio = "stdio"
	MCPTransportHTTP  = "http"
)

// MCP Server 连接状态。
const (
	MCPStatusIdle      = "idle"
	MCPStatusConnected = "connected"
	MCPStatusError     = "error"
	MCPStatusDisabled  = "disabled"
)

// MCPTool 一个 MCP 工具声明。
type MCPTool struct {
	// Name 工具名（原始名，可能带 server 前缀）。
	Name string `json:"name"`
	// Description 工具描述。
	Description string `json:"description,omitempty"`
	// Schema JSON Schema 形式的入参声明。
	Schema map[string]interface{} `json:"input_schema,omitempty"`
	// Server 来源 Server 名。
	Server string `json:"server,omitempty"`
}

// MCPServer MCP Server 配置。
type MCPServer struct {
	ID        uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string `gorm:"size:128;not null;uniqueIndex" json:"name"`
	Transport string `gorm:"size:16;not null;default:'stdio'" json:"transport"`
	// Command stdio 传输的可执行文件。
	Command string `gorm:"size:512;not null;default:''" json:"command"`
	// Args stdio 参数（JSON 数组文本）。
	Args string `gorm:"type:text;not null;default:'[]'" json:"args"`
	// Env stdio 环境变量（JSON 对象文本）。
	Env string `gorm:"type:text;not null;default:'{}'" json:"env"`
	// URL HTTP 传输端点。
	URL string `gorm:"size:512;not null;default:''" json:"url"`
	// Headers HTTP 附加请求头（JSON 对象文本）。
	Headers string `gorm:"type:text;not null;default:'{}'" json:"headers"`
	Enabled bool   `gorm:"not null;default:true" json:"enabled"`
	// AutoExecute 为 true 时由网关代执行多轮工具循环。
	AutoExecute bool   `gorm:"not null;default:false" json:"auto_execute"`
	Status      string `gorm:"size:16;not null;default:'idle'" json:"status"`
	LastError   string `gorm:"type:text;not null;default:''" json:"last_error"`
	// Tools 工具缓存（JSON 数组文本）。
	Tools     string    `gorm:"type:text;not null;default:'[]'" json:"tools"`
	ToolCount int       `gorm:"not null;default:0" json:"tool_count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TableName 表名。
func (MCPServer) TableName() string { return "mcp_servers" }

// ToolList 解析工具缓存。
func (s *MCPServer) ToolList() []MCPTool {
	if s == nil || s.Tools == "" {
		return nil
	}
	var out []MCPTool
	if err := json.Unmarshal([]byte(s.Tools), &out); err != nil {
		return nil
	}
	return out
}

// SetToolList 序列化工具缓存。
func (s *MCPServer) SetToolList(list []MCPTool) error {
	if len(list) == 0 {
		s.Tools = "[]"
		s.ToolCount = 0
		return nil
	}
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	s.Tools = string(data)
	s.ToolCount = len(list)
	return nil
}

// ArgList 解析 stdio 参数。
func (s *MCPServer) ArgList() []string {
	if s == nil || s.Args == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s.Args), &out); err != nil {
		return nil
	}
	return out
}

// EnvMap 解析 stdio 环境变量。
func (s *MCPServer) EnvMap() map[string]string {
	if s == nil || s.Env == "" {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(s.Env), &out); err != nil {
		return nil
	}
	return out
}

// HeaderMap 解析 HTTP 请求头。
func (s *MCPServer) HeaderMap() map[string]string {
	if s == nil || s.Headers == "" {
		return nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(s.Headers), &out); err != nil {
		return nil
	}
	return out
}
