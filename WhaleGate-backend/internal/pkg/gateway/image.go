// Package gateway 中的文生图子系统：把统一的图片生成请求转成上游协议，
// 再把上游返回的图片归一为统一的 ImageResponse。
//
// 注意：图片生成是同步一次性返回，没有流式与工具调用，因此不经过
// UnifiedRequest/UnifiedResponse，而是独立的一对小类型（ImageRequest/ImageResponse）。
package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/whalegate/whalegate/internal/pkg/gateway/gemini"
	"github.com/whalegate/whalegate/internal/pkg/gateway/openai"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

// ImageResponseFormatURL / ImageResponseFormatB64 上游返回格式。
const (
	ImageResponseFormatURL = "url"
	ImageResponseFormatB64 = "b64_json"
)

// SupportsImageGeneration 判断渠道协议是否具备原生图片生成能力。
// Anthropic Messages API 不提供图片生成，明确报错比让上游返回 404 更易排查。
func SupportsImageGeneration(channelType string) bool {
	return channelType == ProtocolOpenAI || channelType == ProtocolGemini
}

// imageModelKeywords 生图模型名关键词。各家命名差异很大，且渠道登记的模型列表
// 未必带能力标记，因此用关键词做兜底识别。
var imageModelKeywords = []string{
	"dall-e", "gpt-image", "image", "imagen", "flux", "stable-diffusion",
	"sd3", "sd-xl", "seedream", "wanx", "kolors", "qwen-image", "cogview",
	"recraft", "ideogram", "midjourney", "nano-banana",
}

// IsImageModel 判断模型名是否指向图片生成模型。
func IsImageModel(id string) bool {
	name := strings.ToLower(strings.TrimSpace(id))
	if name == "" {
		return false
	}
	for _, keyword := range imageModelKeywords {
		if strings.Contains(name, keyword) {
			return true
		}
	}
	return false
}

// BuildImageUpstreamURL 按渠道协议拼装图片生成端点。
func BuildImageUpstreamURL(channelType, baseURL, model string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "", fmt.Errorf("渠道未配置上游地址")
	}
	switch channelType {
	case ProtocolOpenAI:
		return base + "/images/generations", nil
	case ProtocolGemini:
		return fmt.Sprintf("%s/models/%s:%s", base, model, gemini.ActionGenerateContent), nil
	default:
		return "", fmt.Errorf("渠道协议 %s 不支持图片生成", channelType)
	}
}

// BuildImageRequest 把统一的图片请求转换为上游请求体，model 为映射后的上游模型名。
func BuildImageRequest(channelType string, req *spec.ImageRequest, model string) (map[string]interface{}, error) {
	if req == nil {
		return nil, fmt.Errorf("请求为空")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("prompt 不能为空")
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("缺少 model")
	}
	switch channelType {
	case ProtocolOpenAI:
		return buildOpenAIImageRequest(req, model), nil
	case ProtocolGemini:
		return buildGeminiImageRequest(req, model), nil
	default:
		return nil, fmt.Errorf("渠道协议 %s 不支持图片生成", channelType)
	}
}

// buildOpenAIImageRequest 构造 OpenAI 兼容的 /images/generations 请求体。
func buildOpenAIImageRequest(req *spec.ImageRequest, model string) map[string]interface{} {
	// 扩展参数先铺底，标准字段随后覆盖，保证语义明确。
	body := make(map[string]interface{}, len(req.Extra)+8)
	for k, v := range req.Extra {
		if v == nil {
			continue
		}
		body[k] = v
	}
	body["model"] = model
	body["prompt"] = req.Prompt
	if req.NegativePrompt != "" {
		body["negative_prompt"] = req.NegativePrompt
	}
	if req.N > 0 {
		body["n"] = req.N
	}
	if req.Size != "" {
		body["size"] = req.Size
	}
	if req.Quality != "" {
		body["quality"] = req.Quality
	}
	if req.Style != "" {
		body["style"] = req.Style
	}
	if req.ResponseFormat != "" {
		body["response_format"] = req.ResponseFormat
	}
	if req.User != "" {
		body["user"] = req.User
	}
	return body
}

