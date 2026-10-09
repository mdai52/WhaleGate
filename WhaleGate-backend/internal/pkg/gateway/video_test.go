package gateway

import (
	"strings"
	"testing"

	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

const (
	openAIBase = "https://api.example.com/v1"
	zhipuBase  = "https://open.bigmodel.cn/api/paas/v4"
)

// 视频端点没有统一标准，Sora 风格与智谱风格的路径完全不同。
func TestBuildVideoURLs(t *testing.T) {
	submit, err := BuildVideoSubmitURL(ProtocolOpenAI, openAIBase)
	if err != nil || submit != openAIBase+"/videos" {
		t.Fatalf("Sora 风格提交端点错误: %q, %v", submit, err)
	}
	submit, err = BuildVideoSubmitURL(ProtocolOpenAI, zhipuBase)
	if err != nil || submit != zhipuBase+"/videos/generations" {
		t.Fatalf("智谱提交端点错误: %q, %v", submit, err)
	}

	status, err := BuildVideoStatusURL(ProtocolOpenAI, openAIBase, "task-1")
	if err != nil || status != openAIBase+"/videos/task-1" {
		t.Fatalf("Sora 风格查询端点错误: %q, %v", status, err)
	}
	status, err = BuildVideoStatusURL(ProtocolOpenAI, zhipuBase, "task-1")
	if err != nil || status != zhipuBase+"/async-result/task-1" {
		t.Fatalf("智谱查询端点错误: %q, %v", status, err)
	}

	content, err := BuildVideoContentURL(ProtocolOpenAI, openAIBase, "task-1")
	if err != nil || content != openAIBase+"/videos/task-1/content" {
		t.Fatalf("Sora 风格内容端点错误: %q, %v", content, err)
	}
	// 智谱没有独立的内容端点：结果随任务状态一并返回
	if _, err := BuildVideoContentURL(ProtocolOpenAI, zhipuBase, "task-1"); err == nil {
		t.Fatal("智谱不应提供内容端点")
	}

	// 非 OpenAI 协议一律不支持视频生成
	if _, err := BuildVideoSubmitURL(ProtocolGemini, openAIBase); err == nil {
		t.Fatal("Gemini 渠道不应支持视频生成")
	}
}

func TestParseZhipuVideoResponses(t *testing.T) {
	// 提交：只有 task_status，没有 status
	submitted, err := ParseVideoSubmitResponse(ProtocolOpenAI,
		[]byte(`{"id":"8989","model":"cogvideox-flash","task_status":"PROCESSING"}`))
	if err != nil {
		t.Fatalf("解析智谱提交响应失败: %v", err)
	}
	if submitted.TaskID != "8989" {
		t.Fatalf("任务 ID 错误: %q", submitted.TaskID)
	}
	if submitted.Status != spec.VideoStatusProcessing {
		t.Fatalf("状态应为 processing，实际 %q", submitted.Status)
	}

	// 完成：结果在 video_result 里，直接可用，无需再调内容端点
	done, err := ParseVideoStatusResponse(ProtocolOpenAI, []byte(
		`{"id":"8989","task_status":"SUCCESS","video_result":[{"url":"https://cdn.example.com/a.mp4","cover_image_url":"https://cdn.example.com/a.jpg"}]}`))
	if err != nil {
		t.Fatalf("解析智谱完成响应失败: %v", err)
	}
	if done.Status != spec.VideoStatusSucceeded {
		t.Fatalf("状态应为 succeeded，实际 %q", done.Status)
	}
	if len(done.Videos) != 1 || done.Videos[0].URL != "https://cdn.example.com/a.mp4" {
		t.Fatalf("视频地址解析错误: %+v", done.Videos)
	}

	// 失败
	failed, err := ParseVideoStatusResponse(ProtocolOpenAI,
		[]byte(`{"id":"8989","task_status":"FAIL"}`))
	if err != nil {
		t.Fatalf("解析智谱失败响应失败: %v", err)
	}
	if failed.Status != spec.VideoStatusFailed {
		t.Fatalf("状态应为 failed，实际 %q", failed.Status)
	}
}

func TestParseOpenAIVideoResponse(t *testing.T) {
	// 原有 Sora 风格不能被破坏
	res, err := ParseVideoStatusResponse(ProtocolOpenAI,
		[]byte(`{"id":"video_123","object":"video","status":"completed","data":[{"url":"https://x/a.mp4","duration":8}]}`))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if res.Status != spec.VideoStatusSucceeded || len(res.Videos) != 1 {
		t.Fatalf("解析结果错误: %+v", res)
	}
	if res.Videos[0].DurationSec != 8 {
		t.Fatalf("时长应从 data 里取，实际 %d", res.Videos[0].DurationSec)
	}
}

func TestVideoEndpointStyle(t *testing.T) {
	if got := videoEndpointStyle(zhipuBase); got != videoStyleZhipu {
		t.Fatalf("智谱地址应识别为 zhipu，实际 %q", got)
	}
	if got := videoEndpointStyle("https://OPEN.BIGMODEL.CN/api/paas/v4"); got != videoStyleZhipu {
		t.Fatalf("识别应忽略大小写，实际 %q", got)
	}
	if got := videoEndpointStyle(openAIBase); got != videoStyleOpenAI {
		t.Fatalf("普通地址应为 openai，实际 %q", got)
	}
	// 地址里出现域名子串不应误判为独立平台之外的结果
	if !strings.Contains(zhipuBase, "bigmodel.cn") {
		t.Fatal("测试数据本身有误")
	}
}
