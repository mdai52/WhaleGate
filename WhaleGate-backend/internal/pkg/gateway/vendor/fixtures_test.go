package vendor

import "testing"

// 厂商清单已迁到数据库（迁移 000027 灌入种子），代码里不再内置任何厂商。
// 单测因此自带一份最小夹具：只覆盖断言涉及的厂商，避免测试依赖数据库。
func fixtureVendors() []Vendor {
	return []Vendor{
		{
			ID: "openai", Name: "OpenAI", Group: "海外",
			Hosts: []string{"api.openai.com"}, ModelHints: []string{"gpt-", "o1", "o3"},
			BaseURL:   "https://api.openai.com/v1",
			Reasoning: ReasoningContent, MaxTokens: MaxCompletionTokensField,
			Usage: UsageOpenAI, Error: ErrorOpenAI, StreamUsage: true,
			Capabilities: []string{"chat", "vision", "tool", "embedding"},
		},
		{
			ID: "azure", Name: "Azure OpenAI", Group: "海外",
			Hosts:      []string{".openai.azure.com"},
			BaseURL:    "https://<资源名>.openai.azure.com",
			AuthStyle:  AuthRaw,
			AuthHeader: "api-key",
			Query:      "api-version=" + AzureAPIVersion,
			ChatPath:   "/openai/deployments/{model}/chat/completions",
			ModelsPath: "/openai/models",
			Reasoning:  "", MaxTokens: MaxTokensField,
			Usage: UsageOpenAI, Error: ErrorOpenAI, StreamUsage: true,
		},
		{
			ID: "deepseek", Name: "DeepSeek", Group: "国内",
			Hosts: []string{"api.deepseek.com"}, ModelHints: []string{"deepseek"},
			BaseURL:   "https://api.deepseek.com/v1",
			Reasoning: ReasoningContent, MaxTokens: MaxTokensField,
			Usage: UsageOpenAI, Error: ErrorOpenAI, StreamUsage: true,
			ThinkingParam: "enable_thinking", Video: VideoOpenAI, Image: ImageOpenAI,
		},
		{
			ID: "dashscope", Name: "阿里云百炼", Group: "国内",
			Hosts: []string{"dashscope.aliyuncs.com"}, ModelHints: []string{"qwen", "wanx"},
			BaseURL:       "https://dashscope.aliyuncs.com/compatible-mode/v1",
			BaseSuffix:    "compatible-mode/v1",
			StreamHeaders: map[string]string{"X-DashScope-SSE": "enable"},
			Reasoning:     ReasoningContent, MaxTokens: MaxTokensField,
			Usage: UsageInputOutput, Error: ErrorDashScope, StreamUsage: true,
			Video: VideoDashScope, Image: ImageDashScope,
		},
		{
			ID: "volc-ark", Name: "火山方舟", Group: "国内",
			Hosts: []string{"volces.com"}, ModelHints: []string{"doubao"},
			BaseURL:   "https://ark.cn-beijing.volces.com/api/v3",
			Reasoning: ReasoningContent, MaxTokens: MaxTokensField,
			Usage: UsageOpenAI, Error: ErrorOpenAI, StreamUsage: true,
			Video: VideoArk,
		},
		{
			ID: "zhipu", Name: "智谱 GLM", Group: "国内",
			Hosts: []string{"open.bigmodel.cn"}, ModelHints: []string{"glm-"},
			BaseURL:   "https://open.bigmodel.cn/api/paas/v4",
			Reasoning: ReasoningContent, MaxTokens: MaxTokensField,
			Usage: UsageOpenAI, Error: ErrorOpenAI, StreamUsage: true,
			Video: VideoZhipu,
		},
		{
			ID: "qianfan", Name: "百度千帆", Group: "国内",
			Hosts: []string{"qianfan.baidubce.com"}, ModelHints: []string{"ernie"},
			BaseURL:   "https://qianfan.baidubce.com/v2",
			Reasoning: ReasoningContent, MaxTokens: MaxTokensField,
			Usage: UsageOpenAI, Response: ResponseLegacy, Error: ErrorBaidu, StreamUsage: true,
		},
		{
			ID: "xfyun", Name: "讯飞星火", Group: "国内",
			Hosts: []string{"xf-yun.com"}, ModelHints: []string{"spark"},
			Reasoning: ReasoningContent, MaxTokens: MaxTokensField,
			Usage: UsageOpenAI, Error: ErrorXfyun, StreamUsage: true,
		},
		{
			ID: "openrouter", Name: "OpenRouter", Group: "聚合",
			Hosts:     []string{"openrouter.ai"},
			Reasoning: ReasoningPlain, MaxTokens: MaxTokensField,
			Usage: UsageOpenAI, Error: ErrorOpenAI, StreamUsage: true,
		},
		{
			ID: "siliconflow", Name: "硅基流动", Group: "国内",
			Hosts:     []string{"api.siliconflow.cn"},
			Reasoning: ReasoningContent, MaxTokens: MaxTokensField,
			Usage: UsageOpenAI, Error: ErrorOpenAI, StreamUsage: true,
		},
		{
			ID: "ollama", Name: "Ollama", Group: "本地",
			Hosts: []string{"11434", "ollama"}, ModelHints: []string{"llama", "qwen"},
			BaseURL:  "http://localhost:11434/v1",
			ChatPath: "/v1/chat/completions", ModelsPath: "/v1/models",
			Reasoning: ReasoningThinking, MaxTokens: MaxTokensField,
			Usage: UsageOpenAI, Error: ErrorOpenAI, StreamUsage: false,
		},
		{
			ID: "vllm", Name: "vLLM", Group: "本地",
			Hosts:     []string{"8000", "vllm"},
			Reasoning: ReasoningContent, MaxTokens: MaxTokensField,
			Usage: UsageOpenAI, Error: ErrorOpenAI, StreamUsage: true,
		},
		{
			ID: "lmstudio", Name: "LM Studio", Group: "本地",
			Hosts:     []string{"1234", "lmstudio"},
			Reasoning: ReasoningContent, MaxTokens: MaxTokensField,
			Usage: UsageOpenAI, Error: ErrorOpenAI, StreamUsage: true,
		},
	}
}

// 载入夹具，保证每个用例开始前都是干净的存储状态。
func loadFixture(t *testing.T) {
	t.Helper()
	Load(fixtureVendors())
}

func TestMain(m *testing.M) {
	Load(fixtureVendors())
	m.Run()
}
