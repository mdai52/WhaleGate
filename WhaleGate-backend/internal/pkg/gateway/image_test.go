package gateway

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// pngBytes 一个最小的 PNG 头，用于校验类型嗅探。
var pngBytes = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

func TestSupportsImageGeneration(t *testing.T) {
	if !SupportsImageGeneration(ProtocolOpenAI) {
		t.Fatal("openai 渠道应支持图片生成")
	}
	if SupportsImageGeneration(ProtocolAnthropic) {
		t.Fatal("anthropic 渠道不应支持图片生成")
	}
}

func TestBuildImageUpstreamURL(t *testing.T) {
	url, err := BuildImageUpstreamURL(ProtocolOpenAI, "https://api.example.com/v1/", "gpt-image-2")
	if err != nil {
		t.Fatalf("openai 地址拼装失败: %v", err)
	}
	if want := "https://api.example.com/v1/images/generations"; url != want {
		t.Fatalf("openai 地址错误: %s", url)
	}

	url, err = BuildImageUpstreamURL(ProtocolGemini, "https://generativelanguage.googleapis.com/v1beta", "gemini-2.5-flash-image")
	if err != nil {
		t.Fatalf("gemini 地址拼装失败: %v", err)
	}
	want := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash-image:generateContent"
	if url != want {
		t.Fatalf("gemini 地址错误: %s", url)
	}

	if _, err := BuildImageUpstreamURL(ProtocolAnthropic, "https://api.anthropic.com", "claude"); err == nil {
		t.Fatal("anthropic 渠道应报错")
	}
}

func TestBuildImageRequestOpenAI(t *testing.T) {
	req := &ImageRequest{
		Model:          "gpt-image-2",
		Prompt:         "一只在屋顶散步的橘猫",
		NegativePrompt: "模糊",
		N:              2,
		Size:           "1024x1024",
		Quality:        "high",
		ResponseFormat: ImageResponseFormatB64,
		Extra:          map[string]interface{}{"seed": 42},
	}
	body, err := BuildImageRequest(ProtocolOpenAI, req, "upstream-image-model")
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	if body["model"] != "upstream-image-model" {
		t.Fatalf("模型名应使用上游名: %v", body["model"])
	}
	if body["prompt"] != req.Prompt {
		t.Fatalf("提示词丢失: %v", body["prompt"])
	}
	if body["n"] != 2 || body["size"] != "1024x1024" || body["quality"] != "high" {
		t.Fatalf("标准字段缺失: %v", body)
	}
	if body["negative_prompt"] != "模糊" {
		t.Fatalf("负向提示词丢失: %v", body["negative_prompt"])
	}
	if body["seed"] != 42 {
		t.Fatalf("扩展参数未透传: %v", body["seed"])
	}
	if _, ok := body["response_format"]; !ok {
		t.Fatal("response_format 丢失")
	}
}

func TestBuildImageRequestGemini(t *testing.T) {
	body, err := BuildImageRequest(ProtocolGemini, &ImageRequest{Prompt: "赛博朋克城市", N: 2}, "gemini-2.5-flash-image")
	if err != nil {
		t.Fatalf("构建请求失败: %v", err)
	}
	config, ok := body["generationConfig"].(map[string]interface{})
	if !ok {
		t.Fatalf("缺少 generationConfig: %v", body)
	}
	modalities, _ := config["responseModalities"].([]string)
	if len(modalities) != 2 || modalities[0] != "TEXT" || modalities[1] != "IMAGE" {
		t.Fatalf("responseModalities 错误: %v", modalities)
	}
	if config["candidateCount"] != 2 {
		t.Fatalf("candidateCount 应为 2: %v", config["candidateCount"])
	}
	if _, ok := body["contents"]; !ok {
		t.Fatalf("缺少 contents: %v", body)
	}
}

func TestBuildImageRequestRejectsEmptyPrompt(t *testing.T) {
	if _, err := BuildImageRequest(ProtocolOpenAI, &ImageRequest{Prompt: "  "}, "m"); err == nil {
		t.Fatal("空提示词应报错")
	}
}

