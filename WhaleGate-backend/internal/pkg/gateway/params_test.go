package gateway

import (
	"reflect"
	"testing"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

func TestNormalizeParamName(t *testing.T) {
	cases := map[string]string{
		"temperature":           ParamTemperature,
		"Temperature":           ParamTemperature,
		"top_k":                 ParamTopK,
		"topK":                  ParamTopK,
		"top-k":                 ParamTopK,
		"max_tokens":            ParamMaxTokens,
		"max_output_tokens":     ParamMaxTokens,
		"max_completion_tokens": ParamMaxTokens,
		"max_new_tokens":        ParamMaxTokens,
		"stop_sequences":        ParamStop,
		"response_format":       ParamResponseFormat,
		"response_mime_type":    ParamResponseFormat,
		"candidate_count":       ParamN,
		"repetition_penalty":    ParamRepetitionPenalty,
	}
	for in, want := range cases {
		if got := NormalizeParamName(in); got != want {
			t.Errorf("NormalizeParamName(%q) = %q，期望 %q", in, got, want)
		}
	}
	// 未登记的参数名原样保留，交由能力集过滤。
	if got := NormalizeParamName("my_custom_flag"); got != "my_custom_flag" {
		t.Errorf("未登记参数应原样返回，实际 %q", got)
	}
}

func TestCollectParamsStandardFields(t *testing.T) {
	temp := 0.7
	topP := 0.9
	maxTokens := 256
	u := &spec.UnifiedRequest{
		Temperature: &temp,
		TopP:        &topP,
		MaxTokens:   &maxTokens,
		Stop:        []string{"END"},
	}
	params := CollectParams(u)
	if params[ParamTemperature] != 0.7 || params[ParamTopP] != 0.9 ||
		params[ParamMaxTokens] != 256 {
		t.Fatalf("标准字段收集错误: %+v", params)
	}
	if !reflect.DeepEqual(params[ParamStop], []string{"END"}) {
		t.Fatalf("stop 收集错误: %+v", params[ParamStop])
	}
}

func TestCollectParamsExtraOverridesStandard(t *testing.T) {
	temp := 0.7
	u := &spec.UnifiedRequest{
		Temperature: &temp,
		Extra:       map[string]interface{}{"top_k": 40, "temperature": 0.1},
	}
	params := CollectParams(u)
	if params[ParamTemperature] != 0.1 {
		t.Fatalf("Extra 应覆盖标准字段: %+v", params)
	}
	if params[ParamTopK] != 40 {
		t.Fatalf("自定义参数缺失: %+v", params)
	}
}

func TestApplyParamsOpenAIDefault(t *testing.T) {
	payload := map[string]interface{}{"model": "m"}
	params := map[string]interface{}{"temperature": 0.5, "top_k": 40}
	report := ApplyParams(payload, ProtocolOpenAI, params, nil)

	if payload["temperature"] != 0.5 {
		t.Fatalf("temperature 应写入: %+v", payload)
	}
	if _, ok := payload["top_k"]; ok {
		t.Fatalf("OpenAI 默认能力集不含 top_k，不应写入: %+v", payload)
	}
	if !contains(report.Applied, ParamTemperature) || !contains(report.Dropped, ParamTopK) {
		t.Fatalf("分配报告错误: %+v", report)
	}
}

func TestApplyParamsGeminiMapping(t *testing.T) {
	payload := map[string]interface{}{"contents": []interface{}{}}
	params := map[string]interface{}{"temperature": 0.4, "top_k": 20, "max_tokens": 512}
	ApplyParams(payload, ProtocolGemini, params, nil)

	cfg := payload["generationConfig"].(map[string]interface{})
	if cfg["temperature"] != 0.4 || cfg["topK"] != 20 || cfg["maxOutputTokens"] != 512 {
		t.Fatalf("Gemini 参数映射错误: %+v", cfg)
	}
}

func TestApplyParamsResponseFormatConversion(t *testing.T) {
	payload := map[string]interface{}{}
	ApplyParams(payload, ProtocolGemini,
		map[string]interface{}{"response_format": map[string]interface{}{"type": "json_object"}}, nil)
	cfg := payload["generationConfig"].(map[string]interface{})
	if cfg["responseMimeType"] != "application/json" {
		t.Fatalf("response_format 转换错误: %+v", cfg)
	}
}

func TestApplyParamsSchemaSupported(t *testing.T) {
	// 上游声明支持 top_k（如国产兼容 OpenAI 的模型），则应写入。
	payload := map[string]interface{}{}
	schema := &spec.ParamSchema{Supported: []string{"temperature", "top_k", "repetition_penalty"}}
	report := ApplyParams(payload, ProtocolOpenAI,
		map[string]interface{}{"temperature": 0.3, "top_k": 30, "repetition_penalty": 1.1, "seed": 7}, schema)

	if payload["top_k"] != 30 || payload["repetition_penalty"] != 1.1 {
		t.Fatalf("声明支持的参数应写入: %+v", payload)
	}
	if _, ok := payload["seed"]; ok {
		t.Fatalf("未声明支持的 seed 不应写入: %+v", payload)
	}
	if len(report.Applied) != 3 || len(report.Dropped) != 1 {
		t.Fatalf("报告统计错误: %+v", report)
	}
}

func TestApplyParamsExcludeWins(t *testing.T) {
	payload := map[string]interface{}{"temperature": 0.9}
	schema := &spec.ParamSchema{
		Supported: []string{"temperature", "top_p"},
		Exclude:   []string{"temperature"},
	}
	ApplyParams(payload, ProtocolOpenAI, map[string]interface{}{"temperature": 0.9}, schema)
	if _, ok := payload["temperature"]; ok {
		t.Fatalf("exclude 优先级应高于 supported: %+v", payload)
	}
}

// 上游不支持某参数时，适配器已写入的值也应被清除，避免上游报错。
func TestApplyParamsRemovesUnsupportedWrittenValue(t *testing.T) {
	payload := map[string]interface{}{
		"generationConfig": map[string]interface{}{"temperature": 0.8},
	}
	schema := &spec.ParamSchema{Exclude: []string{"temperature"}}
	ApplyParams(payload, ProtocolGemini,
		map[string]interface{}{"temperature": 0.8}, schema)
	if cfg, ok := payload["generationConfig"]; ok {
		t.Fatalf("不支持的参数应连 generationConfig 一起清理: %+v", cfg)
	}
}

func TestApplyParamsDefaults(t *testing.T) {
	payload := map[string]interface{}{}
	schema := &spec.ParamSchema{Defaults: map[string]interface{}{"temperature": 0.2}}
	ApplyParams(payload, ProtocolOpenAI, map[string]interface{}{}, schema)
	if payload["temperature"] != 0.2 {
		t.Fatalf("默认值未生效: %+v", payload)
	}

	// 用户显式传参时默认值不生效。
	payload = map[string]interface{}{}
	ApplyParams(payload, ProtocolOpenAI, map[string]interface{}{"temperature": 0.9}, schema)
	if payload["temperature"] != 0.9 {
		t.Fatalf("用户传参应覆盖默认值: %+v", payload)
	}
}

func TestSupportedParamsFallback(t *testing.T) {
	set := SupportedParams(ProtocolGemini, nil)
	if _, ok := set[ParamTopK]; !ok {
		t.Fatal("Gemini 默认应支持 top_k")
	}
	set = SupportedParams(ProtocolOpenAI, nil)
	if _, ok := set[ParamTopK]; ok {
		t.Fatal("OpenAI 默认不应支持 top_k")
	}
}

func TestSetAndDeletePath(t *testing.T) {
	root := map[string]interface{}{}
	setPath(root, []string{"a", "b"}, 1)
	if root["a"].(map[string]interface{})["b"] != 1 {
		t.Fatalf("嵌套写入失败: %+v", root)
	}
	deletePath(root, []string{"a", "b"})
	if _, ok := root["a"]; ok {
		t.Fatalf("空父节点应被清理: %+v", root)
	}
}

func TestMergeExtra(t *testing.T) {
	merged := MergeExtra(map[string]interface{}{"a": 1}, map[string]interface{}{"b": 2})
	if merged["a"] != 1 || merged["b"] != 2 {
		t.Fatalf("合并失败: %+v", merged)
	}
	if got := MergeExtra(nil, map[string]interface{}{"x": 1}); got["x"] != 1 {
		t.Fatalf("nil base 合并失败: %+v", got)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
