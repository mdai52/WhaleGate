// 提示词 AI 扩写：把一句话想法补全成可直接用于生图模型的提示词。
//
// 与脚本创作同口径计费（按 token 预扣+结算），并写入调用历史，
// 因为走的同样是「对话渠道 → 上游模型」的真实成本。
package service

import (
	"context"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
)

const promptEnhanceSystemPrompt = `你是一名专业的绘画提示词工程师。用户会给出一段简短的画面想法，请把它扩写成一段可直接用于文生图模型的提示词：

要求：
1. 补全主体、动作、环境、光线、构图、镜头视角与艺术风格，让画面具体、可生成
2. 只输出优化后的提示词本身，不要任何解释、前缀、标题或引号
3. 使用与输入相同的语言；输入是中文就输出中文，输入是英文就输出英文`

// PromptEnhanceInput 提示词扩写入参。
type PromptEnhanceInput struct {
	// Model 用于扩写的对话模型。
	Model string
	// Prompt 用户的原始想法，可以是几个词。
	Prompt string
}

// EnhancePrompt 把简短想法扩写成完整生图提示词，返回纯文本。
func (c *Container) EnhancePrompt(
	ctx context.Context,
	userID uint,
	in PromptEnhanceInput,
	meta *ImageCallMeta,
) (string, error) {
	in.Model = strings.TrimSpace(in.Model)
	in.Prompt = strings.TrimSpace(in.Prompt)
	if in.Model == "" {
		return "", apierr.New(apierr.ErrInvalidParam, "请选择用于优化提示词的模型")
	}
	if in.Prompt == "" {
		return "", apierr.New(apierr.ErrInvalidParam, "请先写一点画面想法，哪怕是几个词")
	}

	messages := []spec.Message{
		{Role: spec.RoleSystem, Content: promptEnhanceSystemPrompt},
		{Role: spec.RoleUser, Content: in.Prompt},
	}

	selfUse := c.SelfUseMode()
	var reservation *Reservation
	if !selfUse && userID > 0 {
		points, err := c.PointsFor(ctx, in.Model, c.EstimateUsage(&spec.UnifiedRequest{
			Model: in.Model, Messages: messages,
		}), 1)
		if err == nil {
			reservation, err = c.ReserveQuota(ctx, userID, points)
		}
		if err != nil {
			return "", err
		}
	}

	result, err := c.CompleteText(ctx, TextCompleteInput{
		Model:     in.Model,
		Messages:  messages,
		MaxTokens: 1024,
	})
	if err != nil {
		if reservation != nil {
			reservation.Release(context.Background())
		}
		return "", err
	}

	text := strings.TrimSpace(stripThinkBlocks(result.Text))
	text = strings.Trim(text, "\"“”「」'‘’`")
	if text == "" {
		if reservation != nil {
			reservation.Release(context.Background())
		}
		return "", apierr.New(apierr.ErrUpstreamInvalid, "模型没有返回有效内容，请换一个模型或重试")
	}

	points, err := c.PointsFor(ctx, in.Model, result.Usage, 1)
	if err != nil {
		points = 0
		if c.Logger != nil {
			c.Logger.Warn("计算提示词扩写点数失败", zap.Error(err))
		}
	}
	if reservation != nil {
		reservation.Settle(ctx, points)
	}
	if keyID := meta.KeyID(); keyID > 0 {
		c.UpdateAPIKeyUsage(ctx, keyID, result.Usage.TotalTokens, points)
	}

	if c.Logs != nil {
		c.Logs.Submit(&model.CallLog{
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
	}
	return text, nil
}
