package service

import (
	"context"
	"sort"
	"strings"

	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
	"github.com/whalegate/whalegate/internal/pkg/gateway/vendor"
	"github.com/whalegate/whalegate/internal/pkg/util"
)

// ---------------------------------------------------------------- 可选模型

// PlaygroundModel 调试页可选的模型。
type PlaygroundModel struct {
	// Model 对外模型名。
	Model string `json:"model"`
	// Channel 承载该模型的渠道名。
	Channel string `json:"channel"`
	// ChannelType 渠道协议类型，决定参数落到上游请求体的哪个位置。
	ChannelType string `json:"channel_type"`
	// UpstreamModel 该模型实际发往上游的名字，与对外名不同时前端可提示。
	UpstreamModel string `json:"upstream_model"`
}

// ListPlaygroundModels 汇总调试页可选的模型：启用渠道对外暴露的全部模型名。
//
// 与 /v1/models 的口径一致，只是通过登录态校验，便于用户在网页上先试参数
// 再去写代码，而不必先创建 API Key。
func (c *Container) ListPlaygroundModels(ctx context.Context) ([]PlaygroundModel, error) {
	channels, err := c.EnabledChannels(ctx)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	out := make([]PlaygroundModel, 0, 32)
	for _, ch := range channels {
		for _, name := range ch.ExposedModels() {
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			upstream, _ := ch.ResolveModel(name)
			out = append(out, PlaygroundModel{
				Model:         name,
				Channel:       ch.Name,
				ChannelType:   ch.Type,
				UpstreamModel: upstream,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out, nil
}

// ---------------------------------------------------------------- 参数预览

// ParamPreviewInput 参数预览入参。
type ParamPreviewInput struct {
	// Model 对外模型名。
	Model string `json:"model"`
	// Prompt 可选的提示词，仅用于让预览出的请求体更接近真实调用。
	Prompt string `json:"prompt"`
	// Params 待验证的自定义参数，键为参数名（支持 temperature / topK 等写法）。
	Params map[string]interface{} `json:"params"`
}

// ParamPreview 参数分配预览结果。
//
// 该结果完全来自真实链路使用的 BuildUpstreamRequest + ApplyParams，
// 因此「预览会丢弃」与「实际调用被丢弃」必然一致。
type ParamPreview struct {
	// Model 对外模型名。
	Model string `json:"model"`
	// UpstreamModel 改写后实际发往上游的模型名。
	UpstreamModel string `json:"upstream_model"`
	// Channel 选中的渠道。
	Channel struct {
		ID   uint   `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"channel"`
	// Declared 渠道是否显式声明了参数能力；false 表示按协议默认集判定。
	Declared bool `json:"declared"`
	// Supported 该渠道最终生效的支持参数集（统一名，升序）。
	Supported []string `json:"supported"`
	// Excluded 渠道明确排除的参数。
	Excluded []string `json:"excluded"`
	// Defaults 渠道配置的默认值，仅在用户未传该参数时生效。
	Defaults map[string]interface{} `json:"defaults,omitempty"`
	// Applied 会被写入上游请求的参数。
	Applied []string `json:"applied"`
	// Dropped 会被丢弃的参数（上游不支持或协议无对应字段）。
	Dropped []string `json:"dropped"`
	// Unknown 既不是标准参数、也没有在能力表中出现的参数，仅作提示。
	Unknown []string `json:"unknown"`
	// Injected 渠道级与别名级的强制注入，优先级最高。
	Injected map[string]interface{} `json:"injected,omitempty"`
	// Payload 最终发往上游的请求体。
	Payload map[string]interface{} `json:"payload"`
}

// standardParamNames 当前网关已标准化的参数名集合，用于区分"自定义参数"。
func standardParamNames() map[string]struct{} {
	names := []string{
		gateway.ParamTemperature, gateway.ParamTopP, gateway.ParamTopK,
		gateway.ParamMaxTokens, gateway.ParamStop, gateway.ParamFrequencyPenalty,
		gateway.ParamPresencePenalty, gateway.ParamRepetitionPenalty, gateway.ParamSeed,
		gateway.ParamResponseFormat, gateway.ParamN, gateway.ParamMinP, gateway.ParamModalities,
	}
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}

// PreviewParams 预览一次调用的参数分配结果，不向上游发起请求、不计费。
//
// 用途是让用户在写代码前就能确认「我传的参数哪些生效、哪些会被丢弃、最终写成了什么样」，
// 避免反复试错；分配逻辑与真实转发共用 ApplyParams，结论可靠。
func (c *Container) PreviewParams(ctx context.Context, in ParamPreviewInput) (*ParamPreview, error) {
	in.Model = strings.TrimSpace(in.Model)
	if in.Model == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "请先选择模型")
	}

	channel, err := c.SelectChannel(ctx, in.Model, nil)
	if err != nil {
		return nil, err
	}
	upstreamModel, override := channel.ResolveModel(in.Model)

	prompt := strings.TrimSpace(in.Prompt)
	if prompt == "" {
		// 占位内容只为让请求体结构完整，不会被发出去。
		prompt = "（参数预览占位内容）"
	}

	// 构造与真实转发同构的请求，走完全相同的构建与分配链路。
	unified := &spec.UnifiedRequest{
		Model:    upstreamModel,
		Messages: []spec.Message{{Role: "user", Content: prompt}},
		Extra:    in.Params,
	}
	payload, err := gateway.BuildUpstreamRequest(channel.Type, unified, vendor.FromBaseURL(channel.BaseURL))
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}

	schema := channel.Schema()
	passthrough := gateway.ResolvePassthrough(schema, c.ParamPassthrough())
	report := gateway.ApplyParams(payload, channel.Type, gateway.CollectParams(unified), schema, passthrough)
	if len(override) > 0 {
		payload = util.MergeMap(payload, override)
	}

	out := &ParamPreview{
		Model:         in.Model,
		UpstreamModel: upstreamModel,
		Declared:      schema != nil && len(schema.Supported) > 0,
		Supported:     sortedParamSet(gateway.SupportedParams(channel.Type, schema)),
		Applied:       report.Applied,
		Dropped:       report.Dropped,
		// 集合类字段一律初始化为空切片：nil 会被序列化成 null，
		// 前端对 null 取 length 会抛错，导致整块结果渲染不出来。
		Excluded: []string{},
		Unknown:  []string{},
		Injected: override,
		Payload:  payload,
	}
	out.Channel.ID = channel.ID
	out.Channel.Name = channel.Name
	out.Channel.Type = channel.Type
	if schema != nil {
		for _, name := range schema.Exclude {
			out.Excluded = append(out.Excluded, gateway.NormalizeParamName(name))
		}
		sort.Strings(out.Excluded)
		if len(schema.Defaults) > 0 {
			defaults := make(map[string]interface{}, len(schema.Defaults))
			for k, v := range schema.Defaults {
				defaults[gateway.NormalizeParamName(k)] = v
			}
			out.Defaults = defaults
		}
	}
	out.Unknown = unknownParams(in.Params)
	return out, nil
}

// unknownParams 挑出既非标准参数、也无法归一到标准名的自定义参数。
// 这类参数需要提示用户自行确认上游是否认识，或是否拼写有误。
//
// 始终返回非 nil 切片：空结果序列化为 []，避免前端拿到 null。
func unknownParams(params map[string]interface{}) []string {
	out := []string{}
	if len(params) == 0 {
		return out
	}
	standard := standardParamNames()
	for name := range params {
		if _, ok := standard[gateway.NormalizeParamName(name)]; ok {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// sortedParamSet 把参数集合转为升序切片。
func sortedParamSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
