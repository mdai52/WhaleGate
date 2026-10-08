// 视频工作台：脚本创作（按 token 计费）与视频生成（按秒计费）。
//
// 视频生成与图片的最大差别是**异步**：提交拿到任务 ID → 轮询状态 → 取内容转存。
// 因此作品表带任务状态，提交接口立即返回「排队中」，由后台协程推进到终态，
// 前端轮询作品列表即可看到进度，不必让 HTTP 请求挂几分钟。
package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
	"github.com/whalegate/whalegate/internal/pkg/storage"
	"github.com/whalegate/whalegate/internal/pkg/util"
)

// maxVideoBytes 单个视频文件转存上限（512MB）。
const maxVideoBytes = 512 << 20

// 脚本创作的系统提示词：强制模型输出可解析的 JSON，避免自由发挥导致解析失败。
const scriptSystemPrompt = `你是一名专业的短视频分镜编剧。用户会给出创作需求，你需要输出严格符合以下 JSON 结构的分镜脚本，不要输出任何解释文字、Markdown 代码块或前后缀：

{"title":"脚本标题","scenes":[{"index":1,"title":"镜头小标题","visual":"画面内容描述","narration":"旁白或台词","camera":"运镜方式","seconds":4,"prompt":"可直接用于视频生成模型的画面提示词（英文，含主体、动作、环境、光线、镜头语言、风格）"}]}

要求：
1. scenes 数组长度等于用户要求的镜头数，index 从 1 连续递增
2. seconds 为该镜头建议时长，各镜头 seconds 之和不超过用户要求的总时长
3. prompt 必须是英文，描述一个静态可生成的画面，不要包含时间轴、转场与剪辑指令
4. narration 使用与用户相同的语言`

// ---------------------------------------------------------------- 可选模型

// VideoModel 工作台可选的生成模型。
type VideoModel struct {
	// Model 对外模型名。
	Model string `json:"model"`
	// Channel 承载该模型的渠道名，仅用于提示。
	Channel string `json:"channel"`
	// Kind 用途：video 视频生成 / text 脚本创作。
	Kind string `json:"kind"`
}

// ListVideoModels 汇总启用渠道中可用于视频生成与脚本创作的模型。
//
// 视频模型与脚本模型的识别口径不同：视频模型按关键词与能力标记，
// 脚本模型则是「非生图、非视频」的普通对话模型。
func (c *Container) ListVideoModels(ctx context.Context) ([]VideoModel, error) {
	channels, err := c.EnabledChannels(ctx)
	if err != nil {
		return nil, err
	}
	var capable []string
	_ = c.DB.WithContext(ctx).Model(&model.ModelCatalog{}).
		Where("capabilities LIKE ?", `%"video"%`).
		Pluck("model_id", &capable)
	capableSet := make(map[string]struct{}, len(capable))
	for _, id := range capable {
		capableSet[id] = struct{}{}
	}

	var out []VideoModel
	seen := make(map[string]struct{})
	for _, ch := range channels {
		for _, name := range ch.ExposedModels() {
			if _, ok := seen[name]; ok {
				continue
			}
			kind := ""
			switch {
			case gateway.SupportsVideoGeneration(ch.Type) && (hasKey(capableSet, name) || gateway.IsVideoModel(name)):
				kind = "video"
			case !gateway.IsImageModel(name) && !gateway.IsVideoModel(name):
				kind = "text"
			}
			if kind == "" {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, VideoModel{Model: name, Channel: ch.Name, Kind: kind})
		}
	}
	return out, nil
}

