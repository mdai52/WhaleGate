// Package gateway 中的参数子系统：把用户自定义参数归一到统一命名，
// 再按"上游是否支持"自动分配（写入或丢弃）到目标协议的对应字段。
package gateway

import (
	"sort"
	"strings"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
	"github.com/whalegate/whalegate/internal/pkg/util"
)

// 统一参数名（canonical）。用户可用这些名字，也可使用常见同义名。
const (
	ParamTemperature       = "temperature"
	ParamTopP              = "top_p"
	ParamTopK              = "top_k"
	ParamMaxTokens         = "max_tokens"
	ParamStop              = "stop"
	ParamFrequencyPenalty  = "frequency_penalty"
	ParamPresencePenalty   = "presence_penalty"
	ParamRepetitionPenalty = "repetition_penalty"
	ParamSeed              = "seed"
	ParamResponseFormat    = "response_format"
	ParamN                 = "n"
	ParamMinP              = "min_p"
)

// paramNameAliases 归一化映射：去掉下划线/连字符后的小写名 -> 统一名。
var paramNameAliases = map[string]string{
	"temperature":         ParamTemperature,
	"topp":                ParamTopP,
	"nucleussampling":     ParamTopP,
	"topk":                ParamTopK,
	"maxtokens":           ParamMaxTokens,
	"maxoutputtokens":     ParamMaxTokens,
	"maxnewtokens":        ParamMaxTokens,
	"maxcompletiontokens": ParamMaxTokens,
	"maxlength":           ParamMaxTokens,
	"stop":                ParamStop,
	"stopsequences":       ParamStop,
	"frequencypenalty":    ParamFrequencyPenalty,
	"presencepenalty":     ParamPresencePenalty,
	"repetitionpenalty":   ParamRepetitionPenalty,
	"seed":                ParamSeed,
	"randomseed":          ParamSeed,
	"responseformat":      ParamResponseFormat,
	"responsemimetype":    ParamResponseFormat,
	"n":                   ParamN,
	"num":                 ParamN,
	"candidatecount":      ParamN,
	"minp":                ParamMinP,
}

// NormalizeParamName 把任意写法（topK / top-k / max_output_tokens）归一为统一参数名。
func NormalizeParamName(name string) string {
	key := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(name))
	if canonical, ok := paramNameAliases[key]; ok {
		return canonical
	}
	return name
}

// ParamTarget 参数在目标协议中的位置。
type ParamTarget struct {
	// Path 字段路径，例如 ["temperature"] 或 ["generationConfig","temperature"]。
	Path []string
	// Convert 可选的值转换器，返回 false 表示该值不适用。
	Convert func(interface{}) (interface{}, bool)
}

// openAIParamTargets OpenAI 兼容协议的参数映射。
var openAIParamTargets = map[string]ParamTarget{
	ParamTemperature:       {Path: []string{"temperature"}},
	ParamTopP:              {Path: []string{"top_p"}},
	ParamTopK:              {Path: []string{"top_k"}},
	ParamMaxTokens:         {Path: []string{"max_tokens"}},
	ParamStop:              {Path: []string{"stop"}},
	ParamFrequencyPenalty:  {Path: []string{"frequency_penalty"}},
	ParamPresencePenalty:   {Path: []string{"presence_penalty"}},
	ParamRepetitionPenalty: {Path: []string{"repetition_penalty"}},
	ParamSeed:              {Path: []string{"seed"}},
	ParamN:                 {Path: []string{"n"}},
	ParamMinP:              {Path: []string{"min_p"}},
	ParamResponseFormat:    {Path: []string{"response_format"}},
}

// geminiParamTargets Gemini 原生协议的参数映射。
var geminiParamTargets = map[string]ParamTarget{
	ParamTemperature:      {Path: []string{"generationConfig", "temperature"}},
	ParamTopP:             {Path: []string{"generationConfig", "topP"}},
	ParamTopK:             {Path: []string{"generationConfig", "topK"}},
	ParamMaxTokens:        {Path: []string{"generationConfig", "maxOutputTokens"}},
	ParamStop:             {Path: []string{"generationConfig", "stopSequences"}},
	ParamFrequencyPenalty: {Path: []string{"generationConfig", "frequencyPenalty"}},
	ParamPresencePenalty:  {Path: []string{"generationConfig", "presencePenalty"}},
	ParamSeed:             {Path: []string{"generationConfig", "seed"}},
	ParamN:                {Path: []string{"generationConfig", "candidateCount"}},
	// Gemini 用 responseMimeType 表达结构化输出。
	ParamResponseFormat: {Path: []string{"generationConfig", "responseMimeType"}, Convert: convertResponseFormat},
}

// 协议默认支持的参数集（渠道未声明能力时使用）。
var defaultSupportedParams = map[string][]string{
	ProtocolOpenAI: {
		ParamTemperature, ParamTopP, ParamMaxTokens, ParamStop,
		ParamFrequencyPenalty, ParamPresencePenalty, ParamSeed, ParamN, ParamResponseFormat,
	},
	ProtocolGemini: {
		ParamTemperature, ParamTopP, ParamTopK, ParamMaxTokens, ParamStop,
		ParamFrequencyPenalty, ParamPresencePenalty, ParamSeed, ParamN, ParamResponseFormat,
	},
}

