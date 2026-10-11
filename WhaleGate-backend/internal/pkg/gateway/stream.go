package gateway

import (
	"github.com/whalegate/whalegate/internal/pkg/gateway/anthropic"
	"github.com/whalegate/whalegate/internal/pkg/gateway/spec"
	"github.com/whalegate/whalegate/internal/pkg/gateway/vendor"
)

// StreamDecoder 流式事件解码器。
//
// 大多数协议（OpenAI / Gemini）的事件是自包含的，stateless 即可；
// 但 Anthropic 的工具调用参数通过 input_json_delta 分片下发，必须跨事件累积，
// 因此需要按请求持有同一实例（见 NewStreamDecoder）。
type StreamDecoder interface {
	// Decode 解码一个 SSE data 载荷；返回 nil 表示该事件无需下发客户端。
	Decode(payload []byte) (*spec.StreamChunk, error)
}

// statelessDecoder 把无状态解析函数包装成 StreamDecoder。
type statelessDecoder struct {
	channelType string
	vendor      *vendor.Vendor
}

// Decode 调用协议对应的无状态解析器。
func (d statelessDecoder) Decode(payload []byte) (*spec.StreamChunk, error) {
	return ParseUpstreamStreamEvent(d.channelType, payload, d.vendor)
}

// NewStreamDecoder 创建一次转发所需的流式解码器。
//
// v 为厂商档案：OpenAI 兼容厂商的思维链字段名不同，解码时要按厂商取值。
func NewStreamDecoder(channelType string, v *vendor.Vendor) StreamDecoder {
	if IsAnthropicLike(channelType) {
		return anthropic.NewStreamDecoder()
	}
	return statelessDecoder{channelType: channelType, vendor: v}
}
