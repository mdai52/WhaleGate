package mcp

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// mockServerPath 返回内置 mock MCP Server 路径。
func mockServerPath(t *testing.T) string {
	t.Helper()
	path := "testdata/mock_server.py"
	if _, err := os.Stat(path); err != nil {
		t.Skip("缺少 testdata/mock_server.py")
	}
	return path
}

func pythonCmd(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Skip("未找到 python 运行环境")
	return ""
}

// TestStdioClient 用真实的子进程完成初始化、拉取工具与调用工具的完整握手。
func TestStdioClient(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	transport, err := NewStdioTransport(ctx, pythonCmd(t), []string{mockServerPath(t)}, nil)
	if err != nil {
		t.Fatalf("启动传输失败: %v", err)
	}

	client, err := NewClient(ctx, "mock", transport)
	if err != nil {
		t.Fatalf("初始化握手失败: %v", err)
	}
	defer func() { _ = client.Close() }()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("拉取工具失败: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("期望 2 个工具，实际 %d", len(tools))
	}
	if tools[0].Name != "get_weather" || tools[1].Name != "add" {
		t.Errorf("工具顺序或名称异常: %+v", tools)
	}
	if len(client.CachedTools()) != 2 {
		t.Error("工具缓存未写入")
	}

	result, err := client.CallTool(ctx, "add", map[string]interface{}{"a": 3, "b": 4})
	if err != nil {
		t.Fatalf("调用工具失败: %v", err)
	}
	if got := result.Text(); got != "7.0" {
		t.Errorf("add(3,4) 应为 7.0，实际 %q", got)
	}

	weather, err := client.CallTool(ctx, "get_weather", map[string]interface{}{"city": "深圳"})
	if err != nil {
		t.Fatalf("调用天气工具失败: %v", err)
	}
	if weather.Text() == "" || !contains(weather.Text(), "深圳") {
		t.Errorf("天气结果异常: %q", weather.Text())
	}

	if err := client.Ping(ctx); err != nil {
		t.Errorf("探活失败: %v", err)
	}
}

// TestCallResultText 拼接多个文本块。
func TestCallResultText(t *testing.T) {
	r := &CallResult{Content: []ContentBlock{
		{Type: "text", Text: "第一行"},
		{Type: "text", Text: "第二行"},
	}}
	if got := r.Text(); got != "第一行\n第二行" {
		t.Errorf("拼接异常: %q", got)
	}
	empty := &CallResult{IsError: true}
	if got := empty.Text(); got != "工具调用失败" {
		t.Errorf("错误无文本时应返回兜底: %q", got)
	}
	var nilResult *CallResult
	if nilResult.Text() != "" {
		t.Error("nil 结果应返回空串")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