// convertResponseFormat 把 OpenAI 的 response_format 转换为 Gemini 的 MIME 类型。
func convertResponseFormat(v interface{}) (interface{}, bool) {
	switch typed := v.(type) {
	case string:
		return mimeFromResponseFormat(typed), true
	case map[string]interface{}:
		if t, ok := typed["type"].(string); ok {
			return mimeFromResponseFormat(t), true
		}
	}
	return nil, false
}

func mimeFromResponseFormat(kind string) string {
	switch strings.ToLower(kind) {
	case "json_object", "json", "application/json":
		return "application/json"
	case "text", "text/plain":
		return "text/plain"
	default:
		return "text/plain"
	}
}

// ParamReport 参数分配结果，用于回显给客户端与写入日志。
type ParamReport struct {
	// Applied 已写入上游请求的参数（统一名，升序）。
	Applied []string `json:"applied"`
	// Dropped 因上游不支持而被丢弃的参数。
	Dropped []string `json:"dropped"`
}

// SupportedParams 计算渠道实际支持的参数集：
// 优先渠道声明的 supported，否则用协议默认集；再扣除 exclude。
func SupportedParams(channelType string, schema *spec.ParamSchema) map[string]struct{} {
	set := make(map[string]struct{})
	if schema != nil && len(schema.Supported) > 0 {
		for _, name := range schema.Supported {
			set[NormalizeParamName(name)] = struct{}{}
		}
	} else {
		for _, name := range defaultSupportedParams[channelType] {
			set[name] = struct{}{}
		}
	}
	if schema != nil {
		for _, name := range schema.Exclude {
			delete(set, NormalizeParamName(name))
		}
	}
	return set
}

// CollectParams 汇总请求中的全部参数：标准字段 + 用户自定义 Extra。
// Extra 优先，允许用户显式覆盖标准字段；别名会被归一化。
func CollectParams(u *spec.UnifiedRequest) map[string]interface{} {
	params := make(map[string]interface{})
	if u == nil {
		return params
	}
	if u.Temperature != nil {
		params[ParamTemperature] = *u.Temperature
	}
	if u.TopP != nil {
		params[ParamTopP] = *u.TopP
	}
	if u.MaxTokens != nil && *u.MaxTokens > 0 {
		params[ParamMaxTokens] = *u.MaxTokens
	}
	if len(u.Stop) > 0 {
		params[ParamStop] = u.Stop
	}
	for k, v := range u.Extra {
		if v == nil {
			continue
		}
		params[NormalizeParamName(k)] = v
	}
	return params
}

// ApplyParams 按渠道能力把参数写入上游请求体：
// 支持的参数写入协议对应路径，不支持的从请求体中移除（避免上游报错）。
func ApplyParams(
	payload map[string]interface{},
	channelType string,
	params map[string]interface{},
	schema *spec.ParamSchema,
) ParamReport {
	report := ParamReport{Applied: []string{}, Dropped: []string{}}
	if payload == nil {
		return report
	}

	targets := openAIParamTargets
	if channelType == ProtocolGemini {
		targets = geminiParamTargets
	}
	supported := SupportedParams(channelType, schema)

	// 默认值：仅在用户未显式提供时生效。
	for name, value := range schemaDefaults(schema) {
		if _, ok := params[name]; !ok {
			params[name] = value
		}
	}

	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		value := params[name]
		target, hasTarget := targets[name]
		if _, ok := supported[name]; !ok || !hasTarget {
			// 上游不支持或协议无对应字段：丢弃，并清理适配器可能已写入的值。
			if hasTarget {
				deletePath(payload, target.Path)
			}
			report.Dropped = append(report.Dropped, name)
			continue
		}

		converted := value
		if target.Convert != nil {
			v, ok := target.Convert(value)
			if !ok {
				deletePath(payload, target.Path)
				report.Dropped = append(report.Dropped, name)
				continue
			}
			converted = v
		}
		if setPath(payload, target.Path, converted) {
			report.Applied = append(report.Applied, name)
		}
	}
	return report
}

func schemaDefaults(schema *spec.ParamSchema) map[string]interface{} {
	if schema == nil || len(schema.Defaults) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(schema.Defaults))
	for k, v := range schema.Defaults {
		if v == nil {
			continue
		}
		out[NormalizeParamName(k)] = v
	}
	return out
}

// setPath 按路径写入嵌套 map，自动补齐中间层。
func setPath(root map[string]interface{}, path []string, value interface{}) bool {
	if len(path) == 0 || root == nil {
		return false
	}
	current := root
	for _, key := range path[:len(path)-1] {
		next, ok := current[key].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			current[key] = next
		}
		current = next
	}
	current[path[len(path)-1]] = value
	return true
}

// deletePath 按路径删除字段，若中间层变空则一并删除。
func deletePath(root map[string]interface{}, path []string) {
	if len(path) == 0 || root == nil {
		return
	}
	if len(path) == 1 {
		delete(root, path[0])
		return
	}
	parent, ok := root[path[0]].(map[string]interface{})
	if !ok {
		return
	}
	deletePath(parent, path[1:])
	if len(parent) == 0 {
		delete(root, path[0])
	}
}

// MergeExtra 合并两段自定义参数（后者优先）。
func MergeExtra(base, extra map[string]interface{}) map[string]interface{} {
	if len(base) == 0 {
		return extra
	}
	if len(extra) == 0 {
		return base
	}
	return util.MergeMap(base, extra)
}
