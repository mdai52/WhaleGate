package gateway

import (
	"errors"
	"fmt"
	"io"
)

// 探测相关错误。
var (
	ErrEmptyBaseURL = errors.New("上游地址不能为空")
	ErrEmptyAPIKey  = errors.New("上游密钥不能为空")
	ErrNoModels     = errors.New("上游未返回任何模型")
	ErrProbeFailed  = errors.New("未能识别上游协议，请检查地址与密钥")
)

// ProbeError 单次探测失败明细。
type ProbeError struct {
	Type     string
	Endpoint string
	Status   int
	Message  string
}

func (e *ProbeError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s 探测失败（%s，HTTP %d）：%s", e.Type, e.Endpoint, e.Status, e.Message)
	}
	return fmt.Sprintf("%s 探测失败（%s，HTTP %d）", e.Type, e.Endpoint, e.Status)
}

// readLimited 读取并截断响应体，避免超大响应打爆内存。
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, limit))
}