// buildGeminiImageRequest 构造 Gemini 原生的 generateContent 请求体。
// Gemini 通过 responseModalities 声明需要图片输出。
func buildGeminiImageRequest(req *spec.ImageRequest, model string) map[string]interface{} {
	generationConfig := map[string]interface{}{
		"responseModalities": []string{"TEXT", "IMAGE"},
	}
	if req.N > 1 {
		generationConfig["candidateCount"] = req.N
	}
	body := map[string]interface{}{
		"contents": []map[string]interface{}{{
			"role":  "user",
			"parts": []map[string]interface{}{{"text": req.Prompt}},
		}},
		"generationConfig": generationConfig,
	}
	// 扩展参数透传（如 imageConfig.aspectRatio、safetySettings），已有键不覆盖。
	for k, v := range req.Extra {
		if v == nil {
			continue
		}
		if _, exists := body[k]; exists {
			continue
		}
		body[k] = v
	}
	return body
}

// ParseImageResponse 解析上游图片生成响应。
func ParseImageResponse(channelType string, raw []byte) (*spec.ImageResponse, error) {
	switch channelType {
	case ProtocolOpenAI:
		return parseOpenAIImages(raw)
	case ProtocolGemini:
		return parseGeminiImages(raw)
	default:
		return nil, fmt.Errorf("渠道协议 %s 不支持图片生成", channelType)
	}
}

