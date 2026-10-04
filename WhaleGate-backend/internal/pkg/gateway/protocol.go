package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/whalegate/whalegate/internal/pkg/gateway/anthropic"
	"github.com/whalegate/whalegate/internal/pkg/gateway/gemini"
	"github.com/whalegate/whalegate/internal/pkg/gateway/openai"
)

// ParseClientRequest 按客户端协议解析入站请求体。
// Gemini 的模型名来自 URL 路径，因此额外接收 model 参数。
func ParseClientRequest(clientProtocol string, raw []byte, model string) (*UnifiedRequest, error) {
	if clientProtocol == ProtocolGemini {
		return gemini.ParseRequest(raw, model)
	}
	return openai.ParseRequest(raw)
}

// ---------------------------------------------------------------- 上游侧

// BuildUpstreamURL 按渠道协议拼装上游地址。
func BuildUpstreamURL(channelType, baseURL, model string, stream bool) string {
	base := strings.TrimRight(baseURL, "/")
	switch {
	case channelType == ProtocolGemini:
		action := gemini.ActionGenerateContent
		if stream {
			action = gemini.ActionStreamGenerateContent + "?alt=sse"
		}
		return fmt.Sprintf("%s/models/%s:%s", base, model, action)
	case IsAnthropicLike(channelType):
		// Anthropic 流式与非流式共用同一端点，流标记写在请求体里。
		return anthropic.MessagesURL(baseURL)
	default:
		return base + "/chat/completions"
	}
}

// AnthropicModelsURL 返回 Anthropic 系渠道的模型列表地址，供连通性测试使用。
func AnthropicModelsURL(baseURL string) string {
	return anthropic.ModelsURL(baseURL)
}

// BuildUpstreamAuthHeader 按渠道协议返回主鉴权头键值。
func BuildUpstreamAuthHeader(channelType, apiKey string) (string, string) {
	if channelType == ProtocolGemini {
		return "x-goog-api-key", apiKey
	}
	if IsAnthropicLike(channelType) {
		if channelType == ProtocolClaudeCode {
			// Claude Code 网关沿用 Bearer，其余 Anthropic 系用 x-api-key。
			return anthropic.BearerHeader, "Bearer " + apiKey
		}
		return anthropic.APIKeyHeader, apiKey
	}
	return "Authorization", "Bearer " + apiKey
}

// BuildUpstreamHeaders 返回发往上游所需的全部请求头（含协议版本头）。
// 相比 BuildUpstreamAuthHeader，它还能携带 anthropic-version 这类必需附加头。
func BuildUpstreamHeaders(channelType, apiKey string) map[string]string {
	headers := map[string]string{}
	if IsAnthropicLike(channelType) {
		name, value := BuildUpstreamAuthHeader(channelType, apiKey)
		headers[name] = value
		headers["anthropic-version"] = anthropic.APIVersion
		return headers
	}
	name, value := BuildUpstreamAuthHeader(channelType, apiKey)
	headers[name] = value
	return headers
}

// BuildUpstreamRequest 把统一请求转换为指定协议的上游请求体。
func BuildUpstreamRequest(channelType string, u *UnifiedRequest) (map[string]interface{}, error) {
	switch {
	case channelType == ProtocolGemini:
		return gemini.BuildRequest(u)
	case IsAnthropicLike(channelType):
		return anthropic.BuildRequest(u)
	default:
		return openai.BuildRequest(u)
	}
}

// ParseUpstreamResponse 解析指定协议的上游响应。
func ParseUpstreamResponse(channelType string, raw []byte) (*UnifiedResponse, error) {
	switch {
	case channelType == ProtocolGemini:
		return gemini.ParseResponse(raw)
	case IsAnthropicLike(channelType):
		return anthropic.ParseResponse(raw)
	default:
		return openai.ParseResponse(raw)
	}
}

// ParseUpstreamStreamEvent 解析指定协议的上游流式事件。
//
// 注意：Anthropic 的工具调用参数是跨事件分片的，单次调用无法还原完整参数，
// 转发场景应使用 NewStreamDecoder 在整个流上持有同一个解码器。
func ParseUpstreamStreamEvent(channelType string, payload []byte) (*StreamChunk, error) {
	switch {
	case channelType == ProtocolGemini:
		return gemini.ParseStreamEvent(payload)
	case IsAnthropicLike(channelType):
		return anthropic.ParseStreamEvent(payload)
	default:
		return openai.ParseStreamEvent(payload)
	}
}

// ExtractUpstreamError 从上游响应体提取错误信息。
func ExtractUpstreamError(channelType string, raw []byte) string {
	switch {
	case channelType == ProtocolGemini:
		return gemini.ExtractError(raw)
	case IsAnthropicLike(channelType):
		return anthropic.ExtractError(raw)
	default:
		return openai.ExtractError(raw)
	}
}

// ---------------------------------------------------------------- 客户端侧

// WriteClientResponse 按客户端协议输出非流式响应。
func WriteClientResponse(clientProtocol string, c *gin.Context, resp *UnifiedResponse) {
	if clientProtocol == ProtocolGemini {
		c.JSON(http.StatusOK, gemini.BuildResponse(resp))
		return
	}
	c.JSON(http.StatusOK, openai.BuildResponse(resp))
}

// WriteClientError 按客户端协议输出错误。
func WriteClientError(clientProtocol string, c *gin.Context, status int, message string) {
	if clientProtocol == ProtocolGemini {
		body := gemini.ErrorResponse(message, http.StatusText(status))
		c.JSON(status, body)
		return
	}
	c.JSON(status, openai.ErrorResponse(message, "server_error"))
}

// WriteClientStreamHeader 设置 SSE 响应头。
func WriteClientStreamHeader(clientProtocol string, c *gin.Context) {
	c.Writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	if clientProtocol == ProtocolGemini {
		c.Writer.Header().Set("Content-Type", "text/event-stream")
	}
	c.Writer.WriteHeader(http.StatusOK)
}

// WriteClientStreamChunk 按客户端协议写出一个流式块。
func WriteClientStreamChunk(clientProtocol string, w io.Writer, chunk *StreamChunk) error {
	var (
		data []byte
		err  error
	)
	if clientProtocol == ProtocolGemini {
		data, err = gemini.BuildStreamEvent(chunk)
	} else {
		data, err = openai.BuildStreamEvent(chunk)
	}
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// WriteClientStreamTail 输出流式结束标记（Gemini 无需）。
func WriteClientStreamTail(clientProtocol string, w io.Writer) error {
	if clientProtocol == ProtocolGemini {
		return nil
	}
	_, err := w.Write(openai.DoneEvent())
	return err
}

// WriteClientStreamError 在流式中输出错误块（流已开始时使用）。
func WriteClientStreamError(clientProtocol string, w io.Writer, message string) error {
	if clientProtocol == ProtocolGemini {
		data, err := json.Marshal(gemini.ErrorResponse(message, "INTERNAL"))
		if err != nil {
			return err
		}
		_, err = w.Write(append(append([]byte("data: "), data...), '\r', '\n', '\r', '\n'))
		return err
	}
	data, err := json.Marshal(openai.ErrorResponse(message, "server_error"))
	if err != nil {
		return err
	}
	_, err = w.Write(append(append([]byte("data: "), data...), '\n', '\n'))
	return err
}
