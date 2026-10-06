// Package gateway 中的视频生成子系统：把统一的视频请求转成上游协议，
// 再把上游的异步任务响应归一为统一的 VideoResponse。
//
// 与图片不同，视频生成几乎都是异步的：提交拿到任务 ID → 轮询状态 → 取内容。
// 因此这里提供三对函数（提交 / 查询 / 取内容），由 service 层串联成任务流程。
package gateway

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

// videoModelKeywords 视频生成模型名关键词，用于从模型目录里识别可用模型。
var videoModelKeywords = []string{
	"sora", "veo", "kling", "hailuo", "wan", "wanx", "runway", "luma",
	"pixverse", "seedance", "vidu", "cogvideo", "hunyuan-video", "animate",
	"video", "gen-3", "gen-4", "ray", "dreamina",
}

// SupportsVideoGeneration 判断渠道协议是否具备视频生成能力。
//
// 目前只认 OpenAI 兼容的 /v1/videos（Sora 风格）——各家聚合平台基本都按这个形态
// 提供视频生成。Gemini 的 Veo 需要 predictLongRunning 这套独立接口，Anthropic 不提供，
// 明确报错比让上游返回 404 更易排查。
func SupportsVideoGeneration(channelType string) bool {
	return channelType == ProtocolOpenAI
}

// IsVideoModel 判断模型名是否指向视频生成模型。
func IsVideoModel(id string) bool {
	name := strings.ToLower(strings.TrimSpace(id))
	if name == "" {
		return false
	}
	for _, keyword := range videoModelKeywords {
		if strings.Contains(name, keyword) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- 端点

// BuildVideoSubmitURL 提交生成的端点。
func BuildVideoSubmitURL(channelType, baseURL string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "", fmt.Errorf("渠道未配置上游地址")
	}
	if !SupportsVideoGeneration(channelType) {
		return "", fmt.Errorf("渠道协议 %s 不支持视频生成", channelType)
	}
	return base + "/videos", nil
}

// BuildVideoStatusURL 查询任务状态的端点。
func BuildVideoStatusURL(channelType, baseURL, taskID string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return "", fmt.Errorf("渠道未配置上游地址")
	}
	if !SupportsVideoGeneration(channelType) {
		return "", fmt.Errorf("渠道协议 %s 不支持视频生成", channelType)
	}
	if strings.TrimSpace(taskID) == "" {
		return "", fmt.Errorf("缺少上游任务 ID")
	}
	return base + "/videos/" + taskID, nil
}

// BuildVideoContentURL 拉取生成内容的端点。
func BuildVideoContentURL(channelType, baseURL, taskID string) (string, error) {
	url, err := BuildVideoStatusURL(channelType, baseURL, taskID)
	if err != nil {
		return "", err
	}
	return url + "/content", nil
}

// ---------------------------------------------------------------- 请求 / 响应

// BuildVideoRequest 把统一视频请求转换为上游请求体，model 为映射后的上游模型名。
func BuildVideoRequest(channelType string, req *spec.VideoRequest, model string) (map[string]interface{}, error) {
	if req == nil {
		return nil, fmt.Errorf("请求为空")
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, fmt.Errorf("prompt 不能为空")
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("缺少 model")
	}
	if !SupportsVideoGeneration(channelType) {
		return nil, fmt.Errorf("渠道协议 %s 不支持视频生成", channelType)
	}
	return buildOpenAIVideoRequest(req, model), nil
}

// buildOpenAIVideoRequest 构造 OpenAI 兼容的 /videos 请求体（Sora 风格）。
func buildOpenAIVideoRequest(req *spec.VideoRequest, model string) map[string]interface{} {
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
	if req.Seconds > 0 {
		body["seconds"] = req.Seconds
	}
	if req.Size != "" {
		body["size"] = req.Size
	}
	if req.AspectRatio != "" {
		body["aspect_ratio"] = req.AspectRatio
	}
	if req.FPS > 0 {
		body["fps"] = req.FPS
	}
	if req.Seed != nil {
		body["seed"] = *req.Seed
	}
	if req.Image != "" {
		body["image"] = req.Image
	}
	if req.User != "" {
		body["user"] = req.User
	}
	return body
}

// ParseVideoSubmitResponse 解析提交响应：拿到任务 ID，也可能直接带出结果。
func ParseVideoSubmitResponse(channelType string, raw []byte) (*spec.VideoResponse, error) {
	if !SupportsVideoGeneration(channelType) {
		return nil, fmt.Errorf("渠道协议 %s 不支持视频生成", channelType)
	}
	out, err := parseOpenAIVideoTask(raw)
	if err != nil {
		return nil, err
	}
	// 上游未显式给状态时视为已排队：多数平台提交后立即返回 queued。
	if out.Status == "" {
		out.Status = spec.VideoStatusQueued
	}
	return out, nil
}

