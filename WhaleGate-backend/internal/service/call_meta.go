package service

import "github.com/whalegate/whalegate/internal/model"

// CallMeta 一次调用的可观测上下文，由调用方从 HTTP 上下文中提取。
//
// 工作台（绘图 / 视频 / 画布）与网关出口都要落调用日志，
// 因此这份上下文是共用的，不专属某一种能力。
type CallMeta struct {
	// UserID 归属用户。
	UserID uint
	// APIKeyID 通过 API Key 调用时为密钥 ID，工作台调用为 0。
	APIKeyID uint
	// TraceID 链路追踪 ID。
	TraceID string
	// ClientIP 客户端真实 IP。
	ClientIP string
	// RequestScheme 入站协议：http / https。
	RequestScheme string
}

// ImageCallMeta 旧名，保留别名以免调用方跟着改名。
type ImageCallMeta = CallMeta

// KeyID 取密钥 ID；Meta 缺失时返回 0。
func (m *CallMeta) KeyID() uint {
	if m == nil {
		return 0
	}
	return m.APIKeyID
}

// UserIDOf 取归属用户；Meta 缺失时返回 0。
func (m *CallMeta) UserIDOf() uint {
	if m == nil {
		return 0
	}
	return m.UserID
}

// applyTo 把上下文填进调用日志；Meta 缺失时不覆盖已有值。
func (m *CallMeta) applyTo(entry *model.CallLog) {
	if m == nil || entry == nil {
		return
	}
	entry.UserID = m.UserID
	entry.APIKeyID = m.APIKeyID
	entry.TraceID = m.TraceID
	entry.ClientIP = m.ClientIP
	entry.RequestScheme = m.RequestScheme
}
