package vendor

import "testing"

func TestFromBaseURL(t *testing.T) {
	cases := []struct {
		baseURL string
		want    string
	}{
		{"https://api.deepseek.com/v1", IDDeepSeek},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", IDDashScope},
		{"https://ark.cn-beijing.volces.com/api/v3", IDVolcArk},
		{"https://my-resource.openai.azure.com", IDAzure},
		{"http://localhost:11434", IDOllama},
		{"http://127.0.0.1:8000/v1", IDVLLM},
		{"https://open.bigmodel.cn/api/paas/v4", IDZhipu},
		{"https://openrouter.ai/api/v1", IDOpenRouter},
		{"https://api.siliconflow.cn/v1", IDSiliconFlow},
		{"https://unknown-vendor.example.com/v1", IDGeneric},
	}
	for _, c := range cases {
		if got := FromBaseURL(c.baseURL); got.ID != c.want {
			t.Errorf("地址 %s 期望 %s，实际 %s", c.baseURL, c.want, got.ID)
		}
	}
}

// 同机部署多套推理栈时，端口是唯一可辨识的信号：
// 误判会让 vLLM 渠道被当成 Ollama 打到 /api/chat。
func TestFromBaseURLLocalStacks(t *testing.T) {
	if got := FromBaseURL("http://localhost:8000/v1"); got.ID != IDVLLM {
		t.Fatalf("localhost:8000 应识别为 vLLM，实际 %s", got.ID)
	}
	if got := FromBaseURL("http://localhost:1234/v1"); got.ID != IDLMStudio {
		t.Fatalf("localhost:1234 应识别为 LM Studio，实际 %s", got.ID)
	}
}

func TestFromModel(t *testing.T) {
	if got := FromModel("deepseek-reasoner"); got.ID != IDDeepSeek {
		t.Fatalf("deepseek 模型应识别为 DeepSeek，实际 %s", got.ID)
	}
	if got := FromModel("doubao-seedance-1-0-pro"); got.ID != IDVolcArk {
		t.Fatalf("豆包模型应识别为火山方舟，实际 %s", got.ID)
	}
	if got := FromModel("totally-unknown-model"); got.ID != IDGeneric {
		t.Fatalf("未知模型应回落通用档案，实际 %s", got.ID)
	}
}

// 各家对同一份「思考过程」用了三种字段名，取错就只剩一条空回复。
func TestReasoningFieldPerVendor(t *testing.T) {
	cases := map[string]string{
		"https://api.deepseek.com/v1":           ReasoningContent,
		"https://openrouter.ai/api/v1":          ReasoningPlain,
		"http://localhost:11434":                ReasoningThinking,
		"https://unknown-vendor.example.com/v1": ReasoningContent,
	}
	for baseURL, want := range cases {
		if got := FromBaseURL(baseURL).ReasoningField(); got != want {
			t.Errorf("地址 %s 思维链字段期望 %s，实际 %s", baseURL, want, got)
		}
	}
}

func TestErrorStylePerVendor(t *testing.T) {
	if got := FromBaseURL("https://qianfan.baidubce.com/v2").ErrorStyle(); got != ErrorBaidu {
		t.Fatalf("千帆错误风格期望 baidu，实际 %s", got)
	}
	if got := FromBaseURL("https://spark-api-open.xf-yun.com/v1").ErrorStyle(); got != ErrorXfyun {
		t.Fatalf("讯飞错误风格期望 xfyun，实际 %s", got)
	}
	if got := FromBaseURL("https://unknown-vendor.example.com/v1").ErrorStyle(); got != ErrorOpenAI {
		t.Fatalf("未知厂商应回落 openai 错误风格，实际 %s", got)
	}
}

