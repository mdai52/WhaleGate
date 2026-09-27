// Package gateway 聚合协议适配能力：统一格式类型（见 spec 子包）、
// 上游调用、SSE 解析，以及按协议分发的编解码入口。
//
// spec 子包只描述数据结构，不依赖任何适配器，从而避免循环依赖。
package gateway

import "github.com/whalegate/whalegate/internal/pkg/gateway/spec"

// 统一格式类型别名，便于调用方只 import 本包。
type (
	// UnifiedRequest 统一请求。
	UnifiedRequest = spec.UnifiedRequest
	// UnifiedResponse 统一非流式响应。
	UnifiedResponse = spec.UnifiedResponse
	// StreamChunk 统一流式响应块。
	StreamChunk = spec.StreamChunk
	// StreamChoice 流式候选。
	StreamChoice = spec.StreamChoice
	// Choice 非流式候选。
	Choice = spec.Choice
	// Message 统一消息。
	Message = spec.Message
	// Delta 流式增量。
	Delta = spec.Delta
	// ToolCall 工具调用。
	ToolCall = spec.ToolCall
	// ToolDefinition 工具定义。
	ToolDefinition = spec.ToolDefinition
	// Usage token 用量。
	Usage = spec.Usage
)

// 统一格式常量别名。
const (
	ProtocolOpenAI = spec.ProtocolOpenAI
	ProtocolGemini = spec.ProtocolGemini

	RoleSystem    = spec.RoleSystem
	RoleUser      = spec.RoleUser
	RoleAssistant = spec.RoleAssistant
	RoleTool      = spec.RoleTool

	FinishStop      = spec.FinishStop
	FinishLength    = spec.FinishLength
	FinishToolCalls = spec.FinishToolCalls
)

// FinishReasonPtr 便捷构造结束原因指针。
var FinishReasonPtr = spec.FinishReasonPtr
