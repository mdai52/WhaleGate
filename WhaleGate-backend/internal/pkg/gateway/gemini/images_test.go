package gemini

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

// Gemini 侧生图结果放在 content.parts[].inlineData。
func TestParseResponseInlineData(t *testing.T) {
	raw := []byte(`{
      "candidates":[{"content":{"role":"model","parts":[
        {"text":"已生成"},
        {"inlineData":{"mimeType":"image/png","data":"iVBORw0KGgo="}}
      ]},"finishReason":"STOP","index":0}],
      "modelVersion":"gemini-image"
    }`)
	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.Text() != "已生成" {
		t.Fatalf("文本错误: %q", resp.Text())
	}
	images := resp.Choices[0].Message.Images
	if len(images) != 1 || images[0].B64Data != "iVBORw0KGgo=" || images[0].MimeType != "image/png" {
		t.Fatalf("inlineData 归一错误: %+v", images)
	}
	if images[0].Type != "inline_data" {
		t.Fatalf("类型错误: %s", images[0].Type)
	}
}

// 反向：统一格式输出为 Gemini 时应还原为 inlineData parts。
func TestBuildResponseInlineData(t *testing.T) {
	body := BuildResponse(&spec.UnifiedResponse{
		Model: "gemini-image",
		Choices: []spec.Choice{{
			Index:   0,
			Message: spec.Message{Role: spec.RoleAssistant, Content: "ok", Images: []spec.Image{{MimeType: "image/webp", B64Data: "UklGRg=="}}},
		}},
	})
	data, _ := json.Marshal(body)
	if !strings.Contains(string(data), `"inlineData"`) {
		t.Fatalf("输出缺少 inlineData: %s", data)
	}
	if strings.Contains(string(data), `"images"`) {
		t.Fatalf("Gemini 协议不应输出 images 扩展字段: %s", data)
	}

	var back Response
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	parts := back.Candidates[0].Content.Parts
	if len(parts) != 2 || parts[1].InlineData == nil || parts[1].InlineData.Data != "UklGRg==" {
		t.Fatalf("inlineData 还原错误: %+v", parts)
	}
}

func TestBuildRequestImages(t *testing.T) {
	body, err := BuildRequest(&spec.UnifiedRequest{
		Model: "gemini-image",
		Messages: []spec.Message{{
			Role:    spec.RoleUser,
			Content: "画一只鲸鱼",
			Images:  []spec.Image{{MimeType: "image/png", B64Data: "iVBORw0KGgo="}},
		}},
	})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	data, _ := json.Marshal(body)
	if !strings.Contains(string(data), `"inlineData"`) {
		t.Fatalf("输入图片未转为 inlineData: %s", data)
	}
}

func TestStreamInlineDataRoundTrip(t *testing.T) {
	event, err := BuildStreamEvent(&spec.StreamChunk{
		Model: "gemini-image",
		Choices: []spec.StreamChoice{{
			Index: 0,
			Delta: spec.Delta{Images: []spec.Image{{MimeType: "image/png", B64Data: "AAAA"}}},
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
	if len(chunk.Choices[0].Delta.Images) != 1 || chunk.Choices[0].Delta.Images[0].B64Data != "AAAA" {
		t.Fatalf("流式图片往返不一致: %+v", chunk.Choices[0].Delta)
	}
	if chunk.IsEmpty() {
		t.Fatal("含图片的块不应判定为空")
	}
}

// 思维链：Gemini 用 thoughtsTokenCount 表达。
func TestParseResponseThoughtsTokenCount(t *testing.T) {
	raw := []byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"index":0}],
      "usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":21,"totalTokenCount":30,"thoughtsTokenCount":15}}`)
	resp, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.Usage.ReasoningTokens != 15 {
		t.Fatalf("thoughtsTokenCount 映射错误: %d", resp.Usage.ReasoningTokens)
	}
	if resp.Usage.CompletionTokens != 21 || resp.Usage.TotalTokens != 30 {
		t.Fatalf("用量错误: %+v", resp.Usage)
	}
}

func TestConvertUsageThoughtsOnly(t *testing.T) {
	u := convertUsage(&UsageMetadata{PromptTokenCount: 4, CandidatesTokenCount: 0, TotalTokenCount: 0, ThoughtsTokenCount: 11})
	if u.CompletionTokens != 11 {
		t.Fatalf("仅思维链时应补全 completion: %d", u.CompletionTokens)
	}
	if u.TotalTokens != 15 {
		t.Fatalf("total 推导错误: %d", u.TotalTokens)
	}
}

// 从 OpenAI 协议进、Gemini 渠道出时，图片应从 images 扩展转为 inlineData。
func TestCrossProtocolImageConversion(t *testing.T) {
	openaiLike := &spec.UnifiedRequest{
		Model: "gemini-image",
		Messages: []spec.Message{{
			Role:   spec.RoleAssistant,
			Images: []spec.Image{{Type: "image_url", MimeType: "image/png", B64Data: "iVBORw0KGgo="}},
		}},
	}
	body, err := BuildRequest(openaiLike)
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	data, _ := json.Marshal(body)
	if strings.Contains(string(data), `"images"`) {
		t.Fatalf("Gemini 上游请求不应携带 images: %s", data)
	}
	if !strings.Contains(string(data), `"inlineData"`) {
		t.Fatalf("应转换为 inlineData: %s", data)
	}
}