// ParseVideoStatusResponse 解析轮询响应。
func ParseVideoStatusResponse(channelType string, raw []byte) (*spec.VideoResponse, error) {
	if !SupportsVideoGeneration(channelType) {
		return nil, fmt.Errorf("渠道协议 %s 不支持视频生成", channelType)
	}
	out, err := parseOpenAIVideoTask(raw)
	if err != nil {
		return nil, err
	}
	if out.Status == "" {
		// 已完成但没写状态时，用是否有内容反推，避免任务永远停在原地。
		if len(out.Videos) > 0 {
			out.Status = spec.VideoStatusSucceeded
		} else {
			out.Status = spec.VideoStatusProcessing
		}
	}
	return out, nil
}

// parseOpenAIVideoTask 解析 OpenAI 风格的任务对象，上游字段命名差异较大，
// 这里对常见同义字段做兼容。
func parseOpenAIVideoTask(raw []byte) (*spec.VideoResponse, error) {
	var resp struct {
		ID       string `json:"id"`
		Object   string `json:"object"`
		Status   string `json:"status"`
		Progress int    `json:"progress"`
		// Sora 允许把 seconds 写成字符串（"4"/"8"），也可能是数字，
		// 因此用 json.Number 承接再统一转换，避免直接解析成 int 报错。
		Seconds json.Number `json:"seconds"`
		Model   string      `json:"model"`
		Error   struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
		// 部分平台把时长写成字符串（Sora 的 seconds 允许 "4"/"8"）。
		SecondsText string `json:"seconds_text"`
		URL         string `json:"url"`
		VideoURL    string `json:"video_url"`
		DownloadURL string `json:"download_url"`
		B64JSON     string `json:"b64_json"`
		Data        []struct {
			URL       string `json:"url"`
			B64JSON   string `json:"b64_json"`
			MimeType  string `json:"mime_type"`
			Duration  int    `json:"duration"`
			Seconds   int    `json:"seconds"`
			RevisedPt string `json:"revised_prompt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("解析视频任务响应失败: %w", err)
	}

	out := &spec.VideoResponse{
		TaskID:   firstVideoID(resp.ID, resp.Object),
		Status:   normalizeVideoStatus(resp.Status),
		Progress: resp.Progress,
		Seconds:  firstNonZero(secondsValue(resp.Seconds), parseIntOrZero(resp.SecondsText)),
	}
	if resp.Error.Message != "" {
		out.Error = resp.Error.Message
		if resp.Error.Code != "" {
			out.Error = resp.Error.Code + ": " + out.Error
		}
		// 上游显式报错时状态未必写 failed，这里补上，避免继续空轮询。
		out.Status = spec.VideoStatusFailed
	}

	// 直接带出结果的情况：单条字段或 data 数组。
	if resp.B64JSON != "" {
		out.Videos = append(out.Videos, spec.Video{Type: "inline_data", B64Data: resp.B64JSON, MimeType: "video/mp4", DurationSec: out.Seconds})
	} else if url := firstVideoURL(resp.URL, resp.VideoURL, resp.DownloadURL); url != "" {
		out.Videos = append(out.Videos, spec.Video{Type: "url", URL: url, MimeType: "video/mp4", DurationSec: out.Seconds})
	}
	for _, item := range resp.Data {
		video := spec.Video{
			MimeType:    item.MimeType,
			DurationSec: firstNonZero(item.Duration, item.Seconds, out.Seconds),
		}
		switch {
		case item.B64JSON != "":
			video.Type = "inline_data"
			video.B64Data = item.B64JSON
		case item.URL != "":
			video.Type = "url"
			video.URL = item.URL
		default:
			continue
		}
		if video.MimeType == "" {
			video.MimeType = "video/mp4"
		}
		out.Videos = append(out.Videos, video)
	}
	return out, nil
}

// normalizeVideoStatus 把各家状态文案归一到内部四个取值；未知文案按生成中处理。
func normalizeVideoStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "pending", "waiting", "submitted", "created":
		return spec.VideoStatusQueued
	case "processing", "running", "in_progress", "generating", "started":
		return spec.VideoStatusProcessing
	case "completed", "complete", "succeeded", "success", "done", "finished":
		return spec.VideoStatusSucceeded
	case "failed", "error", "cancelled", "canceled", "failed_permanent":
		return spec.VideoStatusFailed
	default:
		return ""
	}
}

// ParseVideoContent 把取到的内容归一为一段视频；二进制内容按 base64 内联保存。
func ParseVideoContent(raw []byte, contentType string) (spec.Video, error) {
	if len(raw) == 0 {
		return spec.Video{}, fmt.Errorf("上游未返回视频内容")
	}
	mime := strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	if !strings.HasPrefix(mime, "video/") {
		mime = "video/mp4"
	}
	return spec.Video{Type: "inline_data", MimeType: mime, B64Data: base64.StdEncoding.EncodeToString(raw)}, nil
}

// ---------------------------------------------------------------- 客户端侧

// knownVideoFields 请求体中已被标准化的字段，其余字段统一收进 Extra。
var knownVideoFields = map[string]struct{}{
	"model": {}, "prompt": {}, "negative_prompt": {}, "seconds": {},
	"size": {}, "aspect_ratio": {}, "fps": {}, "seed": {}, "image": {}, "user": {},
}

// ParseVideoClientRequest 解析 OpenAI 兼容的视频生成请求体。
func ParseVideoClientRequest(raw []byte) (*spec.VideoRequest, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("解析视频请求失败: %w", err)
	}
	seconds := imageIntField(payload, "seconds")
	if seconds <= 0 {
		seconds = imageIntField(payload, "duration")
	}
	req := &spec.VideoRequest{
		Model:          imageStringField(payload, "model"),
		Prompt:         imageStringField(payload, "prompt"),
		NegativePrompt: imageStringField(payload, "negative_prompt"),
		Seconds:        seconds,
		Size:           imageStringField(payload, "size"),
		AspectRatio:    imageStringField(payload, "aspect_ratio"),
		FPS:            imageIntField(payload, "fps"),
		Image:          imageStringField(payload, "image"),
		User:           imageStringField(payload, "user"),
	}
	if v, ok := payload["seed"]; ok {
		if n, ok := v.(float64); ok {
			seed := int(n)
			req.Seed = &seed
		}
	}
	for key, value := range payload {
		if _, ok := knownVideoFields[key]; ok {
			continue
		}
		if req.Extra == nil {
			req.Extra = make(map[string]interface{})
		}
		req.Extra[key] = value
	}
	return req, nil
}

// BuildVideoClientResponse 把统一结果转回 OpenAI 兼容响应体。
func BuildVideoClientResponse(work *VideoWorkView) map[string]interface{} {
	data := make([]map[string]interface{}, 0, len(work.Videos))
	for _, video := range work.Videos {
		item := map[string]interface{}{"mime_type": video.MimeType}
		if video.B64Data != "" {
			item["b64_json"] = video.B64Data
		} else {
			item["url"] = video.URL
		}
		if video.DurationSec > 0 {
			item["duration"] = video.DurationSec
		}
		data = append(data, item)
	}
	return map[string]interface{}{
		"id":       work.TaskID,
		"object":   "video",
		"status":   work.StatusText,
		"model":    work.Model,
		"progress": work.Progress,
		"seconds":  work.DurationSec,
		"data":     data,
	}
}

// VideoWorkView 客户端响应需要的作品摘要（由 service 层填充）。
type VideoWorkView struct {
	// TaskID 上游任务 ID，响应里作为视频对象的 id。
	TaskID string
	// Model 对外模型名。
	Model string
	// StatusText 归一后的状态文案：queued / processing / succeeded / failed。
	StatusText string
	// Progress 进度百分比。
	Progress int
	// DurationSec 生成秒数。
	DurationSec int
	// Videos 已生成的内容。
	Videos []spec.Video
}

// VideoCreatedAt 生成时间兜底：上游未提供时用当前时间。
func VideoCreatedAt() int64 { return time.Now().Unix() }

func firstVideoID(values ...string) string {
	for _, v := range values {
		if v != "" && v != "video" && v != "list" {
			return v
		}
	}
	return ""
}

func firstVideoURL(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// secondsValue 把上游的时长字段统一为整数：可能是数字、字符串或缺失。
func secondsValue(raw json.Number) int {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return 0
	}
	if n, err := strconv.Atoi(text); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(text, 64); err == nil {
		return int(f)
	}
	return 0
}

// parseIntOrZero 解析字符串形式的秒数（Sora 允许 "4"），失败返回 0。
func parseIntOrZero(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	n := 0
	for _, r := range text {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
