// Package vendor 维护「同一套 OpenAI 兼容协议之上，各家的细节差异」。
//
// 几乎所有厂商都自称 OpenAI 兼容，但兼容得不彻底：思维链叫 reasoning_content
// 还是 thinking、上限字段是 max_tokens 还是 max_completion_tokens、usage 用
// prompt_tokens 还是 input_tokens、错误体是 error.message 还是 error_msg。
// 这些差异过去只能散落在各处 if 里，新增一家要改好几个文件；
// 这里把它们收敛成一张表，新增厂商只需在 registry 里加一条记录。
//
// 本包只描述数据，不发起任何网络请求，可被各协议适配器安全引用。
package vendor

import (
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// 厂商标识。未命中任何已知厂商时返回 IDGeneric。
const (
	IDGeneric     = "generic"
	IDOpenAI      = "openai"
	IDAzure       = "azure"
	IDAnthropic   = "anthropic"
	IDGemini      = "gemini"
	IDDeepSeek    = "deepseek"
	IDMoonshot    = "moonshot"
	IDZhipu       = "zhipu"
	IDDashScope   = "dashscope"
	IDVolcArk     = "volc-ark"
	IDQianFan     = "qianfan"
	IDXfyun       = "xfyun"
	IDMiniMax     = "minimax"
	IDHunyuan     = "hunyuan"
	IDBaichuan    = "baichuan"
	IDSiliconFlow = "siliconflow"
	IDOpenRouter  = "openrouter"
	IDGroq        = "groq"
	IDTogether    = "together"
	IDFireworks   = "fireworks"
	IDBedrock     = "bedrock"
	IDOllama      = "ollama"
	IDVLLM        = "vllm"
	IDLocalAI     = "localai"
	IDXinference  = "xinference"
	IDLMStudio    = "lmstudio"
)

// 思维链字段命名。同一份推理正文，各家放的字段名不一样。
const (
	// ReasoningContent 主流命名：DeepSeek / 通义 / 智谱 / Kimi / 硅基流动 / vLLM。
	ReasoningContent = "reasoning_content"
	// ReasoningPlain OpenRouter 与 Groq 使用的同义字段。
	ReasoningPlain = "reasoning"
	// ReasoningThinking Ollama（>=0.9）与部分本地推理栈使用的字段名。
	ReasoningThinking = "thinking"
)

// 输出上限字段名。OpenAI 的 o 系列与 gpt-5 只认 max_completion_tokens。
const (
	MaxTokensField           = "max_tokens"
	MaxCompletionTokensField = "max_completion_tokens"
)

// Usage 字段风格。
const (
	// UsageOpenAI prompt_tokens / completion_tokens，绝大多数兼容厂商沿用。
	UsageOpenAI = "openai"
	// UsageInputOutput 通义 DashScope、部分聚合平台用 input_tokens / output_tokens。
	UsageInputOutput = "input_output"
	// UsageCamel Bedrock Converse 风格：inputTokens / outputTokens。
	UsageCamel = "camel"
	// UsageEval Ollama：prompt_eval_count / eval_count，且只在流结束时给出。
	UsageEval = "eval"
)

// 错误体风格。
const (
	// ErrorOpenAI {"error":{"message":...}}，兼容生态的通用形态。
	ErrorOpenAI = "openai"
	// ErrorMessage 顶层 {"message": ...}，部分聚合平台如此返回。
	ErrorMessage = "message"
	// ErrorDashScope {"code":..,"message":..} 或包一层 error。
	ErrorDashScope = "dashscope"
	// ErrorBaidu 文心：{"error_code":..,"error_msg":..}。
	ErrorBaidu = "baidu"
	// ErrorXfyun 讯飞：{"code":..,"message":..,"sid":..}。
	ErrorXfyun = "xfyun"
)

// 视频端点风格。视频生成没有统一标准，路径与字段各家自定。
const (
	VideoOpenAI    = "openai"
	VideoZhipu     = "zhipu"
	VideoDashScope = "dashscope"
	VideoArk       = "ark"
)

// 文生图端点风格。
const (
	ImageOpenAI    = "openai"
	ImageDashScope = "dashscope"
	ImageArk       = "ark"
)

// 协议：厂商官方端点使用的渠道协议。
//
// 判据是「上游是否提供 OpenAI 兼容端点」：提供的一律走 openai 协议，
// 差异（路径、鉴权、字段命名）由厂商档案补齐；
// 不提供的（Gemini / Anthropic 这类自有报文格式）才需要专属适配器。
const (
	ProtocolOpenAI    = "openai"
	ProtocolGemini    = "gemini"
	ProtocolAnthropic = "anthropic"
)

// 鉴权风格。
const (
	// AuthBearer Authorization: Bearer <key>，兼容生态的通用形态。
	AuthBearer = "bearer"
	// AuthRaw 头里直接放密钥，不带 Bearer 前缀（Azure 的 api-key）。
	AuthRaw = "raw"
)

// 响应体风格。
const (
	// ResponseOpenAI 标准 choices[] 结构。
	ResponseOpenAI = "openai"
	// ResponseLegacy 老版字段兜底：正文在 result、错误在 error_code/error_msg（百度千帆旧端点）。
	ResponseLegacy = "legacy"
)

// AzureAPIVersion Azure OpenAI 要求的 api-version 查询参数。
const AzureAPIVersion = "2024-10-01-preview"

// Vendor 一家上游的档案。
// Vendor 一家上游的适配档案。
//
// 档案本身是数据而不是逻辑：它存在 vendor_profiles 表里，由管理员在后台维护，
// 启动时灌入内存供识别使用。本包只负责「档案怎么解读」，不内置任何厂商清单。
type Vendor struct {
	// ID 唯一标识，见 ID* 常量。
	ID string `json:"id"`
	// Name 展示名。
	Name string `json:"name"`
	// Group 分组：国内 / 海外 / 聚合 / 本地，供前端归类展示。
	Group string `json:"group"`
	// Hosts 上游地址中出现的主机关键词（小写子串），用于按地址识别厂商。
	Hosts []string `json:"hosts,omitempty"`
	// ModelHints 模型名关键词，地址识别不到时按模型名兜底。
	ModelHints []string `json:"model_hints,omitempty"`
	// BaseURL 官方 OpenAI 兼容端点（含版本前缀），用作接入预设。
	BaseURL string `json:"base_url,omitempty"`
	// Protocol 建议的渠道协议类型；为空表示走 OpenAI 兼容。
	Protocol string `json:"protocol,omitempty"`
	// AuthHeader 主鉴权头名，为空表示标准 Authorization: Bearer。
	AuthHeader string `json:"auth_header,omitempty"`
	// AuthStyle 鉴权风格，见 Auth* 常量；为空表示 Bearer。
	AuthStyle string `json:"auth_style,omitempty"`
	// Query 追加到请求地址上的查询串（例如 Azure 的 api-version=…）。
	Query string `json:"query,omitempty"`
	// ExtraHeaders 该厂商要求的附加请求头（例如百炼流式的 X-DashScope-SSE）。
	ExtraHeaders map[string]string `json:"extra_headers,omitempty"`
	// StreamHeaders 仅流式请求追加的头，避免非流式多下发无用字段。
	StreamHeaders map[string]string `json:"stream_headers,omitempty"`
	// ChatPath 相对 BaseURL 的对话路径，默认 /chat/completions。
	// 支持 {model} 占位符（Azure 把部署名写在路径里）。
	ChatPath string `json:"chat_path,omitempty"`
	// ModelsPath 相对 BaseURL 的模型列表路径，默认 /models。
	ModelsPath string `json:"models_path,omitempty"`
	// BaseSuffix 官方端点里那段版本前缀（如百炼的 compatible-mode/v1）。
	// 管理员填的地址已经带上它时，路径只补 /chat/completions，避免拼出两段前缀。
	BaseSuffix string `json:"base_suffix,omitempty"`
	// Reasoning 思维链字段名，见 Reasoning* 常量；空表示不产出思维链。
	Reasoning string `json:"reasoning,omitempty"`
	// MaxTokens 输出上限字段名，见 MaxTokens* 常量。
	MaxTokens string `json:"max_tokens,omitempty"`
	// Usage usage 字段风格，见 Usage* 常量。
	Usage string `json:"usage,omitempty"`
	// Response 响应体风格，见 Response* 常量；legacy 表示要兼容老版字段。
	Response string `json:"response,omitempty"`
	// Error 错误体风格，见 Error* 常量。
	Error string `json:"error,omitempty"`
	// StreamUsage 是否支持 stream_options.include_usage。
	// 不支持的厂商不能下发该字段，否则上游直接 400，
	// 网关会在流结束后改用估算口径计量。
	StreamUsage bool `json:"stream_usage"`
	// ThinkingParam 思考开关的参数名（enable_thinking / thinking / reasoning_effort…）。
	ThinkingParam string `json:"thinking_param,omitempty"`
	// ExtraParams 该厂商额外支持的统一参数名（除协议默认集之外）。
	ExtraParams []string `json:"extra_params,omitempty"`
	// Video 视频端点风格，见 Video* 常量。
	Video string `json:"video,omitempty"`
	// Image 文生图端点风格，见 Image* 常量。
	Image string `json:"image,omitempty"`
	// Capabilities 该厂商通常具备的能力。
	Capabilities []string `json:"capabilities,omitempty"`
	// Note 给管理员看的接入提示（例如「官方仅提供 OpenAI 兼容端点」）。
	Note string `json:"note,omitempty"`
}

// ---------------------------------------------------------------- 默认值

// ChatPathOrDefault 对话路径，未配置时回落 OpenAI 形态。
func (v *Vendor) ChatPathOrDefault() string {
	if v != nil && v.ChatPath != "" {
		return v.ChatPath
	}
	return "/chat/completions"
}

// ChatPathFor 按实际填写的 Base URL 给出对话路径。
//
// 两处需要因地制宜：
//  1. 路径里的 {model} 要换成真实模型名——Azure 把部署名写在 URL 上；
//  2. 管理员填的地址可能已经带上版本前缀（百炼的 compatible-mode/v1），
//     此时只补 /chat/completions，否则会拼出两段前缀变成 404。
func (v *Vendor) ChatPathFor(baseURL, model string) string {
	return v.resolvePath(baseURL, v.ChatPathOrDefault(), model)
}

// ModelsPathFor 模型列表路径，处理规则同 ChatPathFor。
func (v *Vendor) ModelsPathFor(baseURL string) string {
	path := "/models"
	if v != nil && v.ModelsPath != "" {
		path = v.ModelsPath
	}
	return v.resolvePath(baseURL, path, "")
}

// resolvePath 处理版本前缀去重与 {model} 占位替换。
func (v *Vendor) resolvePath(baseURL, path, model string) string {
	if v != nil && v.BaseSuffix != "" && hasSuffixPath(baseURL, v.BaseSuffix) {
		path = strings.TrimPrefix(path, "/"+v.BaseSuffix)
	}
	if model != "" {
		path = strings.ReplaceAll(path, "{model}", url.PathEscape(model))
	}
	return path
}

// hasSuffixPath 判断地址是否已以给定的相对路径结尾。
func hasSuffixPath(baseURL, suffix string) bool {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	want := "/" + strings.Trim(suffix, "/")
	return strings.HasSuffix(strings.ToLower(base), strings.ToLower(want))
}

// QueryOrDefault 请求地址上要追加的查询串，无档案时为空。
func (v *Vendor) QueryOrDefault() string {
	if v != nil {
		return v.Query
	}
	return ""
}

// HasCustomEndpoint 该厂商是否需要走自定义端点或鉴权方式。
//
// 为 true 时自动探测会先试它的官方端点，而不是通用 /models
// （Azure 要 api-key + api-version，Ollama 的兼容端点在 /v1 下）。
func (v *Vendor) HasCustomEndpoint() bool {
	return v != nil && (v.ModelsPath != "" || v.AuthHeader != "" || v.Query != "")
}

// AuthStyleOrDefault 鉴权风格。
func (v *Vendor) AuthStyleOrDefault() string {
	if v != nil && v.AuthStyle != "" {
		return v.AuthStyle
	}
	return AuthBearer
}

// AuthHeaderOrDefault 主鉴权头名。
func (v *Vendor) AuthHeaderOrDefault() string {
	if v != nil && v.AuthHeader != "" {
		return v.AuthHeader
	}
	return "Authorization"
}

// HeaderOverrides 该厂商要求的附加请求头，stream 为真时并入流式专用头。
//
// 返回的是副本，调用方改动不会影响档案表（档案表是全局共享的）。
func (v *Vendor) HeaderOverrides(stream bool) map[string]string {
	if v == nil {
		return nil
	}
	out := make(map[string]string, len(v.ExtraHeaders)+len(v.StreamHeaders))
	for k, val := range v.ExtraHeaders {
		out[k] = val
	}
	if stream {
		for k, val := range v.StreamHeaders {
			out[k] = val
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ReasoningField 思维链字段名，无档案时按主流命名处理。
func (v *Vendor) ReasoningField() string {
	if v != nil && v.Reasoning != "" {
		return v.Reasoning
	}
	return ReasoningContent
}

// MaxTokensFieldOr 输出上限字段名。
func (v *Vendor) MaxTokensField() string {
	if v != nil && v.MaxTokens != "" {
		return v.MaxTokens
	}
	return MaxTokensField
}

// UsageStyle usage 字段风格。
func (v *Vendor) UsageStyle() string {
	if v != nil && v.Usage != "" {
		return v.Usage
	}
	return UsageOpenAI
}

// ResponseStyle 响应体风格。
func (v *Vendor) ResponseStyle() string {
	if v != nil && v.Response != "" {
		return v.Response
	}
	return ResponseOpenAI
}

// ErrorStyle 错误体风格。
func (v *Vendor) ErrorStyle() string {
	if v != nil && v.Error != "" {
		return v.Error
	}
	return ErrorOpenAI
}

// VideoStyle 视频端点风格。
func (v *Vendor) VideoStyle() string {
	if v != nil && v.Video != "" {
		return v.Video
	}
	return VideoOpenAI
}

// ImageStyle 文生图端点风格。
func (v *Vendor) ImageStyle() string {
	if v != nil && v.Image != "" {
		return v.Image
	}
	return ImageOpenAI
}

// ProtocolOrDefault 建议的渠道协议。
func (v *Vendor) ProtocolOrDefault() string {
	if v != nil && v.Protocol != "" {
		return v.Protocol
	}
	return ProtocolOpenAI
}

// SupportsStreamUsage 是否可下发 stream_options。
//
// 档案缺失时返回 true：绝大多数兼容平台都支持，
// 未知厂商按「支持」处理，出错时上游会明确报错，比一律不下发更容易定位。
func (v *Vendor) SupportsStreamUsage() bool {
	if v == nil || v.ID == IDGeneric {
		return true
	}
	return v.StreamUsage
}

// ---------------------------------------------------------------- 档案表

// 默认能力集：多数厂商都具备对话能力。
var (
	capChatOnly = []string{"chat"}
	capStandard = []string{"chat", "vision", "tool", "reasoning"}
	capFull     = []string{"chat", "vision", "tool", "reasoning", "image", "embedding"}
	capLocal    = []string{"chat", "vision", "tool", "embedding"}
)

// registry 厂商档案表。新增一家厂商只需在这里追加一条。
// ---------------------------------------------------------------- 档案存储
//
// 厂商清单不再写死在代码里：它在 vendor_profiles 表中，启动时由 service 载入，
// 后台改动后重新 Load。代码里只保留「识别规则」与兜底档案。

type vendorSet struct {
	list   []Vendor
	byID   map[string]*Vendor
	sorted []Vendor
}

// store 当前生效的档案集合；整体替换，读侧无锁。
var store atomic.Value

// Load 载入档案集合，同时清空地址识别缓存。
//
// 用 atomic 整体替换而不是就地改 map：识别发生在每个请求的关键路径上，
// 就地改会让读写产生竞态，整体替换则只需一次原子写。
func Load(list []Vendor) {
	set := &vendorSet{list: append([]Vendor(nil), list...), byID: map[string]*Vendor{}}
	for i := range set.list {
		set.byID[set.list[i].ID] = &set.list[i]
	}
	set.sorted = append([]Vendor(nil), set.list...)
	sort.Slice(set.sorted, func(i, j int) bool { return set.sorted[i].Name < set.sorted[j].Name })
	store.Store(set)

	hostCacheMu.Lock()
	hostCache = map[string]*Vendor{}
	hostCacheMu.Unlock()
}

// current 取当前档案集合；未载入时返回 nil（调用方按兜底处理）。
func current() *vendorSet {
	if v, ok := store.Load().(*vendorSet); ok {
		return v
	}
	return nil
}

// Loaded 档案是否已载入（表为空或尚未初始化时为 false）。
func Loaded() bool { return current() != nil }

// ---------------------------------------------------------------- 可选维度
//
// 这些取值原本散落在各处的常量里，前端要写死一份才能做下拉。
// 统一暴露出来后，新增一种风格只需加常量，前端自动多一个选项。

func ReasoningFields() []string {
	return []string{"", ReasoningContent, ReasoningPlain, ReasoningThinking}
}

func UsageStyles() []string {
	return []string{UsageOpenAI, UsageInputOutput, UsageCamel, UsageEval}
}
func ResponseStyles() []string { return []string{ResponseOpenAI, ResponseLegacy} }

func ErrorStyles() []string {
	return []string{ErrorOpenAI, ErrorMessage, ErrorDashScope, ErrorBaidu, ErrorXfyun}
}

func AuthStyles() []string { return []string{AuthBearer, AuthRaw} }

func VideoStyles() []string {
	return []string{VideoOpenAI, VideoZhipu, VideoDashScope, VideoArk}
}

func ImageStyles() []string { return []string{ImageOpenAI, ImageDashScope, ImageArk} }

// Groups 分组名；档案里出现过的分组也会一并带上，
// 这样管理员在后台自定义的分组不会在下拉里丢失。
func Groups() []string {
	base := []string{"国内", "海外", "聚合", "本地", "其他"}
	seen := map[string]bool{}
	for _, g := range base {
		seen[g] = true
	}
	if set := current(); set != nil {
		for i := range set.list {
			if g := strings.TrimSpace(set.list[i].Group); g != "" && !seen[g] {
				base = append(base, g)
				seen[g] = true
			}
		}
	}
	return base
}

var generic = Vendor{
	ID: IDGeneric, Name: "通用 OpenAI 兼容", Group: "其他",
	Reasoning: ReasoningContent, MaxTokens: MaxTokensField,
	Usage: UsageOpenAI, Error: ErrorOpenAI, StreamUsage: true,
	Capabilities: capChatOnly,
}

// ---------------------------------------------------------------- 查询

// All 返回全部已载入档案（按名称排序），供前端做接入预设。
func All() []Vendor {
	set := current()
	if set == nil {
		return nil
	}
	out := make([]Vendor, len(set.sorted))
	copy(out, set.sorted)
	return out
}

// Get 按 ID 取档案，未找到返回 nil。
func Get(id string) *Vendor {
	if id == "" {
		return nil
	}
	if set := current(); set != nil {
		return set.byID[id]
	}
	return nil
}

// Generic 返回兜底档案。
func Generic() *Vendor { return &generic }

// hostCache 缓存地址 -> 厂商的识别结果，避免每个请求重复做子串匹配。
var (
	hostCacheMu sync.RWMutex
	hostCache   = map[string]*Vendor{}
)

// FromBaseURL 按上游地址识别厂商：匹配主机关键词，未命中返回兜底档案。
//
// 识别只影响「差异字段怎么填」，即使识别错了也只会退回通用 OpenAI 形态，
// 不会改变请求语义，因此可以安全地按地址猜测。
func FromBaseURL(baseURL string) *Vendor {
	host := hostOf(baseURL)
	if host == "" {
		return &generic
	}
	hostCacheMu.RLock()
	if cached, ok := hostCache[host]; ok {
		hostCacheMu.RUnlock()
		return cached
	}
	hostCacheMu.RUnlock()

	set := current()
	if set == nil {
		return &generic
	}
	matched := &generic
	for i := range set.list {
		for _, hint := range set.list[i].Hosts {
			if hint == "" {
				continue
			}
			if strings.Contains(host, hint) {
				matched = &set.list[i]
				break
			}
		}
		if matched.ID != IDGeneric {
			break
		}
	}
	// 端口类关键词（如 11434）只在内网地址上才可信，公网域名不参与匹配。
	if matched.ID == IDGeneric {
		matched = matchByPort(baseURL)
	}

	hostCacheMu.Lock()
	hostCache[host] = matched
	hostCacheMu.Unlock()
	return matched
}

// FromModel 按模型名兜底识别厂商（地址无法判断时使用）。
func FromModel(model string) *Vendor {
	name := strings.ToLower(strings.TrimSpace(model))
	if name == "" {
		return &generic
	}
	set := current()
	if set == nil {
		return &generic
	}
	for i := range set.list {
		for _, hint := range set.list[i].ModelHints {
			if hint == "" || hint == "/" {
				continue
			}
			if strings.Contains(name, hint) {
				return &set.list[i]
			}
		}
	}
	return &generic
}

// matchByPort 内网地址按端口猜本地推理栈：
// 本地栈通常不改域名，只换端口，端口是唯一可辨识的信号。
// matchByPort 内网地址按端口猜本地推理栈：
// 本地栈通常不改域名，只换端口，端口是唯一可辨识的信号。
// 端口从档案的 Hosts 里取（形如 11434 的条目），不在代码里写死。
func matchByPort(rawURL string) *Vendor {
	lower := strings.ToLower(rawURL)
	isLoopback := strings.Contains(lower, "localhost") ||
		strings.Contains(lower, "127.0.0.1") ||
		strings.Contains(lower, "0.0.0.0") ||
		strings.Contains(lower, "host.docker.internal")
	if !isLoopback {
		return &generic
	}
	set := current()
	if set == nil {
		return &generic
	}
	for i := range set.list {
		for _, hint := range set.list[i].Hosts {
			// 端口类关键词是纯数字：只有内网地址走到这里才可信。
			if hint == "" || strings.ContainsAny(hint, ".:/") {
				continue
			}
			if strings.Contains(lower, ":"+hint) {
				return &set.list[i]
			}
		}
	}
	return &generic
}

// hostOf 从地址中取出小写主机（含端口）。
func hostOf(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "http://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return strings.ToLower(trimmed)
	}
	return strings.ToLower(parsed.Host)
}