func TestParseOpenAIImagesWithURL(t *testing.T) {
	raw := []byte(`{
		"created": 1700000000,
		"data": [
			{"url": "https://cdn.example.com/a.png", "revised_prompt": "重写后的提示词"},
			{"url": "https://cdn.example.com/b.png"}
		],
		"usage": {"input_tokens": 12, "output_tokens": 300, "total_tokens": 312}
	}`)
	resp, err := ParseImageResponse(ProtocolOpenAI, raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(resp.Images) != 2 {
		t.Fatalf("图片数量错误: %d", len(resp.Images))
	}
	if resp.Images[0].URL != "https://cdn.example.com/a.png" {
		t.Fatalf("URL 解析错误: %v", resp.Images[0])
	}
	if resp.Images[0].RevisedPrompt != "重写后的提示词" {
		t.Fatalf("revised_prompt 丢失: %v", resp.Images[0])
	}
	if resp.Created != 1700000000 {
		t.Fatalf("created 错误: %d", resp.Created)
	}
	if resp.Usage == nil || resp.Usage.PromptTokens != 12 || resp.Usage.CompletionTokens != 300 {
		t.Fatalf("用量解析错误: %+v", resp.Usage)
	}
	if resp.Usage.TotalTokens != 312 {
		t.Fatalf("total_tokens 错误: %d", resp.Usage.TotalTokens)
	}
}

func TestParseOpenAIImagesWithB64(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(append(append([]byte{}, pngBytes...), []byte("payload")...))
	raw, err := json.Marshal(map[string]interface{}{
		"created": 1700000001,
		"data":    []map[string]interface{}{{"b64_json": encoded}},
	})
	if err != nil {
		t.Fatalf("构造响应失败: %v", err)
	}
	resp, err := ParseImageResponse(ProtocolOpenAI, raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(resp.Images) != 1 {
		t.Fatalf("图片数量错误: %d", len(resp.Images))
	}
	img := resp.Images[0]
	if img.B64Data != encoded {
		t.Fatal("base64 载荷丢失")
	}
	if img.MimeType != "image/png" {
		t.Fatalf("类型嗅探错误: %s", img.MimeType)
	}
}

func TestParseOpenAIImagesEmpty(t *testing.T) {
	if _, err := ParseImageResponse(ProtocolOpenAI, []byte(`{"data":[]}`)); err == nil {
		t.Fatal("空结果应报错")
	}
}

func TestParseGeminiImages(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(append(append([]byte{}, pngBytes...), []byte("payload")...))
	raw, err := json.Marshal(map[string]interface{}{
		"modelVersion": "gemini-2.5-flash-image",
		"candidates": []map[string]interface{}{{
			"index": 0,
			"content": map[string]interface{}{
				"role": "model",
				"parts": []map[string]interface{}{
					{"text": "这是生成的图片："},
					{"inlineData": map[string]interface{}{"mimeType": "image/png", "data": encoded}},
				},
			},
		}},
	})
	if err != nil {
		t.Fatalf("构造响应失败: %v", err)
	}
	resp, err := ParseImageResponse(ProtocolGemini, raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(resp.Images) != 1 {
		t.Fatalf("图片数量错误: %d", len(resp.Images))
	}
	if resp.Images[0].B64Data != encoded {
		t.Fatal("inlineData 未提取")
	}
}

func TestParseGeminiImagesOnlyText(t *testing.T) {
	raw := []byte(`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"我无法生成图片"}]}}]}`)
	_, err := ParseImageResponse(ProtocolGemini, raw)
	if err == nil {
		t.Fatal("纯文本响应应报错")
	}
	if !strings.Contains(err.Error(), "我无法生成图片") {
		t.Fatalf("错误信息应带出上游文本: %v", err)
	}
}

func TestParseImageClientRequestKeepsExtra(t *testing.T) {
	raw := []byte(`{
		"model": "gpt-image-2",
		"prompt": "test",
		"n": 1,
		"size": "1024x1024",
		"watermark_enabled": false,
		"seed": 7
	}`)
	req, err := ParseImageClientRequest(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if req.Model != "gpt-image-2" || req.Size != "1024x1024" || req.N != 1 {
		t.Fatalf("标准字段解析错误: %+v", req)
	}
	if req.Extra["seed"] != float64(7) {
		t.Fatalf("Extra 应保留未知字段: %+v", req.Extra)
	}
	if _, ok := req.Extra["size"]; ok {
		t.Fatal("Extra 不应包含已标准化字段")
	}
}

func TestBuildImageClientResponse(t *testing.T) {
	out := BuildImageClientResponse(&ImageResponse{
		Created: 1700000000,
		Images: []Image{
			{URL: "https://cdn.example.com/a.png"},
			{B64Data: "AAAA", MimeType: "image/png"},
		},
	})
	data, ok := out["data"].([]map[string]interface{})
	if !ok || len(data) != 2 {
		t.Fatalf("data 结构错误: %v", out["data"])
	}
	if data[0]["url"] != "https://cdn.example.com/a.png" {
		t.Fatalf("URL 项错误: %v", data[0])
	}
	if data[1]["b64_json"] != "AAAA" {
		t.Fatalf("b64 项错误: %v", data[1])
	}
	if _, exists := data[0]["b64_json"]; exists {
		t.Fatal("URL 项不应输出 b64_json")
	}
}

func TestBuildImageChatRequest(t *testing.T) {
	body, err := BuildImageChatRequest(&ImageRequest{
		Prompt:         "一只橘猫",
		NegativePrompt: "模糊",
		Extra:          map[string]interface{}{"seed": 7},
	}, "gemini-3.1-flash-image")
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if body["model"] != "gemini-3.1-flash-image" {
		t.Fatalf("模型名错误: %v", body["model"])
	}
	modalities, _ := body["modalities"].([]string)
	if len(modalities) != 2 || modalities[0] != "image" {
		t.Fatalf("modalities 应声明图片输出: %v", modalities)
	}
	messages, _ := body["messages"].([]map[string]interface{})
	if len(messages) != 1 {
		t.Fatalf("messages 结构错误: %v", body["messages"])
	}
	// 负向提示词没有标准字段，应拼进正文。
	content, _ := messages[0]["content"].(string)
	if !strings.Contains(content, "一只橘猫") || !strings.Contains(content, "模糊") {
		t.Fatalf("正文应包含正向与负向提示词: %q", content)
	}
	if body["seed"] != 7 {
		t.Fatalf("扩展参数未透传: %v", body["seed"])
	}
	if _, ok := body["n"]; ok {
		t.Fatal("chat 生图不应传 n，多数平台不支持")
	}
}

func TestBuildImageChatRequestRejectsEmptyPrompt(t *testing.T) {
	if _, err := BuildImageChatRequest(&ImageRequest{Prompt: " "}, "m"); err == nil {
		t.Fatal("空提示词应报错")
	}
}

func TestParseImageChatResponse(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(append(append([]byte{}, pngBytes...), []byte("payload")...))
	raw, err := json.Marshal(map[string]interface{}{
		"created": 1700000000,
		"model":   "gemini-3.1-flash-image",
		"choices": []map[string]interface{}{{
			"index": 0,
			"message": map[string]interface{}{
				"role":    "assistant",
				"content": "",
				"images": []map[string]interface{}{{
					"type":      "image_url",
					"image_url": map[string]interface{}{"url": "data:image/png;base64," + encoded},
				}},
			},
		}},
		"usage": map[string]interface{}{"prompt_tokens": 9, "completion_tokens": 1559, "total_tokens": 1568},
	})
	if err != nil {
		t.Fatalf("构造响应失败: %v", err)
	}
	resp, err := ParseImageChatResponse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(resp.Images) != 1 {
		t.Fatalf("图片数量错误: %d", len(resp.Images))
	}
	if resp.Images[0].B64Data != encoded {
		t.Fatal("data URI 未解出 base64")
	}
	if resp.Usage == nil || resp.Usage.CompletionTokens != 1559 {
		t.Fatalf("用量解析错误: %+v", resp.Usage)
	}
}

func TestParseImageChatResponseOnlyText(t *testing.T) {
	raw := []byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"该模型不支持出图"}}]}`)
	_, err := ParseImageChatResponse(raw)
	if err == nil {
		t.Fatal("纯文本响应应报错")
	}
	if !strings.Contains(err.Error(), "该模型不支持出图") {
		t.Fatalf("错误信息应带出上游文本: %v", err)
	}
}

func TestModalitiesParamMapping(t *testing.T) {
	if NormalizeParamName("modality") != ParamModalities {
		t.Fatalf("modality 应归一为 %s", ParamModalities)
	}
	target, ok := openAIParamTargets[ParamModalities]
	if !ok {
		t.Fatal("modalities 应写入 OpenAI 协议字段")
	}
	if len(target.Path) != 1 || target.Path[0] != "modalities" {
		t.Fatalf("字段路径错误: %v", target.Path)
	}
	supported := SupportedParams(ProtocolOpenAI, nil)
	if _, ok := supported[ParamModalities]; !ok {
		t.Fatal("modalities 应在 OpenAI 默认支持参数集中")
	}
}