func hasKey(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

// ---------------------------------------------------------------- 脚本创作

// VideoScriptInput 一次脚本创作的需求。
type VideoScriptInput struct {
	// Model 用于创作脚本的对话模型。
	Model string
	// Topic 主题 / 创意核心。
	Topic string
	// Style 风格，如「科技感」「电影感」。
	Style string
	// Audience 目标受众。
	Audience string
	// Duration 目标总时长（秒）。
	Duration int
	// Scenes 镜头数。
	Scenes int
	// Notes 补充要求。
	Notes string
	// TemplateID 套用的脚本模板 ID（仅记录来源）。
	TemplateID uint
}

// VideoScriptView 脚本视图。
type VideoScriptView struct {
	ID           uint                     `json:"id"`
	Model        string                   `json:"model"`
	Title        string                   `json:"title"`
	Brief        string                   `json:"brief"`
	Params       map[string]interface{}   `json:"params"`
	Script       []model.VideoScriptScene `json:"script"`
	SceneCount   int                      `json:"scene_count"`
	TotalSeconds int                      `json:"total_seconds"`
	Status       int                      `json:"status"`
	ErrorMessage string                   `json:"error_message,omitempty"`
	Points       int64                    `json:"points"`
	SelfUse      bool                     `json:"self_use"`
	LatencyMS    int                      `json:"latency_ms"`
	CreatedAt    time.Time                `json:"created_at"`
}

// scriptPayload 模型输出的脚本结构。
type scriptPayload struct {
	Title  string                   `json:"title"`
	Scenes []model.VideoScriptScene `json:"scenes"`
}

// CreateVideoScript 生成分镜脚本并落库。
//
// 计费口径与 chat 转发一致（按 token）：先按估算用量预扣，成功按实际用量结算。
func (c *Container) CreateVideoScript(
	ctx context.Context,
	userID uint,
	in VideoScriptInput,
	meta *ImageCallMeta,
) (*VideoScriptView, error) {
	in.Model = strings.TrimSpace(in.Model)
	in.Topic = strings.TrimSpace(in.Topic)
	if in.Model == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "请选择用于创作脚本的模型")
	}
	if in.Topic == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "请填写视频主题")
	}
	if in.Scenes <= 0 {
		in.Scenes = 5
	}
	if in.Scenes > 20 {
		return nil, apierr.New(apierr.ErrInvalidParam, "单次最多生成 20 个镜头")
	}
	if in.Duration <= 0 {
		in.Duration = 30
	}

	messages := []spec.Message{
		{Role: spec.RoleSystem, Content: scriptSystemPrompt},
		{Role: spec.RoleUser, Content: buildScriptUserPrompt(in)},
	}
	request := &spec.UnifiedRequest{Model: in.Model, Messages: messages}
	// 思考型模型（GLM 等）会把推理 token 也计入 max_tokens，给太小必然截断 JSON
	if maxTokens := 1024 + in.Scenes*400; maxTokens > 0 {
		if maxTokens > 8192 {
			maxTokens = 8192
		}
		request.MaxTokens = &maxTokens
	}

	selfUse := c.SelfUseMode()
	var reservation *Reservation
	if !selfUse && userID > 0 {
		estimate := c.EstimateUsage(request)
		points, err := c.PointsFor(ctx, in.Model, estimate, 1)
		if err == nil {
			reservation, err = c.ReserveQuota(ctx, userID, points)
		}
		if err != nil {
			return nil, err
		}
	}

	result, err := c.CompleteText(ctx, TextCompleteInput{
		Model:     in.Model,
		Messages:  messages,
		MaxTokens: *request.MaxTokens,
	})
	if err != nil {
		if reservation != nil {
			reservation.Release(context.Background())
		}
		return nil, err
	}

	payload, err := parseScriptPayload(result.Text)
	if err != nil {
		if reservation != nil {
			reservation.Release(context.Background())
		}
		return nil, apierr.Errorf(apierr.ErrUpstreamInvalid, "脚本解析失败：%s", err.Error())
	}
	normalizeScenes(payload, in)

	points, err := c.PointsFor(ctx, in.Model, result.Usage, 1)
	if err != nil {
		points = 0
		if c.Logger != nil {
			c.Logger.Warn("计算脚本点数失败", zap.Error(err))
		}
	}
	if reservation != nil {
		reservation.Settle(ctx, points)
	}
	if keyID := meta.KeyID(); keyID > 0 {
		c.UpdateAPIKeyUsage(ctx, keyID, result.Usage.TotalTokens, points)
	}

	script := &model.VideoScript{
		UserID:       userID,
		APIKeyID:     meta.KeyID(),
		ChannelID:    result.ChannelID,
		Model:        in.Model,
		Title:        payload.Title,
		Brief:        in.Topic,
		Params:       scriptParams(in),
		Script:       payload.Scenes,
		SceneCount:   len(payload.Scenes),
		TotalSeconds: sumSceneSeconds(payload.Scenes),
		Status:       model.VideoScriptStatusSuccess,
		Points:       points,
		SelfUse:      selfUse,
		LatencyMS:    int(result.LatencyMS),
	}
	if script.Title == "" {
		script.Title = truncate(in.Topic, 60)
	}
	if err := c.DB.WithContext(ctx).Create(script).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}

	c.logVideo(ctx, &model.CallLog{
		UserID:           userID,
		APIKeyID:         meta.KeyID(),
		SelfUse:          selfUse,
		Model:            in.Model,
		Protocol:         gateway.ProtocolOpenAI,
		Status:           model.CallStatusSuccess,
		PromptTokens:     result.Usage.PromptTokens,
		CompletionTokens: result.Usage.CompletionTokens,
		ReasoningTokens:  result.Usage.ReasoningTokens,
		TotalTokens:      result.Usage.TotalTokens,
		Points:           points,
		LatencyMS:        result.LatencyMS,
		UsageConfidence:  model.ConfidenceReported,
		ChannelID:        result.ChannelID,
		ChannelName:      result.ChannelName,
		UpstreamModel:    result.UpstreamModel,
		CreatedAt:        time.Now(),
	})

	view := toVideoScriptView(script)
	return &view, nil
}

// ListVideoScripts 分页查询当前用户的脚本。
func (c *Container) ListVideoScripts(ctx context.Context, userID uint, page, pageSize int) ([]VideoScriptView, int64, error) {
	page, pageSize = normalizePage(page, pageSize)
	q := c.DB.WithContext(ctx).Model(&model.VideoScript{}).Where("user_id = ?", userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var rows []model.VideoScript
	if err := q.Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	views := make([]VideoScriptView, 0, len(rows))
	for i := range rows {
		views = append(views, toVideoScriptView(&rows[i]))
	}
	return views, total, nil
}

// DeleteVideoScript 删除脚本。
func (c *Container) DeleteVideoScript(ctx context.Context, userID, scriptID uint, admin bool) error {
	q := c.DB.WithContext(ctx)
	if !admin {
		q = q.Where("user_id = ?", userID)
	}
	res := q.Delete(&model.VideoScript{}, scriptID)
	if res.Error != nil {
		return apierr.Wrap(apierr.ErrDatabase, res.Error)
	}
	if res.RowsAffected == 0 {
		return apierr.New(apierr.ErrNotFound, "脚本不存在")
	}
	return nil
}

func toVideoScriptView(s *model.VideoScript) VideoScriptView {
	return VideoScriptView{
		ID:           s.ID,
		Model:        s.Model,
		Title:        s.Title,
		Brief:        s.Brief,
		Params:       s.Params,
		Script:       s.Script,
		SceneCount:   s.SceneCount,
		TotalSeconds: s.TotalSeconds,
		Status:       s.Status,
		ErrorMessage: s.ErrorMessage,
		Points:       s.Points,
		SelfUse:      s.SelfUse,
		LatencyMS:    s.LatencyMS,
		CreatedAt:    s.CreatedAt,
	}
}

func scriptParams(in VideoScriptInput) map[string]interface{} {
	params := map[string]interface{}{"scenes": in.Scenes, "duration": in.Duration}
	if strings.TrimSpace(in.Style) != "" {
		params["style"] = strings.TrimSpace(in.Style)
	}
	if strings.TrimSpace(in.Audience) != "" {
		params["audience"] = strings.TrimSpace(in.Audience)
	}
	if in.TemplateID > 0 {
		params["template_id"] = in.TemplateID
	}
	return params
}

func buildScriptUserPrompt(in VideoScriptInput) string {
	var sb strings.Builder
	sb.WriteString("主题：")
	sb.WriteString(in.Topic)
	if s := strings.TrimSpace(in.Style); s != "" {
		sb.WriteString("\n风格：")
		sb.WriteString(s)
	}
	if s := strings.TrimSpace(in.Audience); s != "" {
		sb.WriteString("\n目标受众：")
		sb.WriteString(s)
	}
	sb.WriteString("\n总时长：")
	sb.WriteString(strconv.Itoa(in.Duration))
	sb.WriteString(" 秒\n镜头数：")
	sb.WriteString(strconv.Itoa(in.Scenes))
	if s := strings.TrimSpace(in.Notes); s != "" {
		sb.WriteString("\n补充要求：")
		sb.WriteString(s)
	}
	return sb.String()
}

// parseScriptPayload 从模型输出里提取分镜 JSON。
//
// 各家模型的表现差别很大：有的套 ```json 代码块，有的把推理过程留在正文里
// （智谱 GLM 等思考模型常见 <think>…</think>），有的输出被 max_tokens 截断。
// 这里从宽松到严格依次尝试，失败时把原始输出片段带回去，方便判断是哪种情况。
func parseScriptPayload(text string) (*scriptPayload, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, fmt.Errorf("模型未返回内容")
	}
	trimmed = stripThinkBlocks(trimmed)

	candidate := extractJSONObject(trimmed)
	if candidate == "" {
		return nil, fmt.Errorf("输出中未找到 JSON 对象：%s", outputSnippet(trimmed))
	}

	payload := &scriptPayload{}
	if err := json.Unmarshal([]byte(candidate), payload); err == nil {
		return checkScenes(payload)
	}
	// 常见脏字符：数组与对象末尾的尾随逗号
	repaired := trailingComma.ReplaceAllString(candidate, "$1")
	if err := json.Unmarshal([]byte(repaired), payload); err == nil {
		return checkScenes(payload)
	}
	// 输出被截断：补齐缺失的右括号后多数能救回来
	if salvaged := closeTruncatedJSON(repaired); salvaged != "" {
		if err := json.Unmarshal([]byte(salvaged), payload); err == nil {
			return checkScenes(payload)
		}
	}
	return nil, fmt.Errorf("无法解析为 JSON（可能是输出被 max_tokens 截断）：%s", outputSnippet(candidate))
}