// parseOpenAIImages 解析 OpenAI 兼容的 images 响应。
func parseOpenAIImages(raw []byte) (*spec.ImageResponse, error) {
	var resp struct {
		Created int64  `json:"created"`
		Model   string `json:"model"`
		Data    []struct {
			URL           string `json:"url"`
			B64JSON       string `json:"b64_json"`
			RevisedPrompt string `json:"revised_prompt"`
		} `json:"data"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
			// 部分上游沿用 chat 的字段名。
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("解析图片响应失败: %w", err)
	}

	out := &spec.ImageResponse{
		Created: resp.Created,
		Model:   resp.Model,
		Images:  make([]spec.Image, 0, len(resp.Data)),
	}
	if out.Created == 0 {
		out.Created = time.Now().Unix()
	}
	for _, item := range resp.Data {
		img := spec.Image{RevisedPrompt: item.RevisedPrompt}
		switch {
		case item.B64JSON != "":
			img.Type = "inline_data"
			img.B64Data = item.B64JSON
			img.MimeType = detectImageMime(item.B64JSON)
		case item.URL != "":
			img.Type = "image_url"
			img.URL = item.URL
		default:
			continue
		}
		out.Images = append(out.Images, img)
	}
	if len(out.Images) == 0 {
		return nil, fmt.Errorf("上游未返回图片")
	}
	if resp.Usage != nil {
		usage := &spec.Usage{
			PromptTokens:     firstNonZero(resp.Usage.InputTokens, resp.Usage.PromptTokens),
			CompletionTokens: firstNonZero(resp.Usage.OutputTokens, resp.Usage.CompletionTokens),
			TotalTokens:      resp.Usage.TotalTokens,
		}
		usage.Normalize()
		out.Usage = usage
	}
	return out, nil
}

// parseGeminiImages 从 Gemini 的 generateContent 响应中提取 inlineData 图片。
func parseGeminiImages(raw []byte) (*spec.ImageResponse, error) {
	unified, err := gemini.ParseResponse(raw)
	if err != nil {
		return nil, err
	}
	out := &spec.ImageResponse{
		Created: time.Now().Unix(),
		Model:   unified.Model,
		Usage:   unified.Usage,
	}
	for _, choice := range unified.Choices {
		out.Images = append(out.Images, choice.Message.Images...)
	}
	if len(out.Images) == 0 {
		// 上游可能返回纯文本说明（例如模型不支持出图），把原文带出来便于排查。
		if text := strings.TrimSpace(unified.Text()); text != "" {
			return nil, fmt.Errorf("上游未返回图片: %s", truncateRunes(text, 200))
		}
		return nil, fmt.Errorf("上游未返回图片")
	}
	return out, nil
}

// BuildImageChatRequest 把生图请求包装成 OpenAI 兼容的 chat 请求体。
//
// 不少聚合平台并不提供 /images/generations，而是通过 chat 接口的 modalities
// 参数返回图片（OpenRouter 风格，结果在 message.images[]）。此时只能用这条路径。
func BuildImageChatRequest(req *spec.ImageRequest, model string) (map[string]interface{}, error) {
	if req == nil {
		return nil, fmt.Errorf("请求为空")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("prompt 不能为空")
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("缺少 model")
	}
	body := map[string]interface{}{
		"model":  model,
		"stream": false,
		"messages": []map[string]interface{}{
			{"role": "user", "content": imageChatPrompt(req)},
		},
		// 声明需要图片与文本输出；chat 接口凭此切到生图模式。
		"modalities": []string{"image", "text"},
	}
	// 尺寸、质量等标准字段多数平台在 chat 生图下不认，交给 Extra 显式透传。
	for k, v := range req.Extra {
		if v == nil {
			continue
		}
		body[k] = v
	}
	return body, nil
}

// imageChatPrompt 把提示词与负向提示词拼成 chat 生图用的文本。
// chat 接口没有 negative_prompt 字段，只能写进正文。
func imageChatPrompt(req *spec.ImageRequest) string {
	prompt := strings.TrimSpace(req.Prompt)
	if negative := strings.TrimSpace(req.NegativePrompt); negative != "" {
		prompt += "\n\n画面中不要出现以下元素：" + negative
	}
	return prompt
}

// ParseImageChatResponse 从 OpenAI 兼容的 chat 响应中提取图片。
func ParseImageChatResponse(raw []byte) (*spec.ImageResponse, error) {
	unified, err := openai.ParseResponse(raw)
	if err != nil {
		return nil, err
	}
	out := &spec.ImageResponse{
		Created: unified.Created,
		Model:   unified.Model,
		Usage:   unified.Usage,
	}
	if out.Created == 0 {
		out.Created = time.Now().Unix()
	}
	for _, choice := range unified.Choices {
		out.Images = append(out.Images, choice.Message.Images...)
	}
	if len(out.Images) == 0 {
		// 上游可能只返回文字说明（模型不支持出图、提示词被拒等），把原文带出来便于排查。
		if text := strings.TrimSpace(unified.Text()); text != "" {
			return nil, fmt.Errorf("上游未返回图片: %s", truncateRunes(text, 200))
		}
		return nil, fmt.Errorf("上游未返回图片")
	}
	return out, nil
}

// ---------------------------------------------------------------- 客户端侧

// knownImageFields 请求体中已被标准化的字段，其余字段统一收进 Extra。
var knownImageFields = map[string]struct{}{
	"model": {}, "prompt": {}, "negative_prompt": {}, "n": {}, "size": {},
	"quality": {}, "style": {}, "response_format": {}, "user": {},
}

// ParseImageClientRequest 解析 OpenAI 兼容的图片生成请求体。
func ParseImageClientRequest(raw []byte) (*spec.ImageRequest, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("解析图片请求失败: %w", err)
	}
	req := &spec.ImageRequest{
		Model:          imageStringField(payload, "model"),
		Prompt:         imageStringField(payload, "prompt"),
		NegativePrompt: imageStringField(payload, "negative_prompt"),
		N:              imageIntField(payload, "n"),
		Size:           imageStringField(payload, "size"),
		Quality:        imageStringField(payload, "quality"),
		Style:          imageStringField(payload, "style"),
		ResponseFormat: imageStringField(payload, "response_format"),
		User:           imageStringField(payload, "user"),
	}
	for key, value := range payload {
		if _, ok := knownImageFields[key]; ok {
			continue
		}
		if req.Extra == nil {
			req.Extra = make(map[string]interface{})
		}
		req.Extra[key] = value
	}
	return req, nil
}

// BuildImageClientResponse 把统一图片结果转回 OpenAI 兼容响应体。
func BuildImageClientResponse(resp *spec.ImageResponse) map[string]interface{} {
	data := make([]map[string]interface{}, 0, len(resp.Images))
	for _, img := range resp.Images {
		item := make(map[string]interface{}, 2)
		if img.B64Data != "" {
			item["b64_json"] = img.B64Data
		} else {
			item["url"] = img.URL
		}
		if img.RevisedPrompt != "" {
			item["revised_prompt"] = img.RevisedPrompt
		}
		data = append(data, item)
	}
	out := map[string]interface{}{
		"created": resp.Created,
		"data":    data,
	}
	if resp.Usage != nil {
		out["usage"] = resp.Usage
	}
	return out
}

func imageStringField(payload map[string]interface{}, key string) string {
	value, _ := payload[key].(string)
	return value
}

func imageIntField(payload map[string]interface{}, key string) int {
	switch value := payload[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}

// ---------------------------------------------------------------- 工具

// detectImageMime 依据 base64 头部字节推断图片类型，避免让上游把
// 内容类型塞进 payload 或额外回一次请求。
func detectImageMime(b64 string) string {
	const sniffLen = 64
	head := b64
	if len(head) > sniffLen {
		head = head[:sniffLen]
	}
	raw, err := base64.StdEncoding.DecodeString(head)
	if err != nil || len(raw) == 0 {
		return "image/png"
	}
	mime := http.DetectContentType(raw)
	if strings.HasPrefix(mime, "image/") {
		return mime
	}
	return "image/png"
}

func firstNonZero(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
