package service

import (
	"testing"
	"time"

	"github.com/whalegate/whalegate/internal/config"
	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/cache"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

func newTestContainer() *Container {
	return &Container{Config: &config.Config{
		Gateway: config.GatewayConfig{
			DefaultPromptRatio:       15,
			DefaultCompletionRatio:   60,
			DefaultImageRatio:        200,
			EstimateCompletionTokens: 1024,
		},
	}}
}

// newRatioContainer 构造带倍率表缓存的容器，避免测试穿透到数据库。
func newRatioContainer(t *testing.T, ratios ...model.ModelRatio) *Container {
	t.Helper()
	c := newTestContainer()
	c.KeyCache = cache.NewTTLCache(time.Minute, 128)
	table := make(map[string]model.ModelRatio, len(ratios))
	for _, r := range ratios {
		table[r.Model] = r
	}
	c.KeyCache.Set(ratioCacheKey, table)
	return c
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

// ---------------------------------------------------------------- 生图按张计费

func TestImageRatioFallsBackToDefault(t *testing.T) {
	c := newRatioContainer(t)
	got, err := c.ImageRatioFor(nil, "dall-e-3")
	if err != nil {
		t.Fatalf("取生图单价失败: %v", err)
	}
	if got != 200 {
		t.Fatalf("未配置时应回落到全局默认单价: %v", got)
	}
}

func TestImageRatioModelOverridesWildcard(t *testing.T) {
	c := newRatioContainer(t,
		model.ModelRatio{Model: "*", ImageRatio: 150},
		model.ModelRatio{Model: "dall-e-3", ImageRatio: 350},
	)
	// 通配符先兜底，逐模型配置再覆盖，取值顺序需与 token 倍率一致。
	fallback, err := c.ImageRatioFor(nil, "gpt-image-1")
	if err != nil || fallback != 150 {
		t.Fatalf("通配符合配置优先于全局默认: %v %v", fallback, err)
	}
	custom, err := c.ImageRatioFor(nil, "dall-e-3")
	if err != nil || custom != 350 {
		t.Fatalf("逐模型配置应覆盖通配符: %v %v", custom, err)
	}
}

// 单价 0 表示"未单独配置"而非免费：页面上从兜底价开始编辑，
// 无法区分管理员手填 0 与从未配置，统一按继承处理。
func TestImageRatioZeroMeansInherit(t *testing.T) {
	c := newRatioContainer(t,
		model.ModelRatio{Model: "*", ImageRatio: 300},
		model.ModelRatio{Model: "dall-e-3", ImageRatio: 0},
	)
	got, err := c.ImageRatioFor(nil, "dall-e-3")
	if err != nil {
		t.Fatalf("取生图单价失败: %v", err)
	}
	if got != 300 {
		t.Fatalf("单价为 0 应继承兜底而非免费: %v", got)
	}
}

func TestPointsForImage(t *testing.T) {
	c := newRatioContainer(t, model.ModelRatio{Model: "*", ImageRatio: 200})

	if got, _ := c.PointsForImage(nil, "dall-e-3", 0, 1); got != 0 {
		t.Fatalf("张数为 0 不应计费: %d", got)
	}
	if got, _ := c.PointsForImage(nil, "dall-e-3", 3, 1); got != 600 {
		t.Fatalf("3 张 × 200 点应为 600: %d", got)
	}
}

// 分组倍率在最终点数上相乘：避免分组把 per-model 单价整条表重复一遍。
func TestPointsForImageAppliesGroupMultiplier(t *testing.T) {
	c := newRatioContainer(t, model.ModelRatio{Model: "*", ImageRatio: 150})

	got, err := c.PointsForImage(nil, "dall-e-3", 2, 1.5)
	if err != nil || got != 450 {
		t.Fatalf("分组系数应在点数上相乘: %d %v", got, err)
	}
	// 系数必须先乘后取整：0.75 × 3 张 × 150 = 337.5 → 338，而不是先取整再乘。
	got, _ = c.PointsForImage(nil, "dall-e-3", 3, 0.75)
	if got != 338 {
		t.Fatalf("四舍五入只应发生一次: %d", got)
	}
	if got, _ = c.PointsForImage(nil, "dall-e-3", 2, 0); got != 300 {
		t.Fatalf("非法系数（0）应按 1 处理: %d", got)
	}
}

func TestImageBillingFreeWhenDefaultRatioZero(t *testing.T) {
	c := newRatioContainer(t)
	c.Config.Gateway.DefaultImageRatio = 0
	got, err := c.PointsForImage(nil, "dall-e-3", 4, 1)
	if err != nil {
		t.Fatalf("取点数失败: %v", err)
	}
	if got != 0 {
		t.Fatalf("兜底单价为 0 时应免费: %d", got)
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
