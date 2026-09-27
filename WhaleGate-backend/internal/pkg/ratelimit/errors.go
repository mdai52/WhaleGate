package ratelimit

import "errors"

// ErrConcurrencyExceeded 表示并发额度已耗尽。
var ErrConcurrencyExceeded = errors.New("并发请求数超出限制")
