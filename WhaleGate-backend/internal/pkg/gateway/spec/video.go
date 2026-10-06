package spec

// 视频生成与图片生成一样是独立的一对类型：既没有流式也没有工具调用，
// 经过统一格式再转回反而会丢失 async 任务语义，因此单独定义。

// VideoRequest 一次视频生成请求。
type VideoRequest struct {
	// Model 对外模型名。
	Model string
	// Prompt 画面提示词。
	Prompt string
	// NegativePrompt 不希望出现的元素。
	NegativePrompt string
	// Seconds 目标时长（秒）；上游未返回时长时按此计。
	Seconds int
	// Size 分辨率，如 1280x720。
	Size string
	// AspectRatio 画幅，如 16:9。
	AspectRatio string
	// FPS 帧率。
	FPS int
	// Seed 随机种子，便于结果复现。
	Seed *int
	// Image 图生视频的首帧图片（URL 或 data URI）。
	Image string
	// User 终端用户标识。
	User string
	// Extra 上游扩展参数，原样透传。
	Extra map[string]interface{}
}

// Video 一段生成结果。
type Video struct {
	// Type 取值：url（远程地址）、inline_data（原始 base64）。
	Type string `json:"type,omitempty"`
	// MimeType 例如 video/mp4。
	MimeType string `json:"mime_type,omitempty"`
	// B64Data 纯 base64 载荷。
	B64Data string `json:"b64_data,omitempty"`
	// URL 远程地址，与 B64Data 二选一。
	URL string `json:"url,omitempty"`
	// DurationSec 该片段时长（秒）。
	DurationSec int `json:"duration_sec,omitempty"`
}

// IsEmpty 判断是否为空结果。
func (v Video) IsEmpty() bool { return v.B64Data == "" && v.URL == "" }

// 视频任务状态（内部归一后的取值）。
const (
	VideoStatusQueued     = "queued"
	VideoStatusProcessing = "processing"
	VideoStatusSucceeded  = "succeeded"
	VideoStatusFailed     = "failed"
)

// VideoResponse 一次视频生成（提交或轮询）的结果。
type VideoResponse struct {
	// TaskID 上游任务标识，用于后续轮询与取内容。
	TaskID string `json:"task_id,omitempty"`
	// Status 归一后的任务状态，见 VideoStatus* 常量。
	Status string `json:"status"`
	// Progress 上游上报的进度百分比（0-100），未上报为 0。
	Progress int `json:"progress,omitempty"`
	// Seconds 上游上报的实际时长（秒）。
	Seconds int `json:"seconds,omitempty"`
	// Videos 已生成的内容；仅在成功后非空。
	Videos []Video `json:"videos,omitempty"`
	// Error 上游返回的失败原因。
	Error string `json:"error,omitempty"`
}

// Done 判断任务是否已终态（成功或失败）。
func (r *VideoResponse) Done() bool {
	if r == nil {
		return false
	}
	return r.Status == VideoStatusSucceeded || r.Status == VideoStatusFailed
}
