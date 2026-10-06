package handler

import (
	"testing"

	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

func TestEstimateEmbeddingTokens(t *testing.T) {
	// 字符串按约 4 字节 ≈ 1 token 折算。
	if got := estimateEmbeddingTokens("abcdefgh"); got != 2 {
		t.Fatalf("字符串估算错误: %d", got)
	}
	// 数组输入按元素累加。
	if got := estimateEmbeddingTokens([]interface{}{"abcdefgh", "ijklmnop"}); got != 4 {
		t.Fatalf("数组估算错误: %d", got)
	}
	// token 数组形式的输入按元素数计。
	if got := estimateEmbeddingTokens([]interface{}{[]interface{}{1, 2, 3, 4, 5, 6, 7, 8}}); got != 8 {
		t.Fatalf("token 数组估算错误: %d", got)
	}
	// 空输入至少按 1 计，避免预估点数为 0 导致预扣形同虚设。
	if got := estimateEmbeddingTokens(""); got != 1 {
		t.Fatalf("空输入应至少计 1: %d", got)
	}
}

func TestParseEmbeddingUsage(t *testing.T) {
	usage := parseEmbeddingUsage([]byte(
		`{"object":"list","data":[{"embedding":[0.1]}],"usage":{"prompt_tokens":5,"total_tokens":5}}`))
	if usage.PromptTokens != 5 || usage.TotalTokens != 5 {
		t.Fatalf("用量解析错误: %+v", usage)
	}
	// 上游未返回 usage 时返回零值，调用方按估算口径结算。
	empty := parseEmbeddingUsage([]byte(`{"object":"list"}`))
	if empty.PromptTokens != 0 || empty.TotalTokens != 0 {
		t.Fatalf("缺失 usage 应返回零值: %+v", empty)
	}
	if empty == nil {
		t.Fatal("不应返回 nil，避免调用方判空")
	}
}

// 透传开关解析与配置默认值的联动在 gateway 包已覆盖，
// 这里验证默认能力集在两种模式下的行为分叉点。
func TestApplyParamsModeSplit(t *testing.T) {
	payload := map[string]interface{}{}
	params := map[string]interface{}{"logit_bias": map[string]interface{}{"1": 2}}
	gateway.ApplyParams(payload, gateway.ProtocolOpenAI, params, nil, false)
	if _, ok := payload["logit_bias"]; ok {
		t.Fatal("非透传模式应丢弃能力表外参数")
	}

	payload = map[string]interface{}{}
	gateway.ApplyParams(payload, gateway.ProtocolOpenAI, params, nil, true)
	if payload["logit_bias"] == nil {
		t.Fatal("透传模式应转发能力表外参数")
	}

	// 预览/转发共用同一份报告结构，确保 usage 归一入口存在。
	usage := &spec.Usage{PromptTokens: 1, TotalTokens: 1}
	usage.Normalize()
	if usage.TotalTokens != 1 {
		t.Fatalf("用量归一错误: %+v", usage)
	}
}
