package service

import (
	"testing"

	"github.com/whalegate/whalegate/internal/config"
	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

func newVideoTestContainer(t *testing.T, ratios ...model.ModelRatio) *Container {
	t.Helper()
	c := newRatioContainer(t, ratios...)
	c.Config.Gateway.DefaultVideoRatio = 100
	return c
}

// ---------------------------------------------------------------- 协议识别

func TestIsVideoModel(t *testing.T) {
	cases := map[string]bool{
		"sora-2":       true,
		"veo-3.1":      true,
		"kling-v2":     true,
		"wan2.5-i2v":   true,
		"hailuo-02":    true,
		"qwen3.5-0.8b": false,
		"gpt-4o":       false,
		"gpt-image-1":  false,
	}
	for name, want := range cases {
		if got := gateway.IsVideoModel(name); got != want {
			t.Fatalf("IsVideoModel(%q) = %v，期望 %v", name, got, want)
		}
	}
}

func TestSupportsVideoGenerationByProtocol(t *testing.T) {
	// 只有 OpenAI 兼容协议提供 /v1/videos；Gemini 的 Veo 走独立接口，Anthropic 不提供。
	if !gateway.SupportsVideoGeneration(gateway.ProtocolOpenAI) {
		t.Fatal("OpenAI 兼容协议应支持视频生成")
	}
	if gateway.SupportsVideoGeneration(gateway.ProtocolAnthropic) {
		t.Fatal("Anthropic 协议不应支持视频生成")
	}
}

func TestBuildVideoSubmitURL(t *testing.T) {
	url, err := gateway.BuildVideoSubmitURL(gateway.ProtocolOpenAI, "https://up.example.com/v1/")
	if err != nil || url != "https://up.example.com/v1/videos" {
		t.Fatalf("提交端点错误: %q %v", url, err)
	}
	if _, err := gateway.BuildVideoStatusURL(gateway.ProtocolOpenAI, "https://up.example.com/v1", ""); err == nil {
		t.Fatal("缺少任务 ID 应报错")
	}
	status, _ := gateway.BuildVideoStatusURL(gateway.ProtocolOpenAI, "https://up.example.com/v1", "vid_1")
	if status != "https://up.example.com/v1/videos/vid_1" {
		t.Fatalf("状态端点错误: %q", status)
	}
	content, _ := gateway.BuildVideoContentURL(gateway.ProtocolOpenAI, "https://up.example.com/v1", "vid_1")
	if content != "https://up.example.com/v1/videos/vid_1/content" {
		t.Fatalf("内容端点错误: %q", content)
	}
}

func TestBuildVideoRequestFields(t *testing.T) {
	seed := 42
	body, err := gateway.BuildVideoRequest(gateway.ProtocolOpenAI, "https://up.example.com/v1", &spec.VideoRequest{
		Prompt:  "a cat on a roof",
		Seconds: 8,
		Size:    "1280x720",
		Seed:    &seed,
	}, "sora-2")
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if body["model"] != "sora-2" || body["prompt"] != "a cat on a roof" {
		t.Fatalf("基础字段错误: %v", body)
	}
	if body["seconds"] != 8 || body["size"] != "1280x720" || body["seed"] != 42 {
		t.Fatalf("可选字段错误: %v", body)
	}
	// 未填写的字段不应写进请求体，避免上游因空值报错。
	if _, ok := body["negative_prompt"]; ok {
		t.Fatalf("空字段不应下发: %v", body)
	}
}

// ---------------------------------------------------------------- 状态归一

func TestParseVideoSubmitResponse(t *testing.T) {
	resp, err := gateway.ParseVideoSubmitResponse(gateway.ProtocolOpenAI,
		[]byte(`{"id":"vid_abc","object":"video","status":"queued"}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.TaskID != "vid_abc" || resp.Status != spec.VideoStatusQueued {
		t.Fatalf("提交响应错误: %+v", resp)
	}

	// 上游不写 status 时按已排队处理，避免任务卡在空状态。
	resp, _ = gateway.ParseVideoSubmitResponse(gateway.ProtocolOpenAI, []byte(`{"id":"vid_x"}`))
	if resp.Status != spec.VideoStatusQueued {
		t.Fatalf("缺失 status 应视为排队: %+v", resp)
	}
}

func TestParseVideoStatusNormalizesAliases(t *testing.T) {
	// 各家状态文案不同，必须归一到内部四个取值。
	cases := map[string]string{
		`{"id":"v","status":"pending"}`:         spec.VideoStatusQueued,
		`{"id":"v","status":"in_progress"}`:     spec.VideoStatusProcessing,
		`{"id":"v","status":"completed"}`:       spec.VideoStatusSucceeded,
		`{"id":"v","status":"failed"}`:          spec.VideoStatusFailed,
		`{"id":"v","error":{"message":"boom"}}`: spec.VideoStatusFailed,
	}
	for raw, want := range cases {
		resp, err := gateway.ParseVideoStatusResponse(gateway.ProtocolOpenAI, []byte(raw))
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", raw, err)
		}
		if resp.Status != want {
			t.Fatalf("状态归一错误 %s: %s，期望 %s", raw, resp.Status, want)
		}
	}
}

func TestParseVideoStatusSucceededWithURL(t *testing.T) {
	resp, err := gateway.ParseVideoStatusResponse(gateway.ProtocolOpenAI,
		[]byte(`{"id":"v","status":"completed","seconds":"8","url":"https://cdn.example.com/a.mp4"}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if resp.Status != spec.VideoStatusSucceeded || resp.Seconds != 8 {
		t.Fatalf("成功态解析错误: %+v", resp)
	}
	if len(resp.Videos) != 1 || resp.Videos[0].URL != "https://cdn.example.com/a.mp4" {
		t.Fatalf("结果地址缺失: %+v", resp)
	}
	if !resp.Done() {
		t.Fatal("已完成应判定为终态")
	}
}

func TestParseVideoContent(t *testing.T) {
	video, err := gateway.ParseVideoContent([]byte("fake-bytes"), "video/mp4")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if video.Type != "inline_data" || video.MimeType != "video/mp4" || video.B64Data == "" {
		t.Fatalf("内容归一错误: %+v", video)
	}
	// 未声明类型时按 mp4 兜底，避免落盘后无法播放。
	video, _ = gateway.ParseVideoContent([]byte("x"), "application/octet-stream")
	if video.MimeType != "video/mp4" {
		t.Fatalf("未知类型应兜底为 mp4: %+v", video)
	}
}

func TestParseVideoClientRequest(t *testing.T) {
	req, err := gateway.ParseVideoClientRequest([]byte(
		`{"model":"sora-2","prompt":"a cat","seconds":6,"size":"1280x720","custom_flag":true}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if req.Model != "sora-2" || req.Seconds != 6 || req.Size != "1280x720" {
		t.Fatalf("标准字段错误: %+v", req)
	}
	// 非标准字段进 Extra，交给参数能力协商处理。
	if req.Extra["custom_flag"] != true {
		t.Fatalf("扩展字段丢失: %+v", req.Extra)
	}
}

// ---------------------------------------------------------------- 脚本解析

func TestParseScriptPayloadToleratesFence(t *testing.T) {
	payload, err := parseScriptPayload("```json\n" + `{"title":"t","scenes":[{"index":1,"visual":"v","seconds":4,"prompt":"p"}]}` + "\n```")
	if err != nil {
		t.Fatalf("带代码块的输出应能解析: %v", err)
	}
	if payload.Title != "t" || len(payload.Scenes) != 1 {
		t.Fatalf("解析结果错误: %+v", payload)
	}
}

func TestParseScriptPayloadRejectsGarbage(t *testing.T) {
	if _, err := parseScriptPayload("我需要更多信息才能开始"); err == nil {
		t.Fatal("非 JSON 输出应报错")
	}
	if _, err := parseScriptPayload(`{"title":"t","scenes":[]}`); err == nil {
		t.Fatal("空镜头数组应报错")
	}
}

func TestNormalizeScenesFillsDefaults(t *testing.T) {
	payload := &scriptPayload{Scenes: []model.VideoScriptScene{
		{Visual: "a"}, // 缺少时长
		{Prompt: "p"}, // 缺少画面描述
		{Visual: "c", Seconds: 2},
	}}
	normalizeScenes(payload, VideoScriptInput{Duration: 30, Scenes: 3})

	if payload.Scenes[0].Seconds != 4 {
		t.Fatalf("缺失时长应补默认值: %+v", payload.Scenes[0])
	}
	if payload.Scenes[0].Prompt != "a" || payload.Scenes[1].Visual != "p" {
		t.Fatalf("提示词与画面描述应互为兜底: %+v", payload.Scenes)
	}
	if payload.Scenes[0].Index != 1 || payload.Scenes[2].Index != 3 {
		t.Fatalf("镜头序号应连续补齐: %+v", payload.Scenes)
	}
}

func TestNormalizeScenesScalesToDuration(t *testing.T) {
	payload := &scriptPayload{Scenes: []model.VideoScriptScene{
		{Visual: "a", Seconds: 4},
		{Visual: "b", Seconds: 10},
		{Visual: "c", Seconds: 10},
	}}
	// 24 秒需收敛到 10 秒以内，且每镜至少 1 秒。
	normalizeScenes(payload, VideoScriptInput{Duration: 10, Scenes: 3})
	if total := sumSceneSeconds(payload.Scenes); total > 10 {
		t.Fatalf("超出总时长未收敛: %d", total)
	}
	for _, s := range payload.Scenes {
		if s.Seconds < 1 {
			t.Fatalf("收敛后不应出现 0 秒镜头: %+v", s)
		}
	}
}

// ---------------------------------------------------------------- 按秒计费

func TestPointsForVideo(t *testing.T) {
	c := newVideoTestContainer(t, model.ModelRatio{Model: "*", VideoRatio: 100})

	if got, _ := c.PointsForVideo(nil, "sora-2", 0, 1); got != 0 {
		t.Fatalf("秒数为 0 不应计费: %d", got)
	}
	if got, _ := c.PointsForVideo(nil, "sora-2", 5, 1); got != 500 {
		t.Fatalf("5 秒 × 100 点应为 500: %d", got)
	}
	// 10 秒与 4 秒成本差一倍，按秒计费才能反映真实消耗。
	if long, _ := c.PointsForVideo(nil, "sora-2", 10, 1); long != 1000 {
		t.Fatalf("10 秒应为 1000: %d", long)
	}
}

func TestPointsForVideoModelOverride(t *testing.T) {
	c := newVideoTestContainer(t,
		model.ModelRatio{Model: "*", VideoRatio: 100},
		model.ModelRatio{Model: "sora-2", VideoRatio: 250},
	)
	got, err := c.PointsForVideo(nil, "sora-2", 4, 1)
	if err != nil || got != 1000 {
		t.Fatalf("逐模型单价应覆盖兜底: %d %v", got, err)
	}
	if other, _ := c.PointsForVideo(nil, "veo-3", 4, 1); other != 400 {
		t.Fatalf("未配置模型应按兜底价: %d", other)
	}
}

func TestPointsForVideoAppliesGroupMultiplier(t *testing.T) {
	c := newVideoTestContainer(t, model.ModelRatio{Model: "*", VideoRatio: 100})
	got, err := c.PointsForVideo(nil, "sora-2", 3, 1.5)
	if err != nil || got != 450 {
		t.Fatalf("分组系数应在点数上相乘: %d %v", got, err)
	}
}

func TestVideoBillingFreeWhenDefaultZero(t *testing.T) {
	c := newVideoTestContainer(t)
	c.Config.Gateway.DefaultVideoRatio = 0
	got, err := c.PointsForVideo(nil, "sora-2", 10, 1)
	if err != nil || got != 0 {
		t.Fatalf("兜底单价为 0 时应免费: %d %v", got, err)
	}
}

// ---------------------------------------------------------------- 入参校验

func TestNormalizeVideoInput(t *testing.T) {
	c := &Container{Config: &config.Config{}}
	c.Config.Studio.VideoMaxSeconds = 20

	in := VideoWorkInput{Prompt: "a cat"}
	if err := c.normalizeVideoInput(&in); err == nil {
		t.Fatal("缺少模型应报错")
	}
	in = VideoWorkInput{Model: "sora-2"}
	if err := c.normalizeVideoInput(&in); err == nil {
		t.Fatal("缺少提示词应报错")
	}
	in = VideoWorkInput{Model: "sora-2", Prompt: "a cat", Seconds: 60}
	if err := c.normalizeVideoInput(&in); err == nil {
		t.Fatal("超出最大秒数应报错")
	}
	// 未填时长时给一个合理默认值，而不是按 0 处理。
	in = VideoWorkInput{Model: "sora-2", Prompt: "a cat"}
	if err := c.normalizeVideoInput(&in); err != nil || in.Seconds != 5 {
		t.Fatalf("未填时长应取默认值: %d %v", in.Seconds, err)
	}
}

func TestVideoStatusMapping(t *testing.T) {
	// 成功也要先转存，转存完成才置为已完成。
	if got := statusFromSpec(spec.VideoStatusSucceeded); got != model.VideoWorkStatusProcessing {
		t.Fatalf("成功态应先进入处理中: %d", got)
	}
	if got := statusFromSpec(spec.VideoStatusFailed); got != model.VideoWorkStatusFailed {
		t.Fatalf("失败态映射错误: %d", got)
	}
	if got := statusFromSpec(spec.VideoStatusQueued); got != model.VideoWorkStatusQueued {
		t.Fatalf("排队态映射错误: %d", got)
	}
	if statusText(model.VideoWorkStatusSuccess) != spec.VideoStatusSucceeded {
		t.Fatal("对外状态文案应归一")
	}
}

func TestDecodeBase64ToleratesMissingPadding(t *testing.T) {
	data, err := decodeBase64("aGVsbG8")
	if err != nil || string(data) != "hello" {
		t.Fatalf("缺失填充应自动补齐: %q %v", data, err)
	}
}
