package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	urlpkg "net/url"
	"sort"
	"strings"
	"time"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

// 能力标识。
const (
	CapChat      = "chat"
	CapVision    = "vision"
	CapTool      = "tool"
	CapImage     = "image"
	CapEmbedding = "embedding"
	CapAudio     = "audio"
	CapReasoning = "reasoning"
)

// DetectedModel 探测到的上游模型。
type DetectedModel struct {
	// ID 上游模型标识，已去除 provider 前缀（如 gemini 的 "models/"）。
	ID string `json:"id"`
	// DisplayName 展示名。
	DisplayName string `json:"display_name,omitempty"`
	// Capabilities 能力集合：chat / vision / tool / image / embedding / audio / reasoning。
	Capabilities []string `json:"capabilities,omitempty"`
	// ContextWindow 上下文窗口，未知为 0。
	ContextWindow int `json:"context_window,omitempty"`
	// MaxOutput 最大输出 token，未知为 0。
	MaxOutput int `json:"max_output,omitempty"`
	// Raw 上游返回的原始条目，便于前端调试与未来扩展。
	Raw map[string]interface{} `json:"raw,omitempty"`
}

// ProbeResult 一次探测的结果。
type ProbeResult struct {
	// Type 命中的协议类型：openai / anthropic / claude-code / codex / gemini。
	Type string `json:"type"`
	// BaseURL 规范化后的 base URL（已去除结尾斜杠，保留用户填写的版本前缀）。
	BaseURL string `json:"base_url"`
	// Endpoint 实际命中的模型列表地址。
	Endpoint string `json:"endpoint"`
	// Models 解析出的模型列表。
	Models []DetectedModel `json:"models"`
	// LatencyMS 探测耗时。
	LatencyMS int64 `json:"latency_ms"`
}

// probeSpec 一种协议的探测规则。
type probeSpec struct {
	Type string
	// Paths 依次尝试的相对路径（已考虑用户是否带 /v1 前缀）。
	Paths []string
	// QueryKey 为 true 时密钥通过 ?key= 传递（Gemini）。
	QueryKey bool
	Headers  func(key string) map[string]string
	Parse    func(body []byte) ([]DetectedModel, error)
}

// protocolOrder 探测顺序：hint 指定的协议优先，其余按常见度排列。
func protocolOrder(hint string) []probeSpec {
	all := []probeSpec{
		{
			Type:  ProtocolOpenAI,
			Paths: []string{"/models", "/v1/models"},
			Headers: func(key string) map[string]string {
				return map[string]string{"Authorization": "Bearer " + key}
			},
			Parse: parseOpenAIModels,
		},
		{
			Type:  "anthropic",
			Paths: []string{"/v1/models", "/models"},
			Headers: func(key string) map[string]string {
				return map[string]string{
					"x-api-key":         key,
					"anthropic-version": "2023-06-01",
				}
			},
			Parse: parseOpenAIModels,
		},
		{
			Type:  "claude-code",
			Paths: []string{"/v1/models"},
			Headers: func(key string) map[string]string {
				return map[string]string{
					"Authorization":     "Bearer " + key,
					"anthropic-version": "2023-06-01",
				}
			},
			Parse: parseOpenAIModels,
		},
		{
			Type:  "codex",
			Paths: []string{"/models", "/v1/models"},
			Headers: func(key string) map[string]string {
				return map[string]string{"Authorization": "Bearer " + key}
			},
			Parse: parseOpenAIModels,
		},
		{
			Type:     ProtocolGemini,
			Paths:    []string{"/models", "/v1beta/models", "/v1/models"},
			QueryKey: true,
			Headers:  func(string) map[string]string { return nil },
			Parse:    parseGeminiModels,
		},
	}
	if hint == "" {
		return all
	}
	ordered := make([]probeSpec, 0, len(all))
	for _, p := range all {
		if p.Type == hint {
			ordered = append(ordered, p)
		}
	}
	for _, p := range all {
		if p.Type != hint {
			ordered = append(ordered, p)
		}
	}
	return ordered
}

