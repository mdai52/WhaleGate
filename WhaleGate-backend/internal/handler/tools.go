package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
	"github.com/whalegate/whalegate/internal/pkg/mcp"
	"github.com/whalegate/whalegate/internal/pkg/util"
	"github.com/whalegate/whalegate/internal/service"
)

// 工具角色常量。
const (
	roleTool = "tool"
)

// applyEnhancements 在转发前注入技能提示词与 MCP 工具定义。
// 返回是否启用了"网关代执行"（任一 Server 开启 auto_execute 且请求非流式）。
func (h *RelayHandler) applyEnhancements(c *gin.Context, req *gateway.UnifiedRequest) (bool, int) {
	cfg := h.svc.Settings()

	if cfg.SkillsEnabled {
		h.injectSkills(c.Request.Context(), req)
	}
	if !cfg.MCPEnabled {
		return false, 0
	}

	tools, err := h.svc.ListMCPTools(c.Request.Context())
	if err != nil {
		if h.svc.Logger != nil {
			h.svc.Logger.Warn("读取 MCP 工具失败", zap.Error(err))
		}
		return false, 0
	}
	if len(tools) == 0 {
		return false, 0
	}

	// 转换为统一工具定义并追加（客户端自带的 tools 优先保留）
	defs := make([]spec.ToolDefinition, 0, len(tools))
	auto := false
	for _, t := range tools {
		defs = append(defs, spec.ToolDefinition{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Schema,
		})
		if t.AutoExecute {
			auto = true
		}
	}
	req.Tools = append(req.Tools, defs...)

	if h.svc.Logger != nil {
		h.svc.Logger.Debug("已注入 MCP 工具", zap.Int("count", len(defs)), zap.Bool("auto_execute", auto))
	}
	// 流式响应无法在网关侧中断续接，代执行仅对非流式生效
	return auto && !req.Stream, cfg.MCPMaxRounds
}

// injectSkills 把技能提示词并入 system 消息。
func (h *RelayHandler) injectSkills(ctx context.Context, req *gateway.UnifiedRequest) {
	prompt := h.svc.BuildSkillPrompt(ctx)
	if prompt == "" {
		return
	}
	for i := range req.Messages {
		if req.Messages[i].Role == spec.RoleSystem {
			req.Messages[i].Content = strings.TrimSpace(req.Messages[i].Content) + "\n\n" + prompt
			return
		}
	}
	// 没有 system 消息时插到最前面
	req.Messages = append([]spec.Message{{Role: spec.RoleSystem, Content: prompt}}, req.Messages...)
}

// collectToolCalls 提取响应中的工具调用。
func collectToolCalls(resp *spec.UnifiedResponse) []spec.ToolCall {
	if resp == nil {
		return nil
	}
	var out []spec.ToolCall
	for _, ch := range resp.Choices {
		out = append(out, ch.Message.ToolCalls...)
	}
	return out
}

// forwardWithToolLoop 网关代执行多轮工具循环：
// 模型返回 tool_calls → 调用 MCP → 结果回喂 → 再次请求，直到无工具调用或达到轮次上限。
// 计量按各轮累加，最终只把最后一轮响应写回客户端。
func (h *RelayHandler) forwardWithToolLoop(
	ctx context.Context,
	c *gin.Context,
	clientProtocol string,
	channel *model.Channel,
	req *gateway.UnifiedRequest,
	maxRounds int,
) (*relayOutcome, int, error) {
	if maxRounds <= 0 {
		maxRounds = defaultToolRounds
	}

	total := &spec.Usage{}
	current := *req
	rounds := 0
	var lastReport gateway.ParamReport
	var upstreamModel string

	for round := 0; round <= maxRounds; round++ {
		resp, report, modelName, err := h.fetchUpstream(ctx, c, clientProtocol, channel, &current)
		if err != nil {
			// 已执行过工具轮次时，把累计用量一并返回，避免计量丢失
			return &relayOutcome{Usage: total, Params: &lastReport, UpstreamModel: upstreamModel}, rounds, err
		}
		lastReport = report
		upstreamModel = modelName
		rounds++

		if resp.Usage != nil {
			total.Add(resp.Usage)
		}

		calls := collectToolCalls(resp)
		if len(calls) == 0 || round == maxRounds {
			gateway.WriteClientResponse(clientProtocol, c, resp)
			outcome := &relayOutcome{Usage: total, Params: &report, UpstreamModel: modelName}
			return outcome, rounds, nil
		}

		// 追加助手的工具调用消息与工具结果
		current.Messages = append(current.Messages, spec.Message{
			Role:      spec.RoleAssistant,
			Content:   resp.Text(),
			ToolCalls: calls,
		})
		for _, call := range calls {
			text, err := h.executeTool(ctx, call)
			if err != nil && h.svc.Logger != nil {
				h.svc.Logger.Warn("MCP 工具调用失败",
					zap.String("tool", call.Name), zap.Error(err))
			}
			current.Messages = append(current.Messages, spec.Message{
				Role:       roleTool,
				ToolCallID: call.ID,
				Content:    text,
			})
		}
	}

	if errors.Is(ctx.Err(), context.Canceled) {
		return &relayOutcome{Usage: total, Params: &lastReport, UpstreamModel: upstreamModel}, rounds, ctx.Err()
	}
	return &relayOutcome{Usage: total, Params: &lastReport, UpstreamModel: upstreamModel}, rounds, nil
}