func checkScenes(payload *scriptPayload) (*scriptPayload, error) {
	if len(payload.Scenes) == 0 {
		return nil, fmt.Errorf("未包含任何镜头")
	}
	return payload, nil
}

var (
	thinkBlock    = regexp.MustCompile(`(?s)<think>.*?</think>`)
	unclosedThink = regexp.MustCompile(`(?s)<think>.*`)
	// 尾随逗号：{"a":1,} / [1,2,]
	trailingComma = regexp.MustCompile(`,\s*([}\]])`)
)

// stripThinkBlocks 去掉思考模型残留在正文里的推理过程。
func stripThinkBlocks(text string) string {
	text = thinkBlock.ReplaceAllString(text, "")
	// 只有开始标记没有结束：整段都是思考过程，直接丢弃
	if idx := strings.Index(text, "<think>"); idx >= 0 {
		text = text[:idx]
	}
	return strings.TrimSpace(text)
}

// extractJSONObject 按括号配对截取最外层 JSON 对象，能正确跳过字符串里的括号。
// 括号不配对（输出被截断）时返回从首个 { 到结尾的片段，交给截断修复兜底。
func extractJSONObject(text string) string {
	start := strings.Index(text, "{")
	if start < 0 {
		return ""
	}
	depth, inString, escaped := 0, false, false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return text[start:]
}

// closeTruncatedJSON 为被截断的 JSON 补齐缺失的右括号。
// 用括号栈保证闭合顺序正确（截断常发生在 scenes 数组的某个对象中间）。
// 只处理括号层级，不猜字段值：能救回"最后一个镜头对象刚好完整"的情况。
func closeTruncatedJSON(text string) string {
	var stack []byte
	inString, escaped := false, false
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if inString {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, ch)
		case '}':
			if n := len(stack); n > 0 && stack[n-1] == '{' {
				stack = stack[:n-1]
			}
		case ']':
			if n := len(stack); n > 0 && stack[n-1] == '[' {
				stack = stack[:n-1]
			}
		}
	}
	// 还停在字符串里说明最后一个值只写了一半，救不回来
	if len(stack) == 0 || inString {
		return ""
	}
	// 悬空的逗号会让补齐后的 JSON 仍然非法
	out := strings.TrimRight(text, " \t\r\n,:")
	// 栈底到栈顶是开括号的出现顺序，闭合要从栈顶往回补，且要换成对应的闭括号
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '[' {
			out += "]"
		} else {
			out += "}"
		}
	}
	return out
}

// outputSnippet 截取输出片段用于报错，压掉换行避免日志与提示框被撑爆。
func outputSnippet(text string) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	const max = 160
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return string(runes)
}

// normalizeScenes 补齐缺失字段，并对超出总时长的镜头做等比收敛。
func normalizeScenes(payload *scriptPayload, in VideoScriptInput) {
	for i := range payload.Scenes {
		scene := &payload.Scenes[i]
		if scene.Index == 0 {
			scene.Index = i + 1
		}
		if scene.Seconds <= 0 {
			scene.Seconds = 4
		}
		if scene.Prompt == "" {
			scene.Prompt = scene.Visual
		}
		if scene.Visual == "" {
			scene.Visual = scene.Prompt
		}
	}
	// 超出目标总时长时按各镜头时长等比收敛，保证脚本可直接用于生成。
	if total := sumSceneSeconds(payload.Scenes); total > in.Duration && total > 0 {
		scale := float64(in.Duration) / float64(total)
		for i := range payload.Scenes {
			seconds := int(float64(payload.Scenes[i].Seconds) * scale)
			if seconds < 1 {
				seconds = 1
			}
			payload.Scenes[i].Seconds = seconds
		}
	}
}

func sumSceneSeconds(scenes []model.VideoScriptScene) int {
	total := 0
	for _, s := range scenes {
		total += s.Seconds
	}
	return total
}

// ---------------------------------------------------------------- 视频生成

// VideoWorkInput 工作台发起的一次视频生成。
type VideoWorkInput struct {
	// Model 视频模型名。
	Model string
	// Prompt 画面提示词。
	Prompt string
	// NegativePrompt 不希望出现的元素。
	NegativePrompt string
	// Seconds 目标时长（秒）。
	Seconds int
	// Size 分辨率。
	Size string
	// AspectRatio 画幅。
	AspectRatio string
	// FPS 帧率。
	FPS int
	// Image 图生视频的首帧。
	Image string
	// ScriptID / SceneIndex 来源脚本与镜头，仅作留痕。
	ScriptID   uint
	SceneIndex int
	// Extra 上游扩展参数。
	Extra map[string]interface{}
}

