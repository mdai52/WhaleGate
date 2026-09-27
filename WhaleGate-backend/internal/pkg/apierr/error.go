package apierr

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// Error 是携带业务码与可选明细的错误。
type Error struct {
	// Errno 内嵌标准错误码定义。
	Errno
	// Detail 面向排查的补充说明，不直接暴露给最终用户时可为空。
	Detail string `json:"detail,omitempty"`
	err    error
}

// New 创建带明细的业务错误。
func New(eno Errno, detail string) *Error {
	return &Error{Errno: eno, Detail: detail}
}

// Wrap 用业务码包装底层错误。
func Wrap(eno Errno, err error) *Error {
	return &Error{Errno: eno, err: err}
}

// Wrapf 用业务码包装底层错误并附加格式化说明。
func Wrapf(eno Errno, err error, format string, args ...interface{}) *Error {
	return &Error{Errno: eno, Detail: fmt.Sprintf(format, args...), err: err}
}

// Errorf 构造仅带格式化明细的业务错误。
func Errorf(eno Errno, format string, args ...interface{}) *Error {
	return &Error{Errno: eno, Detail: fmt.Sprintf(format, args...)}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.err != nil {
		if e.Detail != "" {
			return fmt.Sprintf("%s: %s: %v", e.Message, e.Detail, e.err)
		}
		return fmt.Sprintf("%s: %v", e.Message, e.err)
	}
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.Detail)
	}
	return e.Message
}

// Unwrap 返回被包装的原始错误。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

// WithDetail 追加明细，返回新错误。
func (e *Error) WithDetail(detail string) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.Detail = detail
	return &cp
}

// Is 判断 err 是否为指定业务码。
func Is(err error, eno Errno) bool {
	if err == nil {
		return false
	}
	var be *Error
	if errors.As(err, &be) {
		return be.Code == eno.Code
	}
	return false
}

// From 将任意错误归一化为 *Error，未知错误映射为 ErrInternal。
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var be *Error
	if errors.As(err, &be) {
		return be
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Wrap(ErrNotFound, err)
	}
	return Wrap(ErrInternal, err)
}

// CodeOf 返回错误对应的业务码，非业务错误返回 CodeInternal。
func CodeOf(err error) int {
	if err == nil {
		return CodeOK
	}
	return From(err).Code
}