// defaultToolRounds 默认最大工具轮次。
const defaultToolRounds = 5

// executeTool 按调用名路由到 MCP Server，返回结果文本（失败时返回错误描述以回喂模型）。
func (h *RelayHandler) executeTool(ctx context.Context, call spec.ToolCall) (string, error) {
	args := map[string]interface{}{}
	if strings.TrimSpace(call.Arguments) != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return "工具参数解析失败：" + err.Error(), err
		}
	}
	text, _, err := h.svc.CallMCPTool(ctx, call.Name, args)
	if err != nil {
		if text != "" {
			return text, err
		}
		return "工具调用失败：" + err.Error(), err
	}
	if text == "" {
		text = "（工具无返回内容）"
	}
	return text, nil
}

// prepareUpstream 构建一次上游请求所需的全部材料，供单次转发与多轮循环复用。
func (h *RelayHandler) prepareUpstream(
	ctx context.Context,
	c *gin.Context,
	channel *model.Channel,
	req *gateway.UnifiedRequest,
) (string, map[string]string, []byte, string, gateway.ParamReport, error) {
	apiKey, err := h.svc.ChannelUpstreamKey(ctx, channel)
	if err != nil {
		return "", nil, nil, "", gateway.ParamReport{}, err
	}

	upstreamModel, override := channel.ResolveModel(req.Model)
	upstreamReq := *req
	upstreamReq.Model = upstreamModel

	payload, err := gateway.BuildUpstreamRequest(channel.Type, &upstreamReq)
	if err != nil {
		return "", nil, nil, "", gateway.ParamReport{}, apierr.Wrap(apierr.ErrInvalidParam, err)
	}

	report := gateway.ApplyParams(payload, channel.Type, gateway.CollectParams(req), channel.Schema())
	if len(report.Applied) > 0 {
		c.Header(headerParamsApplied, strings.Join(report.Applied, ","))
	}
	if len(report.Dropped) > 0 {
		c.Header(headerParamsDropped, strings.Join(report.Dropped, ","))
	}
	if len(override) > 0 {
		// override-raw：强制追加 tools 或改写任意字段，优先级最高
		payload = util.MergeMap(payload, override)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, nil, "", report, apierr.Wrap(apierr.ErrInternal, err)
	}

	url := gateway.BuildUpstreamURL(channel.Type, channel.BaseURL, upstreamModel, req.Stream)
	headerName, headerValue := gateway.BuildUpstreamAuthHeader(channel.Type, apiKey)
	headers := map[string]string{headerName: headerValue}
	return url, headers, body, upstreamModel, report, nil
}

// fetchUpstream 发起一次非流式请求并解析为统一响应，不写客户端。
func (h *RelayHandler) fetchUpstream(
	ctx context.Context,
	c *gin.Context,
	clientProtocol string,
	channel *model.Channel,
	req *gateway.UnifiedRequest,
) (*spec.UnifiedResponse, gateway.ParamReport, string, error) {
	url, headers, body, upstreamModel, report, err := h.prepareUpstream(ctx, c, channel, req)
	if err != nil {
		return nil, report, "", err
	}

	upstream := gateway.NewUpstream(channelTimeout(h.svc, channel))
	resp, err := upstream.Do(ctx, http.MethodPost, url, headers, body)
	if err != nil {
		return nil, report, upstreamModel, classifyUpstreamError(err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, report, upstreamModel, classifyUpstreamError(err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, report, upstreamModel, upstreamHTTPError(channel.Type, resp.StatusCode, raw)
	}

	unified, err := gateway.ParseUpstreamResponse(channel.Type, raw)
	if err != nil {
		return nil, report, upstreamModel, apierr.Wrap(apierr.ErrUpstreamInvalid, err)
	}
	unified.Model = reqModel(channel, unified.Model)
	return unified, report, upstreamModel, nil
}

// mcp 包引用保持：工具结果结构在注册表中归一化。
var _ = mcp.CallResult{}
var _ = service.ExposedTool{}