// 端点风格决定视频/生图走哪条路径，是厂商适配的核心分支依据。
func TestVideoAndImageStylePerVendor(t *testing.T) {
	if got := FromBaseURL("https://open.bigmodel.cn/api/paas/v4").VideoStyle(); got != VideoZhipu {
		t.Fatalf("智谱视频风格期望 zhipu，实际 %s", got)
	}
	if got := FromBaseURL("https://dashscope.aliyuncs.com/compatible-mode/v1").VideoStyle(); got != VideoDashScope {
		t.Fatalf("百炼视频风格期望 dashscope，实际 %s", got)
	}
	if got := FromBaseURL("https://ark.cn-beijing.volces.com/api/v3").VideoStyle(); got != VideoArk {
		t.Fatalf("方舟视频风格期望 ark，实际 %s", got)
	}
	if got := FromBaseURL("https://api.openai.com/v1").VideoStyle(); got != VideoOpenAI {
		t.Fatalf("OpenAI 视频风格期望 openai，实际 %s", got)
	}
}

// nil 档案要能安全取值：历史渠道与测试里大量走无厂商路径。
func TestNilVendorDefaults(t *testing.T) {
	var v *Vendor
	if v.ReasoningField() != ReasoningContent || v.MaxTokensField() != MaxTokensField {
		t.Fatal("nil 档案应回落默认字段命名")
	}
	if !v.SupportsStreamUsage() {
		t.Fatal("nil 档案默认支持 stream_options")
	}
	if v.ChatPathFor("https://x.example.com", "m") != "/chat/completions" || v.ModelsPathFor("https://x.example.com") != "/models" {
		t.Fatal("nil 档案应回落 OpenAI 端点路径")
	}
}

func TestAllIsSortedAndComplete(t *testing.T) {
	loadFixture(t)
	list := All()
	if len(list) == 0 {
		t.Fatal("载入夹具后档案不应为空")
	}
	for i := 1; i < len(list); i++ {
		if list[i-1].Name > list[i].Name {
			t.Fatalf("厂商列表未按名称排序: %s > %s", list[i-1].Name, list[i].Name)
		}
	}
	if Get(IDDeepSeek) == nil || Get("not-exist") != nil {
		t.Fatal("Get 查找行为异常")
	}
}

// TestLoadSwapsRegistry 档案整体替换：后台改动后必须立刻生效，且不留旧的地址缓存。
func TestLoadSwapsRegistry(t *testing.T) {
	t.Cleanup(func() { Load(fixtureVendors()) })

	Load([]Vendor{{ID: "vllm", Name: "vLLM", Hosts: []string{"8000"}, StreamUsage: true}})
	if got := FromBaseURL("http://localhost:8000/v1"); got.ID != "vllm" {
		t.Fatalf("载入后应命中新档案，实际 %s", got.ID)
	}
	if got := FromBaseURL("https://api.deepseek.com/v1"); got.ID != IDGeneric {
		t.Fatalf("旧档案应已被替换，实际 %s", got.ID)
	}

	// 再换一批，验证缓存被清空而不是沿用上一次的结果。
	Load([]Vendor{{ID: "deepseek", Name: "DeepSeek", Hosts: []string{"api.deepseek.com"}}})
	if got := FromBaseURL("https://api.deepseek.com/v1"); got.ID != "deepseek" {
		t.Fatalf("二次载入后应命中新档案，实际 %s", got.ID)
	}
	if got := FromBaseURL("http://localhost:8000/v1"); got.ID != IDGeneric {
		t.Fatalf("二次载入后旧档案不应残留，实际 %s", got.ID)
	}
}

// TestEmptyRegistryFallsBackToGeneric 表为空是合法状态：识别一律回落通用形态。
func TestEmptyRegistryFallsBackToGeneric(t *testing.T) {
	t.Cleanup(func() { Load(fixtureVendors()) })
	Load(nil)
	if got := FromBaseURL("https://api.deepseek.com/v1"); got.ID != IDGeneric {
		t.Fatalf("空档案应回落通用形态，实际 %s", got.ID)
	}
	if got := FromModel("deepseek-chat"); got.ID != IDGeneric {
		t.Fatalf("空档案下模型识别也应回落，实际 %s", got.ID)
	}
	if !genericUsable(Generic()) {
		t.Fatal("兜底档案必须可用：配置齐全才能支撑无档案时的转发")
	}
}

func genericUsable(v *Vendor) bool {
	return v != nil && v.ReasoningField() != "" && v.MaxTokensField() != "" &&
		v.ChatPathOrDefault() == "/chat/completions"
}
