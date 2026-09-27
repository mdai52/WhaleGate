package service

import (
	"testing"

	"github.com/whalegate/whalegate/internal/config"
	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

func newTestContainer() *Container {
	return &Container{Config: &config.Config{
		Gateway: config.GatewayConfig{
			DefaultPromptRatio:       15,
			DefaultCompletionRatio:   60,
			EstimateCompletionTokens: 1024,
		},
	}}
}

func TestEstimateUsageText(t *testing.T) {
	c := newTestContainer()
	req := &spec.UnifiedRequest{
		Messages: []spec.Message{{Role: spec.RoleUser, Content: "hello world, this is a test"}},
	}
	u := c.EstimateUsage(req)
	if u.PromptTokens <= 0 {
		t.Fatalf("应估算出正数 prompt token: %+v", u)
	}
	if u.CompletionTokens != 1024 {
		t.Fatalf("未设置 max_tokens 时应使用配置默认值: %d", u.CompletionTokens)
	}
	if u.TotalTokens != u.PromptTokens+u.CompletionTokens {
		t.Fatalf("total 推导错误: %+v", u)
	}
}

func TestEstimateUsageRespectsMaxTokens(t *testing.T) {
	c := newTestContainer()
	max := 128
	u := c.EstimateUsage(&spec.UnifiedRequest{
		Messages:  []spec.Message{{Role: spec.RoleUser, Content: "hi"}},
		MaxTokens: &max,
	})
	if u.CompletionTokens != 128 {
		t.Fatalf("应取 max_tokens 与默认值的较小者: %d", u.CompletionTokens)
	}
}

func TestEstimateUsageEmpty(t *testing.T) {
	c := newTestContainer()
	if u := c.EstimateUsage(nil); u.PromptTokens != 0 {
		t.Fatalf("nil 请求应返回空用量: %+v", u)
	}
	u := c.EstimateUsage(&spec.UnifiedRequest{})
	if u.PromptTokens != 1 {
		t.Fatalf("空消息至少按 1 token 估算: %d", u.PromptTokens)
	}
}

func TestUsageNormalize(t *testing.T) {
	u := &spec.Usage{PromptTokens: 10, CompletionTokens: 5}
	u.Normalize()
	if u.TotalTokens != 15 {
		t.Fatalf("total 推导错误: %d", u.TotalTokens)
	}

	// 上游只报思维链时，completion 不应小于 reasoning。
	u = &spec.Usage{PromptTokens: 3, ReasoningTokens: 20}
	u.Normalize()
	if u.CompletionTokens != 20 || u.TotalTokens != 23 {
		t.Fatalf("思维链归一错误: %+v", u)
	}
}

func TestUsageAddIncludesReasoning(t *testing.T) {
	total := &spec.Usage{}
	total.Add(&spec.Usage{PromptTokens: 1, CompletionTokens: 2, ReasoningTokens: 1, TotalTokens: 3})
	total.Add(&spec.Usage{PromptTokens: 2, CompletionTokens: 3, ReasoningTokens: 2, TotalTokens: 5})
	if total.ReasoningTokens != 3 || total.TotalTokens != 8 {
		t.Fatalf("累加错误: %+v", total)
	}
}

// ---------------------------------------------------------------- 别名与注入

func TestChannelAliasResolution(t *testing.T) {
	ch := &model.Channel{ModelAlias: `{
      "image-1k": {"model": "gemini-2.0-flash", "override": {"generationConfig": {"imageSize": "1K"}}},
      "image-2k": {"model": "gemini-2.0-flash", "override": {"generationConfig": {"imageSize": "2K"}}}
    }`}

	upstream, override := ch.ResolveModel("image-2k")
	if upstream != "gemini-2.0-flash" {
		t.Fatalf("别名改写错误: %s", upstream)
	}
	cfg, ok := override["generationConfig"].(map[string]interface{})
	if !ok || cfg["imageSize"] != "2K" {
		t.Fatalf("别名专属注入错误: %+v", override)
	}

	// 未命中别名时退化为原名。
	if name, _ := ch.ResolveModel("other-model"); name != "other-model" {
		t.Fatalf("未命中别名应原样返回: %s", name)
	}
}

func TestChannelAliasFallbackToSelf(t *testing.T) {
	ch := &model.Channel{ModelAlias: `{"standalone": {}}`}
	upstream, override := ch.ResolveModel("standalone")
	if upstream != "standalone" {
		t.Fatalf("未指定 model 时应沿用别名自身: %s", upstream)
	}
	if len(override) != 0 {
		t.Fatalf("无注入时不应产生 override: %+v", override)
	}
}

func TestChannelOverrideMergeOrder(t *testing.T) {
	ch := &model.Channel{
		RequestOverride: `{"tools": [{"type": "image_generation"}], "temperature": 0.2}`,
		ModelAlias:      `{"img": {"model": "base", "override": {"temperature": 0.9}}}`,
	}
	_, override := ch.ResolveModel("img")
	if override["temperature"] != 0.9 {
		t.Fatalf("别名注入应覆盖渠道级注入: %+v", override)
	}
	tools, ok := override["tools"].([]interface{})
	if !ok || len(tools) != 1 {
		t.Fatalf("渠道级 tools 注入丢失: %+v", override)
	}
}

func TestChannelSupportsAliasModel(t *testing.T) {
	ch := &model.Channel{Models: `["base-model"]`, ModelAlias: `{"img-model": {"model": "base-model"}}`}
	if !ch.SupportsModel("img-model") {
		t.Fatal("别名模型名应可用于路由")
	}
	if !ch.SupportsModel("base-model") {
		t.Fatal("登记模型应可用于路由")
	}
	if ch.SupportsModel("unknown") {
		t.Fatal("未登记模型不应匹配")
	}
}

func TestChannelExposedModels(t *testing.T) {
	ch := &model.Channel{Models: `["base"]`, ModelAlias: `{"a1": {"model": "base"}, "a2": {}}`}
	exposed := ch.ExposedModels()
	if len(exposed) != 3 {
		t.Fatalf("对外模型名应包含别名: %v", exposed)
	}
	seen := map[string]bool{}
	for _, m := range exposed {
		seen[m] = true
	}
	if !seen["a1"] || !seen["a2"] || !seen["base"] {
		t.Fatalf("模型名缺失: %v", exposed)
	}
}

func TestChannelInvalidAliasJSON(t *testing.T) {
	ch := &model.Channel{ModelAlias: `{bad json`}
	if len(ch.AliasMap()) != 0 {
		t.Fatal("非法 JSON 应返回空映射")
	}
	if name, _ := ch.ResolveModel("x"); name != "x" {
		t.Fatalf("非法别名不应影响解析: %s", name)
	}
}
