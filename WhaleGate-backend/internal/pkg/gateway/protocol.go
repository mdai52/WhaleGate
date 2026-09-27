package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

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
	switch channelType {
	case ProtocolGemini:
		action := gemini.ActionGenerateContent
		if stream {
			action = gemini.ActionStreamGenerateContent + "?alt=sse"
		}
		return fmt.Sprintf("%s/models/%s:%s", base, model, action)
	default:
		return base + "/chat/completions"
	}
}

// BuildUpstreamAuthHeader 按渠道协议返回鉴权头键值。
func BuildUpstreamAuthHeader(channelType, apiKey string) (string, string) {
	if channelType == ProtocolGemini {
		return "x-goog-api-key", apiKey
	}
	return "Authorization", "Bearer " + apiKey
}

// BuildUpstreamRequest 把统一请求转换为指定协议的上游请求体。
func BuildUpstreamRequest(channelType string, u *UnifiedRequest) (map[string]interface{}, error) {
	if channelType == ProtocolGemini {
		return gemini.BuildRequest(u)
	}
	return openai.BuildRequest(u)
}

// ParseUpstreamResponse 解析指定协议的上游响应。
func ParseUpstreamResponse(channelType string, raw []byte) (*UnifiedResponse, error) {
	if channelType == ProtocolGemini {
		return gemini.ParseResponse(raw)
	}
	return openai.ParseResponse(raw)
}

// ParseUpstreamStreamEvent 解析指定协议的上游流式事件。
func ParseUpstreamStreamEvent(channelType string, payload []byte) (*StreamChunk, error) {
	if channelType == ProtocolGemini {
		return gemini.ParseStreamEvent(payload)
	}
	return openai.ParseStreamEvent(payload)
}

// ExtractUpstreamError 从上游响应体提取错误信息。
func ExtractUpstreamError(channelType string, raw []byte) string {
	if channelType == ProtocolGemini {
		return gemini.ExtractError(raw)
	}
	return openai.ExtractError(raw)
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
