// Package mcp 实现 MCP（Model Context Protocol）客户端：
// 建立与 MCP Server 的连接、拉取工具列表、调用工具。
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// 协议常量。
const (
	ProtocolVersion  = "2024-11-05"
	MethodInitialize = "initialize"
	MethodToolsList  = "tools/list"
	MethodToolsCall  = "tools/call"
)

// ErrNotConnected 未建立连接。
var ErrNotConnected = errors.New("MCP 服务未连接")

// Tool 工具声明。
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// CallResult 工具调用结果。
type CallResult struct {
	// Content 文本片段列表。
	Content []ContentBlock `json:"content,omitempty"`
	// IsError 工具是否报告业务错误。
	IsError bool `json:"isError,omitempty"`
}

// ContentBlock 工具返回的内容块。
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Text 拼接所有文本块，便于回喂模型。
func (r *CallResult) Text() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range r.Content {
		if c.Text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(c.Text)
	}
	if r.IsError && b.Len() == 0 {
		return "工具调用失败"
	}
	return b.String()
}

// rpcRequest JSON-RPC 请求。
type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// rpcResponse JSON-RPC 响应。
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("MCP 错误 %d: %s", e.Code, e.Message)
}

// Transport 与 MCP Server 的通信通道。
type Transport interface {
	// RoundTrip 发送一条 JSON-RPC 请求并返回响应。
	RoundTrip(ctx context.Context, req rpcRequest) (*rpcResponse, error)
	// Notify 发送无需响应的通知（只写不读，避免阻塞）。
	Notify(ctx context.Context, req rpcRequest) error
	// Close 释放资源。
	Close() error
}

// ---------------------------------------------------------------- stdio

// StdioTransport 通过子进程 stdin/stdout 通信（换行分隔的 JSON-RPC）。
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	mu     sync.Mutex
	stderr strings.Builder
}

// NewStdioTransport 启动子进程并完成初始化握手。
func NewStdioTransport(ctx context.Context, command string, args []string, env map[string]string) (*StdioTransport, error) {
	if strings.TrimSpace(command) == "" {
		return nil, errors.New("stdio 传输缺少命令")
	}
	cmd := exec.Command(command, args...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), mapToEnv(env)...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	t := &StdioTransport{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout)}
	cmd.Stderr = &t.stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return t, nil
}

func mapToEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}

// RoundTrip 写入一行请求并读取一行响应。
func (t *StdioTransport) RoundTrip(_ context.Context, req rpcRequest) (*rpcResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := t.stdin.Write(append(data, '\n')); err != nil {
		return nil, err
	}

	// 逐行读取，跳过非 JSON 噪声（部分 server 会打印日志到 stdout）
	for {
		line, err := t.stdout.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("MCP 进程已退出: %s", strings.TrimSpace(t.stderr.String()))
			}
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		return &resp, nil
	}
}

// Notify 写入一行通知，不等待响应。
func (t *StdioTransport) Notify(_ context.Context, req rpcRequest) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = t.stdin.Write(append(data, '\n'))
	return err
}

// Close 关闭子进程。
func (t *StdioTransport) Close() error {
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}
	return nil
}

// ---------------------------------------------------------------- HTTP

// HTTPTransport 通过 HTTP POST 通信（Streamable HTTP 的简化形态：
// 直接 POST JSON-RPC，响应为 JSON 行或 SSE 的 data: 行）。
type HTTPTransport struct {
	url     string
	headers map[string]string
	client  *http.Client
}

// NewHTTPTransport 创建 HTTP 传输。
func NewHTTPTransport(url string, headers map[string]string, timeout time.Duration) *HTTPTransport {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &HTTPTransport{url: url, headers: headers, client: &http.Client{Timeout: timeout}}
}

// RoundTrip 发送请求并解析响应。
func (t *HTTPTransport) RoundTrip(ctx context.Context, req rpcRequest) (*rpcResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("MCP 服务返回 HTTP %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	// 兼容 SSE：取最后一个 data: 行
	payload := string(body)
	if strings.Contains(payload, "data:") {
		lines := strings.Split(payload, "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i]), "data:"))
			if strings.HasPrefix(line, "{") {
				payload = line
				break
			}
		}
	}
	var out rpcResponse
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		return nil, fmt.Errorf("MCP 响应解析失败: %w", err)
	}
	return &out, nil
}

// Notify 发送通知并忽略响应体。
func (t *HTTPTransport) Notify(ctx context.Context, req rpcRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}
	resp, err := t.client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return nil
}

// Close 无状态，空实现。
func (t *HTTPTransport) Close() error { return nil }

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// ---------------------------------------------------------------- Client

// Client 一个 MCP Server 连接。
type Client struct {
	name      string
	transport Transport
	mu        sync.Mutex
	nextID    int64
	tools     []Tool
	closed    bool
}

// NewClient 创建并初始化连接，返回可用客户端。
func NewClient(ctx context.Context, name string, transport Transport) (*Client, error) {
	c := &Client{name: name, transport: transport, nextID: 1}
	if err := c.initialize(ctx); err != nil {
		_ = transport.Close()
		return nil, err
	}
	return c, nil
}

// Name 返回 Server 名。
func (c *Client) Name() string { return c.name }

func (c *Client) initialize(ctx context.Context) error {
	params := map[string]interface{}{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "whalegate", "version": "1.0.0"},
	}
	if _, err := c.call(ctx, MethodInitialize, params); err != nil {
		return err
	}
	// initialized 通知：无需等待响应
	_ = c.notify("notifications/initialized", map[string]interface{}{})
	return nil
}

// ListTools 拉取工具列表。
func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
	resp, err := c.call(ctx, MethodToolsList, map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	var result struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.tools = result.Tools
	c.mu.Unlock()
	return result.Tools, nil
}

// CachedTools 返回已缓存的工具列表。
func (c *Client) CachedTools() []Tool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tools
}

// CallTool 调用工具并归一化参数（OpenAI 用 arguments 字符串，MCP 用对象）。
func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (*CallResult, error) {
	if args == nil {
		args = map[string]interface{}{}
	}
	resp, err := c.call(ctx, MethodToolsCall, map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return nil, err
	}
	var result CallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Ping 探活。
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.call(ctx, "ping", map[string]interface{}{})
	if err != nil {
		// 部分 server 不实现 ping，退化为 tools/list
		_, err = c.ListTools(ctx)
	}
	return err
}

// Close 关闭连接。
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.transport.Close()
}

// call 执行一次带 ID 的请求。
func (c *Client) call(ctx context.Context, method string, params interface{}) (*rpcResponse, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	id := c.nextID
	c.nextID++
	c.mu.Unlock()

	req := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	resp, err := c.transport.RoundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp, nil
}

// notify 发送无需响应的通知（不等待回包）。
func (c *Client) notify(method string, params interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := rpcRequest{JSONRPC: "2.0", ID: 0, Method: method, Params: params}
	return c.transport.Notify(ctx, req)
}
