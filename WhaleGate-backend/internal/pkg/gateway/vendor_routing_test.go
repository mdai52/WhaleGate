package gateway

import (
	"testing"

	"github.com/whalegate/whalegate/internal/pkg/gateway/vendor"
)

// 支持 OpenAI 协议的厂商共用 openai 协议，端点与鉴权差异由档案表补齐。
// 这些用例锁住的是「同一份协议 + 不同厂商 → 不同 URL / 请求头」这一层契约。
func TestBuildUpstreamURLPerVendor(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		model   string
		want    string
	}{
		{
			name:    "Ollama 自动补 /v1",
			baseURL: "http://localhost:11434",
			model:   "qwen3:8b",
			want:    "http://localhost:11434/v1/chat/completions",
		},
		{
			name:    "Azure 部署名进路径并带 api-version",
			baseURL: "https://my-resource.openai.azure.com",
			model:   "gpt-4o",
			want:    "https://my-resource.openai.azure.com/openai/deployments/gpt-4o/chat/completions?api-version=" + vendor.AzureAPIVersion,
		},
		{
			name:    "百炼填根地址时补兼容前缀",
			baseURL: "https://dashscope.aliyuncs.com",
			model:   "qwen-plus",
			want:    "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions",
		},
		{
			name:    "百炼已带前缀时不再重复拼接",
			baseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
			model:   "qwen-plus",
			want:    "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions",
		},
		{
			name:    "千帆 v2 走标准路径",
			baseURL: "https://qianfan.baidubce.com/v2",
			model:   "ernie-4.0",
			want:    "https://qianfan.baidubce.com/v2/chat/completions",
		},
		{
			name:    "未识别厂商回落默认路径",
			baseURL: "https://unknown-vendor.example.com/v1",
			model:   "m",
			want:    "https://unknown-vendor.example.com/v1/chat/completions",
		},
	}
	for _, c := range cases {
		got := BuildUpstreamURL(ProtocolOpenAI, c.baseURL, c.model, false, vendor.FromBaseURL(c.baseURL))
		if got != c.want {
			t.Errorf("%s:\n got=%s\nwant=%s", c.name, got, c.want)
		}
	}
}

// 鉴权方式与流式专用头同样由档案驱动。
func TestBuildUpstreamHeadersPerVendor(t *testing.T) {
	azure := vendor.FromBaseURL("https://my-resource.openai.azure.com")
	if h := BuildUpstreamHeaders(ProtocolOpenAI, "key", azure); h["api-key"] != "key" {
		t.Fatalf("Azure 应用 api-key 且不带 Bearer，实际 %v", h)
	}

	ds := vendor.FromBaseURL("https://dashscope.aliyuncs.com")
	if h := BuildUpstreamHeadersFor(ProtocolOpenAI, "key", ds, false); h["X-DashScope-SSE"] != "" {
		t.Fatalf("非流式不应下发 SSE 头，实际 %v", h)
	}
	if h := BuildUpstreamHeadersFor(ProtocolOpenAI, "key", ds, true); h["X-DashScope-SSE"] != "enable" {
		t.Fatalf("流式应下发 SSE 头，实际 %v", h)
	}

	generic := vendor.FromBaseURL("https://unknown-vendor.example.com/v1")
	if h := BuildUpstreamHeaders(ProtocolOpenAI, "key", generic); h["Authorization"] != "Bearer key" {
		t.Fatalf("通用厂商应为 Bearer，实际 %v", h)
	}
}

// 千帆老版端点把正文放在 result、错误放在 error_code/error_msg，
// 新版是标准 choices[]：同一个渠道两种都要能解析。
func TestParseUpstreamResponseLegacyQianFan(t *testing.T) {
	v := vendor.FromBaseURL("https://qianfan.baidubce.com/v2")

	legacy := []byte(`{"id":"as-1","object":"chat.completion","result":"老版回复","is_truncated":false,
		"usage":{"prompt_tokens":2,"completion_tokens":4,"total_tokens":6}}`)
	resp, err := ParseUpstreamResponse(ProtocolOpenAI, legacy, v)
	if err != nil {
		t.Fatalf("老版响应解析失败: %v", err)
	}
	if resp.Choices[0].Message.Content != "老版回复" {
		t.Fatalf("老版正文解析错误: %+v", resp.Choices[0].Message)
	}

	modern := []byte(`{"id":"as-2","choices":[{"index":0,"message":{"role":"assistant","content":"新版回复"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	resp, err = ParseUpstreamResponse(ProtocolOpenAI, modern, v)
	if err != nil {
		t.Fatalf("新版响应解析失败: %v", err)
	}
	if resp.Choices[0].Message.Content != "新版回复" {
		t.Fatalf("新版正文解析错误: %+v", resp.Choices[0].Message)
	}

	// 千帆的错误外壳是 error_code/error_msg，按 OpenAI 解析会得到空文案。
	if got := ExtractUpstreamError(ProtocolOpenAI, []byte(`{"error_code":17,"error_msg":"配额不足"}`), v); got == "" {
		t.Fatal("千帆错误外壳未解析出文案")
	}
}

// 模型列表端点同样按厂商补齐：Azure 要 api-version，Ollama 在 /v1 下。
func TestUpstreamModelsURLPerVendor(t *testing.T) {
	got := UpstreamModelsURL(ProtocolOpenAI, "https://my-resource.openai.azure.com", vendor.FromBaseURL("https://my-resource.openai.azure.com"))
	if want := "https://my-resource.openai.azure.com/openai/models?api-version=" + vendor.AzureAPIVersion; got != want {
		t.Fatalf("Azure 模型端点错误:\n got=%s\nwant=%s", got, want)
	}
	got = UpstreamModelsURL(ProtocolOpenAI, "http://localhost:11434", vendor.FromBaseURL("http://localhost:11434"))
	if want := "http://localhost:11434/v1/models"; got != want {
		t.Fatalf("Ollama 模型端点错误:\n got=%s\nwant=%s", got, want)
	}
}

// 探测顺序应把厂商特有端点排在最前，省掉必然失败的试探。
func TestVendorProbeSpec(t *testing.T) {
	base := "https://my-resource.openai.azure.com"
	spec := vendorProbeSpec(base, vendor.FromBaseURL(base))
	if spec == nil {
		t.Fatal("Azure 应生成厂商探测规则")
	}
	if len(spec.Paths) != 1 || spec.Paths[0] != "/openai/models?api-version="+vendor.AzureAPIVersion {
		t.Fatalf("探测路径错误: %v", spec.Paths)
	}
	if spec.Type != ProtocolOpenAI {
		t.Fatalf("厂商探测仍应归一为 openai，实际 %s", spec.Type)
	}
	if vendorProbeSpec("https://unknown-vendor.example.com", vendor.FromBaseURL("https://unknown-vendor.example.com")) != nil {
		t.Fatal("无自定义端点的厂商不应生成额外探测规则")
	}
}
