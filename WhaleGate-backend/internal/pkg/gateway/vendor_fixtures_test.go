package gateway

import (
	"testing"

	"github.com/whalegate/whalegate/internal/pkg/gateway/vendor"
)

// 厂商清单在数据库里（迁移 000027 灌种子），代码不内置厂商。
// 本包的用例因此自带一份最小夹具，只覆盖断言里用到的那几家。
func loadVendorFixture() {
	vendor.Load([]vendor.Vendor{
		{
			ID: "azure", Name: "Azure OpenAI", Group: "海外",
			Hosts:      []string{".openai.azure.com"},
			AuthStyle:  vendor.AuthRaw,
			AuthHeader: "api-key",
			Query:      "api-version=" + vendor.AzureAPIVersion,
			ChatPath:   "/openai/deployments/{model}/chat/completions",
			ModelsPath: "/openai/models",
			Reasoning:  vendor.ReasoningContent, MaxTokens: vendor.MaxTokensField,
			Usage: vendor.UsageOpenAI, Error: vendor.ErrorOpenAI, StreamUsage: true,
		},
		{
			ID: "dashscope", Name: "阿里云百炼", Group: "国内",
			Hosts:         []string{"dashscope.aliyuncs.com"},
			BaseSuffix:    "compatible-mode/v1",
			ChatPath:      "/compatible-mode/v1/chat/completions",
			ModelsPath:    "/compatible-mode/v1/models",
			StreamHeaders: map[string]string{"X-DashScope-SSE": "enable"},
			Reasoning:     vendor.ReasoningContent, MaxTokens: vendor.MaxTokensField,
			Usage: vendor.UsageInputOutput, Error: vendor.ErrorDashScope, StreamUsage: true,
			Video: vendor.VideoDashScope, Image: vendor.ImageDashScope,
		},
		{
			ID: "qianfan", Name: "百度千帆", Group: "国内",
			Hosts:    []string{"qianfan.baidubce.com"},
			ChatPath: "/chat/completions", ModelsPath: "/models",
			Reasoning: vendor.ReasoningContent, MaxTokens: vendor.MaxTokensField,
			Usage: vendor.UsageOpenAI, Response: vendor.ResponseLegacy,
			Error: vendor.ErrorBaidu, StreamUsage: true,
		},
		{
			ID: "ollama", Name: "Ollama", Group: "本地",
			Hosts:    []string{"11434", "ollama"},
			ChatPath: "/v1/chat/completions", ModelsPath: "/v1/models",
			Reasoning: vendor.ReasoningThinking, MaxTokens: vendor.MaxTokensField,
			Usage: vendor.UsageOpenAI, Error: vendor.ErrorOpenAI, StreamUsage: false,
		},
		{
			ID: "zhipu", Name: "智谱 GLM", Group: "国内",
			Hosts:     []string{"open.bigmodel.cn"},
			Reasoning: vendor.ReasoningContent, MaxTokens: vendor.MaxTokensField,
			Usage: vendor.UsageOpenAI, Error: vendor.ErrorOpenAI, StreamUsage: true,
			Video: vendor.VideoZhipu,
		},
		{
			ID: "volc-ark", Name: "火山方舟", Group: "国内",
			Hosts:     []string{"volces.com"},
			Reasoning: vendor.ReasoningContent, MaxTokens: vendor.MaxTokensField,
			Usage: vendor.UsageOpenAI, Error: vendor.ErrorOpenAI, StreamUsage: true,
			Video: vendor.VideoArk,
		},
		{
			ID: "deepseek", Name: "DeepSeek", Group: "国内",
			Hosts: []string{"api.deepseek.com"}, ModelHints: []string{"deepseek"},
			Reasoning: vendor.ReasoningContent, MaxTokens: vendor.MaxTokensField,
			Usage: vendor.UsageOpenAI, Error: vendor.ErrorOpenAI, StreamUsage: true,
			Video: vendor.VideoOpenAI,
		},
	})
}

func TestMain(m *testing.M) {
	loadVendorFixture()
	m.Run()
}