// ProbeUpstream 用给定的地址与密钥探测上游协议与模型列表。
// hint 为空时自动按 openai → anthropic → claude-code → codex → gemini 顺序试探。
func ProbeUpstream(ctx context.Context, baseURL, apiKey, hint string) (*ProbeResult, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, ErrEmptyBaseURL
	}
	if apiKey == "" {
		return nil, ErrEmptyAPIKey
	}
	if ctx == nil {
		ctx = context.Background()
	}

	client := NewUpstream(15 * time.Second)
	var lastErr error
	// authErr 记录首个鉴权失败：鉴权失败往往是"协议不匹配"而非密钥错误
	// （例如用 Bearer 请求 Gemini 上游必然 401），需继续试探其他协议。
	var authErr error
	for _, p := range protocolOrder(hint) {
		for _, path := range p.Paths {
			target := base + path
			if p.QueryKey {
				target += "?key=" + urlpkg.QueryEscape(apiKey)
			}
			start := time.Now()
			resp, err := client.Do(ctx, http.MethodGet, target, p.Headers(apiKey), nil)
			if err != nil {
				lastErr = err
				continue
			}
			body, readErr := readLimited(resp.Body, 4<<20)
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
				continue
			}
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
				lastErr = &ProbeError{Type: p.Type, Endpoint: target, Status: resp.StatusCode}
				continue
			}
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				if authErr == nil {
					authErr = &ProbeError{Type: p.Type, Endpoint: target, Status: resp.StatusCode,
						Message: ExtractUpstreamError(p.Type, body)}
				}
				continue
			}
			if resp.StatusCode >= http.StatusBadRequest {
				lastErr = &ProbeError{Type: p.Type, Endpoint: target, Status: resp.StatusCode,
					Message: ExtractUpstreamError(p.Type, body)}
				continue
			}
			models, err := p.Parse(body)
			if err != nil || len(models) == 0 {
				if err != nil {
					lastErr = err
				}
				continue
			}
			sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
			return &ProbeResult{
				Type:      p.Type,
				BaseURL:   base,
				Endpoint:  target,
				Models:    models,
				LatencyMS: time.Since(start).Milliseconds(),
			}, nil
		}
	}
	// 全部协议都未命中：优先给出鉴权失败（对排查最有价值），否则给通用失败。
	if authErr != nil {
		return nil, authErr
	}
	if lastErr == nil {
		lastErr = ErrProbeFailed
	}
	return nil, lastErr
}