// VideoWorkView 视频作品视图。
type VideoWorkView struct {
	ID            uint                   `json:"id"`
	Model         string                 `json:"model"`
	UpstreamModel string                 `json:"upstream_model,omitempty"`
	Prompt        string                 `json:"prompt"`
	Params        map[string]interface{} `json:"params"`
	Videos        []VideoAssetView       `json:"videos"`
	Status        int                    `json:"status"`
	StatusText    string                 `json:"status_text"`
	TaskID        string                 `json:"task_id,omitempty"`
	Progress      int                    `json:"progress"`
	DurationSec   int                    `json:"duration_sec"`
	ErrorMessage  string                 `json:"error_message,omitempty"`
	Points        int64                  `json:"points"`
	SelfUse       bool                   `json:"self_use"`
	ChannelID     uint                   `json:"channel_id"`
	LatencyMS     int                    `json:"latency_ms"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

// VideoAssetView 作品中的一段视频。
type VideoAssetView struct {
	// URL 可直接访问的地址：配置了公开访问前缀时为存储直链，否则为本站路径。
	URL string `json:"url"`
	// NeedAuth URL 是否需要携带登录态拉取。
	NeedAuth bool `json:"need_auth"`
	// MimeType 视频类型。
	MimeType string `json:"mime_type"`
	// DurationSec 该片段时长（秒）。
	DurationSec int `json:"duration_sec,omitempty"`
}

// VideoSubmit 提交生成后的即时结果：任务已入队，终态由后台推进。
type VideoSubmit struct {
	// WorkID 作品 ID。
	WorkID uint `json:"work_id"`
	// Status 当前状态。
	Status int `json:"status"`
	// StatusText 状态文案。
	StatusText string `json:"status_text"`
	// TaskID 上游任务 ID。
	TaskID string `json:"task_id,omitempty"`
	// Message 提示信息。
	Message string `json:"message"`
}

// SubmitVideoWork 提交一次视频生成：预扣额度 → 选路提交 → 落库 → 后台推进任务。
//
// 返回时任务通常还在排队；真实结果（成功/失败/时长）由 finalizeVideoWork 写入。
func (c *Container) SubmitVideoWork(
	ctx context.Context,
	userID uint,
	in VideoWorkInput,
	meta *ImageCallMeta,
) (*VideoSubmit, error) {
	if err := c.normalizeVideoInput(&in); err != nil {
		return nil, err
	}

	selfUse := c.SelfUseMode()
	var reservation *Reservation
	if !selfUse && userID > 0 {
		points, err := c.PointsForVideo(ctx, in.Model, in.Seconds, 1)
		if err == nil {
			reservation, err = c.ReserveQuota(ctx, userID, points)
		}
		if err != nil {
			return nil, err
		}
	}

	channel, err := c.SelectChannel(ctx, in.Model, nil)
	if err != nil {
		if reservation != nil {
			reservation.Release(ctx)
		}
		return nil, err
	}
	if !gateway.SupportsVideoGeneration(channel.Type) {
		if reservation != nil {
			reservation.Release(ctx)
		}
		return nil, apierr.Errorf(apierr.ErrInvalidParam,
			"渠道「%s」协议为 %s，不支持视频生成", channel.Name, channel.Type)
	}

	upstreamModel, override := channel.ResolveModel(in.Model)
	submitURL, err := gateway.BuildVideoSubmitURL(channel.Type, channel.BaseURL)
	if err != nil {
		if reservation != nil {
			reservation.Release(ctx)
		}
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	payload, err := gateway.BuildVideoRequest(channel.Type, in.toSpec(), upstreamModel)
	if err != nil {
		if reservation != nil {
			reservation.Release(ctx)
		}
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	if len(override) > 0 {
		payload = util.MergeMap(payload, override)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		if reservation != nil {
			reservation.Release(ctx)
		}
		return nil, apierr.Wrap(apierr.ErrInternal, err)
	}

	apiKey, err := c.ChannelUpstreamKey(ctx, channel)
	if err != nil {
		if reservation != nil {
			reservation.Release(ctx)
		}
		return nil, err
	}
	upstream := gateway.NewUpstream(c.upstreamTimeout(channel))
	resp, err := upstream.Do(ctx, http.MethodPost, submitURL,
		gateway.BuildUpstreamHeaders(channel.Type, apiKey), body)
	if err != nil {
		if reservation != nil {
			reservation.Release(ctx)
		}
		c.RecordChannelFailure(ctx, channel, err.Error())
		return nil, classifyTextError(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTextResponseBody))
	if err != nil {
		if reservation != nil {
			reservation.Release(ctx)
		}
		return nil, apierr.Wrap(apierr.ErrUpstream, err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		if reservation != nil {
			reservation.Release(ctx)
		}
		msg := gateway.ExtractUpstreamError(channel.Type, raw)
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		c.RecordChannelFailure(ctx, channel, msg)
		return nil, upstreamTextHTTPError(resp.StatusCode, msg)
	}

	submitted, err := gateway.ParseVideoSubmitResponse(channel.Type, raw)
	if err != nil {
		if reservation != nil {
			reservation.Release(ctx)
		}
		c.RecordChannelFailure(ctx, channel, err.Error())
		return nil, apierr.Wrap(apierr.ErrUpstreamInvalid, err)
	}
	c.RecordChannelSuccess(ctx, channel)

	work := &model.VideoWork{
		UserID:         userID,
		APIKeyID:       meta.KeyID(),
		ChannelID:      channel.ID,
		Model:          in.Model,
		UpstreamModel:  upstreamModel,
		Prompt:         in.Prompt,
		NegativePrompt: in.NegativePrompt,
		Params:         videoParams(in),
		Status:         statusFromSpec(submitted.Status),
		TaskID:         submitted.TaskID,
		Progress:       submitted.Progress,
		DurationSec:    submitted.Seconds,
		SelfUse:        selfUse,
	}
	if submitted.Status == spec.VideoStatusFailed {
		work.Status = model.VideoWorkStatusFailed
		work.ErrorMessage = truncate(submitted.Error, 500)
	}
	if err := c.DB.WithContext(ctx).Create(work).Error; err != nil {
		if reservation != nil {
			reservation.Release(ctx)
		}
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}

	// 已直接返回内容的同步上游：立即收尾，无需轮询。
	if submitted.Status == spec.VideoStatusSucceeded && len(submitted.Videos) > 0 {
		go c.finalizeVideoWork(work.ID, channel.ID, submitted.TaskID, submitted.Videos, submitted.Seconds, reservation, selfUse, in)
		return &VideoSubmit{
			WorkID:     work.ID,
			Status:     model.VideoWorkStatusProcessing,
			StatusText: model.VideoWorkStatusText(model.VideoWorkStatusProcessing),
			TaskID:     submitted.TaskID,
			Message:    "已提交，正在转存生成结果",
		}, nil
	}

	// 异步任务：后台轮询直到终态。
	go c.pollVideoWork(work.ID, channel.ID, submitted.TaskID, in.Seconds, reservation, selfUse, in)

	return &VideoSubmit{
		WorkID:     work.ID,
		Status:     work.Status,
		StatusText: model.VideoWorkStatusText(work.Status),
		TaskID:     submitted.TaskID,
		Message:    "已提交，可在作品库查看进度",
	}, nil
}

// pollVideoWork 后台轮询上游任务直到终态，再收尾。
func (c *Container) pollVideoWork(
	workID, channelID uint,
	taskID string,
	requestedSeconds int,
	reservation *Reservation,
	selfUse bool,
	in VideoWorkInput,
) {
	ctx, cancel := context.WithTimeout(context.Background(), c.videoMaxWait())
	defer cancel()

	interval := c.Config.Studio.VideoPollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}

	var lastStatus string
	for {
		var channel model.Channel
		if err := c.DB.WithContext(ctx).First(&channel, channelID).Error; err != nil {
			c.failVideoWork(workID, "读取渠道失败", reservation)
			return
		}
		statusURL, err := gateway.BuildVideoStatusURL(channel.Type, channel.BaseURL, taskID)
		if err != nil {
			c.failVideoWork(workID, err.Error(), reservation)
			return
		}
		apiKey, err := c.ChannelUpstreamKey(ctx, &channel)
		if err != nil {
			c.failVideoWork(workID, err.Error(), reservation)
			return
		}
		upstream := gateway.NewUpstream(c.upstreamTimeout(&channel))
		resp, err := upstream.Do(ctx, http.MethodGet, statusURL,
			gateway.BuildUpstreamHeaders(channel.Type, apiKey), nil)
		if err != nil {
			// 轮询失败多为网络抖动，重试到超时为止，不立即判死。
			if !sleepCtx(ctx, interval) {
				c.failVideoWork(workID, "轮询超时："+err.Error(), reservation)
				return
			}
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, maxTextResponseBody))
		resp.Body.Close()
		if err != nil || resp.StatusCode >= http.StatusBadRequest {
			if !sleepCtx(ctx, interval) {
				c.failVideoWork(workID, "轮询上游失败", reservation)
				return
			}
			continue
		}
		status, err := gateway.ParseVideoStatusResponse(channel.Type, raw)
		if err != nil {
			if !sleepCtx(ctx, interval) {
				c.failVideoWork(workID, err.Error(), reservation)
				return
			}
			continue
		}

		if status.Status != lastStatus {
			lastStatus = status.Status
			c.updateVideoProgress(workID, statusFromSpec(status.Status), status.Progress)
		}
		if status.Status == spec.VideoStatusFailed {
			message := status.Error
			if message == "" {
				message = "上游生成失败"
			}
			c.failVideoWork(workID, message, reservation)
			return
		}
		if status.Status == spec.VideoStatusSucceeded {
			videos := status.Videos
			if len(videos) == 0 {
				// 状态已成功但没给地址：按约定去 /content 取内容。
				content, err := c.fetchVideoContent(ctx, &channel, taskID)
				if err != nil {
					c.failVideoWork(workID, err.Error(), reservation)
					return
				}
				videos = []spec.Video{content}
			}
			c.finalizeVideoWork(workID, channelID, taskID, videos,
				firstNonZeroVideoSeconds(status.Seconds, requestedSeconds), reservation, selfUse, in)
			return
		}
		if !sleepCtx(ctx, interval) {
			c.failVideoWork(workID, "生成超时，未在限定时间内完成", reservation)
			return
		}
	}
}

// finalizeVideoWork 下载内容、转存、结算并落日志。
func (c *Container) finalizeVideoWork(
	workID, channelID uint,
	taskID string,
	videos []spec.Video,
	seconds int,
	reservation *Reservation,
	selfUse bool,
	in VideoWorkInput,
) {
	ctx, cancel := context.WithTimeout(context.Background(), c.videoDownloadTimeout())
	defer cancel()

	assets := c.storeVideos(ctx, workID, videos)
	if len(assets) == 0 {
		c.failVideoWork(workID, "生成结果转存失败", reservation)
		return
	}

	points, err := c.PointsForVideo(ctx, in.Model, seconds, 1)
	if err != nil {
		points = 0
		if c.Logger != nil {
			c.Logger.Warn("计算视频点数失败", zap.Error(err))
		}
	}
	if reservation != nil {
		reservation.Settle(ctx, points)
	}
	work := model.VideoWork{}
	if err := c.DB.WithContext(ctx).First(&work, workID).Error; err == nil {
		work.Videos = assets
		work.Status = model.VideoWorkStatusSuccess
		work.Progress = 100
		work.DurationSec = seconds
		work.Points = points
		work.SelfUse = selfUse
		work.ErrorMessage = ""
		_ = c.DB.WithContext(ctx).Save(&work).Error

		if keyID := work.APIKeyID; keyID > 0 {
			c.UpdateAPIKeyUsage(ctx, keyID, 0, points)
		}
		c.logVideo(ctx, &model.CallLog{
			UserID:          work.UserID,
			APIKeyID:        work.APIKeyID,
			SelfUse:         selfUse,
			Model:           work.Model,
			Protocol:        gateway.ProtocolOpenAI,
			Status:          model.CallStatusSuccess,
			Points:          points,
			VideoSeconds:    seconds,
			LatencyMS:       int64(time.Since(work.CreatedAt).Milliseconds()),
			ChannelID:       channelID,
			ChannelName:     channelName(c, ctx, channelID),
			UpstreamModel:   work.UpstreamModel,
			UsageConfidence: model.ConfidenceEstimated,
			CreatedAt:       time.Now(),
		})
	}
	_ = taskID
}

// fetchVideoContent 从 /content 端点取回视频内容。
func (c *Container) fetchVideoContent(ctx context.Context, channel *model.Channel, taskID string) (spec.Video, error) {
	contentURL, err := gateway.BuildVideoContentURL(channel.Type, channel.BaseURL, taskID)
	if err != nil {
		return spec.Video{}, err
	}
	apiKey, err := c.ChannelUpstreamKey(ctx, channel)
	if err != nil {
		return spec.Video{}, err
	}
	upstream := gateway.NewUpstream(c.upstreamTimeout(channel))
	resp, err := upstream.Do(ctx, http.MethodGet, contentURL,
		gateway.BuildUpstreamHeaders(channel.Type, apiKey), nil)
	if err != nil {
		return spec.Video{}, classifyTextError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return spec.Video{}, upstreamTextHTTPError(resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxVideoBytes))
	if err != nil {
		return spec.Video{}, apierr.Wrap(apierr.ErrUpstream, err)
	}
	return gateway.ParseVideoContent(raw, resp.Header.Get("Content-Type"))
}

// storeVideos 把生成结果写入当前配置的存储后端；单段失败时降级保留上游地址。
func (c *Container) storeVideos(ctx context.Context, workID uint, videos []spec.Video) []model.VideoAsset {
	store := c.studioStore.Current()
	assets := make([]model.VideoAsset, 0, len(videos))
	for i, video := range videos {
		asset, err := c.storeVideo(ctx, store, workID, i, video)
		if err != nil {
			if c.Logger != nil {
				c.Logger.Warn("视频转存失败，降级为上游地址",
					zap.Uint("work_id", workID), zap.Int("index", i), zap.Error(err))
			}
			if video.URL == "" {
				continue
			}
			asset = model.VideoAsset{URL: video.URL, MimeType: video.MimeType, DurationSec: video.DurationSec}
		}
		assets = append(assets, asset)
	}
	return assets
}

func (c *Container) storeVideo(
	ctx context.Context,
	store storage.Storage,
	workID uint,
	index int,
	video spec.Video,
) (model.VideoAsset, error) {
	data, mime, err := c.fetchVideoBytes(ctx, video)
	if err != nil {
		return model.VideoAsset{}, err
	}
	key := storage.JoinKey("video", strconv.FormatUint(uint64(workID), 10),
		strconv.Itoa(index)+videoExt(mime))
	if err := store.Put(ctx, storage.Object{Key: key, ContentType: mime, Data: data}); err != nil {
		return model.VideoAsset{}, apierr.Wrap(apierr.ErrInternal, err)
	}
	return model.VideoAsset{Storage: store.Kind(), Key: key, MimeType: mime, DurationSec: video.DurationSec}, nil
}

// fetchVideoBytes 取得视频原始字节：base64 直接解码，远程地址则下载转存。
func (c *Container) fetchVideoBytes(ctx context.Context, video spec.Video) ([]byte, string, error) {
	if video.B64Data != "" {
		data, err := decodeBase64(video.B64Data)
		if err != nil {
			return nil, "", apierr.Wrap(apierr.ErrUpstreamInvalid, err)
		}
		return data, normalizeVideoMime(video.MimeType), nil
	}
	if video.URL == "" {
		return nil, "", apierr.New(apierr.ErrUpstreamInvalid, "上游未返回视频内容")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, video.URL, nil)
	if err != nil {
		return nil, "", apierr.Wrap(apierr.ErrUpstream, err)
	}
	client := &http.Client{Timeout: c.videoDownloadTimeout()}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", apierr.Wrap(apierr.ErrUpstream, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, "", apierr.Errorf(apierr.ErrUpstream, "转存上游视频失败：HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxVideoBytes))
	if err != nil {
		return nil, "", apierr.Wrap(apierr.ErrUpstream, err)
	}
	mime := video.MimeType
	if mime == "" {
		mime = resp.Header.Get("Content-Type")
	}
	return data, normalizeVideoMime(mime), nil
}

// ---------------------------------------------------------------- 作品库

// ListVideoWorks 分页查询当前用户的作品，并把超时的未完成任务标记为失败。
func (c *Container) ListVideoWorks(ctx context.Context, userID uint, page, pageSize int) ([]VideoWorkView, int64, error) {
	c.abandonStaleVideoWorks(ctx, userID)
	page, pageSize = normalizePage(page, pageSize)
	q := c.DB.WithContext(ctx).Model(&model.VideoWork{}).Where("user_id = ?", userID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	var rows []model.VideoWork
	if err := q.Order("created_at DESC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		return nil, 0, apierr.Wrap(apierr.ErrDatabase, err)
	}
	views := make([]VideoWorkView, 0, len(rows))
	for i := range rows {
		views = append(views, c.toVideoWorkView(&rows[i]))
	}
	return views, total, nil
}

// GetVideoWork 查询单个作品（含状态），供前端轮询进度。
func (c *Container) GetVideoWork(ctx context.Context, userID, workID uint, admin bool) (*VideoWorkView, error) {
	var work model.VideoWork
	if err := c.DB.WithContext(ctx).First(&work, workID).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	if !admin && work.UserID != userID {
		return nil, apierr.New(apierr.ErrForbidden, "无权访问该作品")
	}
	view := c.toVideoWorkView(&work)
	return &view, nil
}

// GetVideoWorkByIdentifier 按作品 ID 或上游任务 ID 查询作品。
//
// 对外 API（/v1/videos/{id}）返回的是作品 ID，但 Sora 风格的调用方
// 手里往往只有提交时返回的任务 ID，两种都认可以减少对接摩擦。
func (c *Container) GetVideoWorkByIdentifier(
	ctx context.Context, userID uint, identifier string, admin bool,
) (*VideoWorkView, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return nil, apierr.New(apierr.ErrNotFound, "作品不存在")
	}
	if id, err := strconv.ParseUint(identifier, 10, 64); err == nil && id > 0 {
		return c.GetVideoWork(ctx, userID, uint(id), admin)
	}
	var work model.VideoWork
	q := c.DB.WithContext(ctx).Where("task_id = ?", identifier)
	if !admin {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.Order("id DESC").First(&work).Error; err != nil {
		return nil, apierr.New(apierr.ErrNotFound, "作品不存在")
	}
	view := c.toVideoWorkView(&work)
	return &view, nil
}

// VideoWorkContent 对外取内容：优先回读托管文件；转存失败只保留了
// 上游临时地址时，返回该地址供 302 跳转。
func (c *Container) VideoWorkContent(
	ctx context.Context, userID, workID uint, admin bool,
) (*VideoWorkAsset, string, error) {
	var work model.VideoWork
	if err := c.DB.WithContext(ctx).First(&work, workID).Error; err != nil {
		return nil, "", apierr.Wrap(apierr.ErrNotFound, err)
	}
	if !admin && work.UserID != userID {
		return nil, "", apierr.New(apierr.ErrForbidden, "无权访问该作品")
	}
	if work.Status != model.VideoWorkStatusSuccess || len(work.Videos) == 0 {
		return nil, "", apierr.New(apierr.ErrNotFound, "视频尚未生成完成")
	}
	asset := work.Videos[0]
	if !asset.UsesStorage() {
		if asset.URL == "" {
			return nil, "", apierr.New(apierr.ErrNotFound, "该视频没有可用的内容地址")
		}
		return nil, asset.URL, nil
	}
	file, err := c.OpenVideoWorkFile(ctx, userID, workID, 0, admin)
	if err != nil {
		return nil, "", err
	}
	return file, "", nil
}

// DeleteVideoWork 删除作品并回收其视频。
func (c *Container) DeleteVideoWork(ctx context.Context, userID, workID uint, admin bool) error {
	var work model.VideoWork
	if err := c.DB.WithContext(ctx).First(&work, workID).Error; err != nil {
		return apierr.Wrap(apierr.ErrNotFound, err)
	}
	if !admin && work.UserID != userID {
		return apierr.New(apierr.ErrForbidden, "无权访问该作品")
	}
	if err := c.DB.WithContext(ctx).Delete(&model.VideoWork{}, workID).Error; err != nil {
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	prefix := storage.JoinKey("video", strconv.FormatUint(uint64(workID), 10))
	if err := c.studioStore.For(videoAssetStorage(work.Videos)).Delete(ctx, prefix); err != nil && c.Logger != nil {
		c.Logger.Warn("清理作品视频失败", zap.Uint("work_id", workID), zap.Error(err))
	}
	return nil
}

// VideoWorkAsset 校验归属后定位到的视频文件，供支持 Range 的回读使用。
//
// 这里不给 Reader 而是给存储后端 + 对象键：视频需要按字节区间读取
// （拖动进度条），只有拿到后端才能应答 206 分段。
type VideoWorkAsset struct {
	// Store 该视频写入时使用的存储后端。
	Store storage.Storage
	// Key 对象键。
	Key string
	// ContentType 响应类型，如 video/mp4。
	ContentType string
	// FileName 下载时使用的建议文件名。
	FileName string
}

// OpenVideoWorkFile 打开作品视频。
//
// 返回的是存储定位而不是已打开的流：视频需要按字节区间读取（拖动进度条），
// HTTP 层拿到后端与对象键才能应答 206 分段。
func (c *Container) OpenVideoWorkFile(
	ctx context.Context, userID, workID uint, index int, admin bool,
) (*VideoWorkAsset, error) {
	var work model.VideoWork
	if err := c.DB.WithContext(ctx).First(&work, workID).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	if !admin && work.UserID != userID {
		return nil, apierr.New(apierr.ErrForbidden, "无权访问该作品")
	}
	if index < 0 || index >= len(work.Videos) {
		return nil, apierr.New(apierr.ErrNotFound, "视频不存在")
	}
	asset := work.Videos[index]
	if !asset.UsesStorage() {
		return nil, apierr.New(apierr.ErrNotFound, "该视频未托管在服务端")
	}
	mime := asset.MimeType
	if mime == "" {
		mime = "video/mp4"
	}
	return &VideoWorkAsset{
		Store:       c.studioStore.For(asset.Storage),
		Key:         asset.Key,
		ContentType: mime,
		FileName:    fmt.Sprintf("whalegate-%d-%d%s", workID, index+1, videoExt(mime)),
	}, nil
}

// LocateVideoWorkFile 校验归属后返回视频的存储定位，供支持 Range 的回读使用。
func (c *Container) LocateVideoWorkFile(
	ctx context.Context, userID, workID uint, index int, admin bool,
) (*VideoWorkAsset, error) {
	var work model.VideoWork
	if err := c.DB.WithContext(ctx).First(&work, workID).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	if !admin && work.UserID != userID {
		return nil, apierr.New(apierr.ErrForbidden, "无权访问该作品")
	}
	if index < 0 || index >= len(work.Videos) {
		return nil, apierr.New(apierr.ErrNotFound, "视频不存在")
	}
	asset := work.Videos[index]
	if !asset.UsesStorage() {
		return nil, apierr.New(apierr.ErrNotFound, "该视频未托管在服务端")
	}
	mime := asset.MimeType
	if mime == "" {
		mime = "video/mp4"
	}
	return &VideoWorkAsset{
		Store:       c.studioStore.For(asset.Storage),
		Key:         asset.Key,
		ContentType: mime,
		FileName:    fmt.Sprintf("whalegate-%d-%d%s", workID, index+1, videoExt(mime)),
	}, nil
}

func (c *Container) toVideoWorkView(work *model.VideoWork) VideoWorkView {
	videos := make([]VideoAssetView, 0, len(work.Videos))
	for i, asset := range work.Videos {
		view := VideoAssetView{MimeType: asset.MimeType, DurationSec: asset.DurationSec}
		if !asset.UsesStorage() {
			view.URL = asset.URL
			videos = append(videos, view)
			continue
		}
		if public := c.studioStore.For(asset.Storage).PublicURL(asset.Key); public != "" {
			view.URL = public
			videos = append(videos, view)
			continue
		}
		view.URL = fmt.Sprintf("/api/v1/studio/video/works/%d/videos/%d", work.ID, i)
		view.NeedAuth = true
		videos = append(videos, view)
	}
	return VideoWorkView{
		ID:            work.ID,
		Model:         work.Model,
		UpstreamModel: work.UpstreamModel,
		Prompt:        work.Prompt,
		Params:        work.Params,
		Videos:        videos,
		Status:        work.Status,
		StatusText:    statusText(work.Status),
		TaskID:        work.TaskID,
		Progress:      work.Progress,
		DurationSec:   work.DurationSec,
		ErrorMessage:  work.ErrorMessage,
		Points:        work.Points,
		SelfUse:       work.SelfUse,
		ChannelID:     work.ChannelID,
		LatencyMS:     work.LatencyMS,
		CreatedAt:     work.CreatedAt,
		UpdatedAt:     work.UpdatedAt,
	}
}

// ---------------------------------------------------------------- 内部辅助

func (c *Container) normalizeVideoInput(in *VideoWorkInput) error {
	in.Model = strings.TrimSpace(in.Model)
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.Model == "" {
		return apierr.New(apierr.ErrInvalidParam, "请选择视频模型")
	}
	if in.Prompt == "" {
		return apierr.New(apierr.ErrInvalidParam, "请填写画面提示词")
	}
	if len([]rune(in.Prompt)) > maxImagePromptRunes {
		return apierr.Errorf(apierr.ErrInvalidParam, "提示词过长，最多 %d 字", maxImagePromptRunes)
	}
	// 单次最大秒数在系统设置里维护，管理员调整后即时生效。
	max := c.Settings().VideoMaxSeconds
	if max <= 0 {
		max = c.Config.Studio.VideoMaxSeconds
	}
	if max <= 0 {
		max = 20
	}
	if in.Seconds <= 0 {
		in.Seconds = 5
	}
	if in.Seconds > max {
		return apierr.Errorf(apierr.ErrInvalidParam, "单个视频最多 %d 秒", max)
	}
	return nil
}

func (in VideoWorkInput) toSpec() *spec.VideoRequest {
	return &spec.VideoRequest{
		Model:          in.Model,
		Prompt:         in.Prompt,
		NegativePrompt: in.NegativePrompt,
		Seconds:        in.Seconds,
		Size:           in.Size,
		AspectRatio:    in.AspectRatio,
		FPS:            in.FPS,
		Image:          in.Image,
		Extra:          in.Extra,
	}
}

func videoParams(in VideoWorkInput) map[string]interface{} {
	params := map[string]interface{}{"seconds": in.Seconds}
	if in.Size != "" {
		params["size"] = in.Size
	}
	if in.AspectRatio != "" {
		params["aspect_ratio"] = in.AspectRatio
	}
	if in.FPS > 0 {
		params["fps"] = in.FPS
	}
	if in.ScriptID > 0 {
		params["script_id"] = in.ScriptID
		params["scene_index"] = in.SceneIndex
	}
	return params
}

// failVideoWork 标记失败并回补预扣。
func (c *Container) failVideoWork(workID uint, message string, reservation *Reservation) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = c.DB.WithContext(ctx).Model(&model.VideoWork{}).Where("id = ?", workID).
		Updates(map[string]interface{}{
			"status":        model.VideoWorkStatusFailed,
			"error_message": truncate(message, 500),
		}).Error
	if reservation != nil {
		reservation.Release(ctx)
	}
}

// updateVideoProgress 更新任务状态与进度。
func (c *Container) updateVideoProgress(workID uint, status, progress int) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = c.DB.WithContext(ctx).Model(&model.VideoWork{}).Where("id = ?", workID).
		Updates(map[string]interface{}{"status": status, "progress": progress}).Error
}

// abandonStaleVideoWorks 把超过最长等待时间仍未终态的任务标记为失败。
//
// 后台协程只存在于当前进程：进程重启后遗留的排队任务永远不会被推进，
// 查询时统一收口，避免作品库里出现永远转圈的条目。
func (c *Container) abandonStaleVideoWorks(ctx context.Context, userID uint) {
	deadline := time.Now().Add(-c.videoMaxWait())
	_ = c.DB.WithContext(ctx).Model(&model.VideoWork{}).
		Where("user_id = ? AND status IN ? AND created_at < ?",
			userID, []int{model.VideoWorkStatusQueued, model.VideoWorkStatusProcessing}, deadline).
		Updates(map[string]interface{}{
			"status":        model.VideoWorkStatusFailed,
			"error_message": "生成超时，任务已放弃",
		}).Error
}

// logVideo 落调用日志；日志写入器为空（测试场景）时静默跳过。
func (c *Container) logVideo(ctx context.Context, entry *model.CallLog) {
	if c.Logs == nil {
		return
	}
	c.Logs.Submit(entry)
}

func (c *Container) videoMaxWait() time.Duration {
	if c.Config.Studio.VideoMaxWait > 0 {
		return c.Config.Studio.VideoMaxWait
	}
	return 10 * time.Minute
}

func (c *Container) videoDownloadTimeout() time.Duration {
	if c.Config.Studio.VideoDownloadTimeout > 0 {
		return c.Config.Studio.VideoDownloadTimeout
	}
	return 180 * time.Second
}

func statusFromSpec(status string) int {
	switch status {
	case spec.VideoStatusProcessing:
		return model.VideoWorkStatusProcessing
	case spec.VideoStatusSucceeded:
		return model.VideoWorkStatusProcessing // 成功也要先转存，转存完才置为已完成
	case spec.VideoStatusFailed:
		return model.VideoWorkStatusFailed
	default:
		return model.VideoWorkStatusQueued
	}
}

// statusText 返回对外状态文案（英文，便于客户端直接判断）。
func statusText(status int) string {
	switch status {
	case model.VideoWorkStatusQueued:
		return spec.VideoStatusQueued
	case model.VideoWorkStatusProcessing:
		return spec.VideoStatusProcessing
	case model.VideoWorkStatusSuccess:
		return spec.VideoStatusSucceeded
	default:
		return spec.VideoStatusFailed
	}
}

func firstNonZeroVideoSeconds(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func videoAssetStorage(assets []model.VideoAsset) string {
	for _, a := range assets {
		if a.Storage != "" {
			return a.Storage
		}
	}
	return storage.KindLocal
}

func normalizeVideoMime(mime string) string {
	mime = strings.TrimSpace(strings.SplitN(mime, ";", 2)[0])
	if strings.HasPrefix(mime, "video/") {
		return mime
	}
	return "video/mp4"
}

func videoExt(mime string) string {
	switch mime {
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	case "video/x-msvideo":
		return ".avi"
	default:
		return ".mp4"
	}
}

func channelName(c *Container, ctx context.Context, channelID uint) string {
	if channelID == 0 {
		return ""
	}
	var channel model.Channel
	if err := c.DB.WithContext(ctx).First(&channel, channelID).Error; err != nil {
		return ""
	}
	return channel.Name
}

// decodeBase64 解码上游返回的视频载荷，容忍缺失的填充字符。
func decodeBase64(text string) ([]byte, error) {
	cleaned := strings.TrimSpace(text)
	if pad := len(cleaned) % 4; pad != 0 {
		cleaned += strings.Repeat("=", 4-pad)
	}
	return base64.StdEncoding.DecodeString(cleaned)
}

// sleepCtx 等待指定时长；上下文取消时返回 false。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
