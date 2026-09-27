package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

// 生图场景：上游把结果放在 message.images[]（OpenRouter 风格，data URI）。
func TestParseResponseWithImages(t *testing.T) {
	raw := []byte(`{
      "id":"chatcmpl-img","object":"chat.completion","created":1,"model":"gpt-4o-image",
      "choices":[{"index":0,"message":{"role":"assistant","content":"",
        "images":[{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo="}}]},
        "finish_reason":"stop"}]
    }`)
	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	images := resp.Choices[0].Message.Images
	if len(images) != 1 {
		t.Fatalf("应解析出 1 张图片，实际 %d", len(images))
	}
	if images[0].B64Data != "iVBORw0KGgo=" {
		t.Fatalf("base64 载荷错误: %q", images[0].B64Data)
	}
	if images[0].MimeType != "image/png" {
		t.Fatalf("MIME 解析错误: %q", images[0].MimeType)
	}
	if got := images[0].DataURI(); got != "data:image/png;base64,iVBORw0KGgo=" {
		t.Fatalf("data URI 还原错误: %q", got)
	}
}

// 图片也可能埋在 content 的 image_url 片段里。
func TestParseResponseImagesFromContentParts(t *testing.T) {
	raw := []byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":[
      {"type":"text","text":"已生成"},
      {"type":"image_url","image_url":{"url":"data:image/jpeg;base64,AAAABBBB"}}
    ]}}]}`)
	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.Text() != "已生成" {
		t.Fatalf("文本提取错误: %q", resp.Text())
	}
	if len(resp.Choices[0].Message.Images) != 1 ||
		resp.Choices[0].Message.Images[0].MimeType != "image/jpeg" {
		t.Fatalf("图片提取错误: %+v", resp.Choices[0].Message.Images)
	}
}

// OpenAI 兼容输出侧保留 message.images[] 扩展字段。
func TestBuildResponseKeepsImages(t *testing.T) {
	body := BuildResponse(&spec.UnifiedResponse{
		ID:    "chatcmpl-1",
		Model: "gpt-4o-image",
		Choices: []spec.Choice{{
			Index: 0,
			Message: spec.Message{
				Role:   spec.RoleAssistant,
				Images: []spec.Image{{Type: "image_url", MimeType: "image/webp", B64Data: "UklGRg=="}},
			},
		}},
	})
	data, _ := json.Marshal(body)
	if !strings.Contains(string(data), `"images"`) {
		t.Fatalf("输出缺少 images 字段: %s", data)
	}
	var back Response
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if len(back.Choices[0].Message.Images) != 1 ||
		back.Choices[0].Message.Images[0].ImageURL.URL != "data:image/webp;base64,UklGRg==" {
		t.Fatalf("images 透传错误: %+v", back.Choices[0].Message.Images)
	}
}

func TestStreamImagesRoundTrip(t *testing.T) {
	event, err := BuildStreamEvent(&spec.StreamChunk{
		ID:    "chunk-1",
		Model: "gpt-4o-image",
		Choices: []spec.StreamChoice{{
			Index: 0,
			Delta: spec.Delta{Images: []spec.Image{{MimeType: "image/png", B64Data: "iVBORw0KGgo="}}},
		}},
	})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	payload := strings.TrimPrefix(strings.TrimSpace(string(event)), "data: ")
	chunk, err := ParseStreamEvent([]byte(payload))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(chunk.Choices[0].Delta.Images) != 1 ||
		chunk.Choices[0].Delta.Images[0].B64Data != "iVBORw0KGgo=" {
		t.Fatalf("流式图片往返不一致: %+v", chunk.Choices[0].Delta)
	}
	if chunk.IsEmpty() {
		t.Fatal("含图片的块不应判定为空")
	}
}

func TestParseResponseReasoningTokens(t *testing.T) {
	raw := []byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],
      "usage":{"prompt_tokens":10,"completion_tokens":30,"total_tokens":40,
      "completion_tokens_details":{"reasoning_tokens":18}}}`)
	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.Usage.ReasoningTokens != 18 {
		t.Fatalf("reasoning_tokens 解析错误: %d", resp.Usage.ReasoningTokens)
	}
	if resp.Usage.CompletionTokens != 30 {
		t.Fatalf("思维链应计入 completion，不应被拆分: %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 40 {
		t.Fatalf("total 错误: %d", resp.Usage.TotalTokens)
	}
}

func TestBuildResponseReasoningDetails(t *testing.T) {
	body := BuildResponse(&spec.UnifiedResponse{
		Model: "o1",
		Usage: &spec.Usage{PromptTokens: 5, CompletionTokens: 20, ReasoningTokens: 12, TotalTokens: 25},
	})
	data, _ := json.Marshal(body)
	if !strings.Contains(string(data), `"reasoning_tokens":12`) {
		t.Fatalf("输出缺少 reasoning_tokens: %s", data)
	}
}

// 上游只报思维链、漏报 total 时应自动补齐。
func TestConvertUsageNormalizesTotals(t *testing.T) {
	u := convertUsage(&Usage{
		PromptTokens:            7,
		CompletionTokens:        0,
		TotalTokens:             0,
		CompletionTokensDetails: &CompletionTokensDetails{ReasoningTokens: 9},
	})
	if u.TotalTokens != 16 {
		t.Fatalf("total 应按 prompt+completion 推导，实际 %d", u.TotalTokens)
	}
	if u.CompletionTokens != 9 {
		t.Fatalf("completion 不应小于 reasoning，实际 %d", u.CompletionTokens)
	}
}

func TestBuildRequestInputImages(t *testing.T) {
	body, err := BuildRequest(&spec.UnifiedRequest{
		Model: "gpt-4o",
		Messages: []spec.Message{{
			Role:   spec.RoleUser,
			Images: []spec.Image{{MimeType: "image/png", B64Data: "iVBORw0KGgo="}},
		}},
	})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	data, _ := json.Marshal(body)
	if !strings.Contains(string(data), `"image_url"`) {
		t.Fatalf("输入图片未转为 image_url 片段: %s", data)
	}
}

func TestParseDataURINonBase64(t *testing.T) {
	img, ok := spec.ParseDataURI("data:text/plain,hello")
	if !ok {
		t.Fatal("data URI 应解析成功")
	}
	if img.B64Data != "" || img.URL == "" {
		t.Fatalf("非 base64 应退化为 URL 透传: %+v", img)
	}
	if _, ok := spec.ParseDataURI("https://example.com/a.png"); ok {
		t.Fatal("普通 URL 不应被当作 data URI")
	}
}