// parseOpenAIModels 解析 OpenAI / Anthropic 风格模型列表。
func parseOpenAIModels(body []byte) ([]DetectedModel, error) {
	var obj struct {
		Data []struct {
			ID          string `json:"id"`
			Object      string `json:"object"`
			DisplayName string `json:"display_name"`
			OwnedBy     string `json:"owned_by"`
			Created     int64  `json:"created"`
			// OpenRouter 扩展
			ContextLength int `json:"context_length"`
			Architecture  *struct {
				InputModalities  []string `json:"input_modalities"`
				OutputModalities []string `json:"output_modalities"`
			} `json:"architecture"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &obj); err == nil && len(obj.Data) > 0 {
		out := make([]DetectedModel, 0, len(obj.Data))
		for _, item := range obj.Data {
			if item.ID == "" {
				continue
			}
			m := DetectedModel{
				ID:            item.ID,
				DisplayName:   item.DisplayName,
				ContextWindow: item.ContextLength,
			}
			if item.Architecture != nil {
				m.Capabilities = modalitiesToCaps(item.Architecture.InputModalities, item.Architecture.OutputModalities)
			}
			enrichModel(&m)
			out = append(out, m)
		}
		if len(out) > 0 {
			return out, nil
		}
	}

	// 部分上游直接返回字符串数组。
	var list []string
	if err := json.Unmarshal(body, &list); err == nil && len(list) > 0 {
		out := make([]DetectedModel, 0, len(list))
		for _, id := range list {
			if strings.TrimSpace(id) == "" {
				continue
			}
			m := DetectedModel{ID: id}
			enrichModel(&m)
			out = append(out, m)
		}
		return out, nil
	}

	// 还有上游用 {"models": ["a", "b"]} 或 {"models":[{...}]}
	var alt struct {
		Models json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &alt); err == nil && len(alt.Models) > 0 {
		if strings.HasPrefix(strings.TrimSpace(string(alt.Models)), "\"") ||
			strings.HasPrefix(strings.TrimSpace(string(alt.Models)), "[") {
			return parseOpenAIModels([]byte(`{"data":` + string(alt.Models) + `}`))
		}
	}
	return nil, ErrNoModels
}

// parseGeminiModels 解析 Gemini 原生模型列表。
func parseGeminiModels(body []byte) ([]DetectedModel, error) {
	var resp struct {
		Models []struct {
			Name                       string   `json:"name"`
			DisplayName                string   `json:"displayName"`
			Description                string   `json:"description"`
			InputTokenLimit            int      `json:"inputTokenLimit"`
			OutputTokenLimit           int      `json:"outputTokenLimit"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	out := make([]DetectedModel, 0, len(resp.Models))
	for _, m := range resp.Models {
		id := strings.TrimPrefix(m.Name, "models/")
		if id == "" {
			continue
		}
		caps := make([]string, 0, 4)
		for _, method := range m.SupportedGenerationMethods {
			switch method {
			case "generateContent", "streamGenerateContent":
				caps = append(caps, CapChat)
			case "embedContent":
				caps = append(caps, CapEmbedding)
			case "countTokens":
				// 仅计数能力，不作为对外能力暴露
			}
		}
		dm := DetectedModel{
			ID:            id,
			DisplayName:   m.DisplayName,
			Capabilities:  dedupCaps(caps),
			ContextWindow: m.InputTokenLimit,
			MaxOutput:     m.OutputTokenLimit,
		}
		enrichModel(&dm)
		out = append(out, dm)
	}
	if len(out) == 0 {
		return nil, ErrNoModels
	}
	return out, nil
}

// modalitiesToCaps 把 OpenRouter 的模态声明转成能力集合。
func modalitiesToCaps(inputs, outputs []string) []string {
	caps := []string{}
	has := func(list []string, target string) bool {
		for _, v := range list {
			if strings.EqualFold(v, target) {
				return true
			}
		}
		return false
	}
	if has(inputs, "text") {
		caps = append(caps, CapChat)
	}
	if has(inputs, "image") {
		caps = append(caps, CapVision)
	}
	if has(outputs, "image") {
		caps = append(caps, CapImage)
	}
	if has(outputs, "audio") {
		caps = append(caps, CapAudio)
	}
	if has(inputs, "file") || has(inputs, "audio") {
		caps = append(caps, CapVision)
	}
	return dedupCaps(caps)
}

// enrichModel 依据模型名补全能力与窗口（上游未声明时兜底）。
func enrichModel(m *DetectedModel) {
	if len(m.Capabilities) == 0 {
		m.Capabilities = inferCapabilities(m.ID)
	} else {
		m.Capabilities = dedupCaps(append(m.Capabilities, inferCapabilities(m.ID)...))
	}
	if m.ContextWindow == 0 {
		m.ContextWindow = inferContextWindow(m.ID)
	}
	if m.MaxOutput == 0 {
		m.MaxOutput = inferMaxOutput(m.ID)
	}
	if m.DisplayName == "" {
		m.DisplayName = m.ID
	}
}

// inferCapabilities 按模型名关键词推断能力。
func inferCapabilities(id string) []string {
	name := strings.ToLower(id)
	caps := []string{CapChat}

	switch {
	case containsAny(name, "embedding", "embed", "bge-", "text-embedding", "gte-", "jina-embeddings"):
		return []string{CapEmbedding}
	case containsAny(name, "dall-e", "gpt-image", "flux", "stable-diffusion", "sd-xl", "imagen",
		"midjourney", "kolors", "wanx", "seedream", "recraft"):
		return []string{CapImage}
	case containsAny(name, "whisper", "tts", "speech", "audio", "voice", "sound"):
		return []string{CapAudio}
	case containsAny(name, "rerank", "reranker"):
		return []string{"rerank"}
	}

	if containsAny(name, "vision", "-vl", "4v", "4o", "4.1", "o1", "o3", "o4", "claude-3", "claude-4",
		"gemini", "glm-4v", "qwen-vl", "qwen2-vl", "qwen2.5-vl", "internvl", "minicpm-v", "llava", "pixtral") {
		caps = append(caps, CapVision)
	}
	if containsAny(name, "gpt-4", "gpt-5", "o1", "o3", "o4", "claude", "gemini", "deepseek",
		"qwen", "glm-4", "glm-5", "kimi", "doubao", "hunyuan", "llama-3", "llama-4", "mistral", "command-r") {
		caps = append(caps, CapTool)
	}
	if containsAny(name, "o1", "o3", "o4", "reasoner", "thinking", "-r1", "qwq", "deepseek-r") {
		caps = append(caps, CapReasoning)
	}
	return dedupCaps(caps)
}

// contextHints 已知模型的上下文窗口与输出上限（按顺序匹配）。
var contextHints = []struct {
	match  string
	ctx    int
	maxOut int
}{
	{"gpt-5", 400000, 128000},
	{"gpt-4.1", 1047576, 32768},
	{"gpt-4o", 128000, 16384},
	{"gpt-4-turbo", 128000, 4096},
	{"gpt-4-32k", 32768, 4096},
	{"gpt-4", 8192, 4096},
	{"gpt-3.5", 16385, 4096},
	{"o1", 200000, 100000},
	{"o3", 200000, 100000},
	{"o4", 200000, 100000},
	{"claude-4", 200000, 64000},
	{"claude-3-7", 200000, 64000},
	{"claude-3-5", 200000, 8192},
	{"claude-3", 200000, 4096},
	{"claude-2", 100000, 4096},
	{"gemini-2.5", 1048576, 65536},
	{"gemini-2.0", 1048576, 8192},
	{"gemini-1.5-pro", 2097152, 8192},
	{"gemini-1.5-flash", 1048576, 8192},
	{"gemini-1.0-pro", 32768, 8192},
	{"deepseek", 131072, 8192},
	{"qwen-max", 32768, 8192},
	{"qwen-plus", 131072, 8192},
	{"qwen-turbo", 131072, 8192},
	{"qwen2.5", 131072, 8192},
	{"qwen", 32768, 8192},
	{"glm-4", 131072, 8192},
	{"kimi", 131072, 8192},
	{"moonshot", 131072, 8192},
	{"doubao", 131072, 8192},
	{"hunyuan", 131072, 8192},
	{"ernie", 131072, 8192},
	{"llama-4", 1048576, 8192},
	{"llama-3", 131072, 8192},
	{"llama-2", 4096, 4096},
	{"mixtral", 32768, 8192},
	{"mistral", 32768, 8192},
	{"command-r", 131072, 4096},
	{"grok", 131072, 8192},
}

func inferContextWindow(id string) int {
	name := strings.ToLower(id)
	for _, h := range contextHints {
		if strings.Contains(name, h.match) {
			return h.ctx
		}
	}
	if strings.Contains(name, "32k") {
		return 32768
	}
	if strings.Contains(name, "128k") {
		return 131072
	}
	if strings.Contains(name, "1m") {
		return 1048576
	}
	return 0
}

func inferMaxOutput(id string) int {
	name := strings.ToLower(id)
	for _, h := range contextHints {
		if strings.Contains(name, h.match) {
			return h.maxOut
		}
	}
	return 0
}

// DedupCaps 去重并排序能力集合（对外导出，便于测试与复用）。
func dedupCaps(caps []string) []string {
	seen := make(map[string]struct{}, len(caps))
	out := make([]string, 0, len(caps))
	for _, c := range caps {
		if c == "" {
			continue
		}
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// SuggestedCapabilities 汇总一批模型具备的能力，用于渠道能力标记。
func SuggestedCapabilities(models []DetectedModel) []string {
	var caps []string
	for _, m := range models {
		caps = append(caps, m.Capabilities...)
	}
	return dedupCaps(caps)
}

// FilterModels 按能力过滤模型列表，caps 为空表示不过滤。
func FilterModels(models []DetectedModel, caps []string) []DetectedModel {
	if len(caps) == 0 {
		return models
	}
	want := make(map[string]struct{}, len(caps))
	for _, c := range caps {
		want[c] = struct{}{}
	}
	out := make([]DetectedModel, 0, len(models))
	for _, m := range models {
		for _, c := range m.Capabilities {
			if _, ok := want[c]; ok {
				out = append(out, m)
				break
			}
		}
	}
	return out
}

// spec 引用保持：探测结果可直接构造统一参数声明的默认值。
var _ = spec.ProtocolOpenAI
