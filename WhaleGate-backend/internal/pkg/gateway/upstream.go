package gateway

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Upstream 封装对上游渠道的 HTTP 调用。
type Upstream struct {
	// Client 底层 HTTP 客户端，超时由渠道配置决定。
	Client *http.Client
}

// sharedTransport 进程内共享的连接池。
//
// 之前每次请求都 new 一个 Transport：连接池里永远只有一条连接，
// MaxIdleConns 之类的调优完全失效，请求结束后连接与其读协程还要存活到 IdleConnTimeout，
// QPS 一上去就是 socket 与 goroutine 的持续放大。
var sharedTransport = &http.Transport{
	MaxIdleConns:        512,
	MaxIdleConnsPerHost: 64,
	MaxConnsPerHost:     256,
	IdleConnTimeout:     90 * time.Second,
	DisableCompression:  true,
	ForceAttemptHTTP2:   true,
}

// NewUpstream 创建上游调用器。
//
// Client 本身很轻，按请求创建没有问题；真正需要复用的是 Transport，这里统一用共享实例。
func NewUpstream(timeout time.Duration) *Upstream {
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &Upstream{Client: &http.Client{
		Timeout:   timeout,
		Transport: sharedTransport,
	}}
}

// DefaultTransport 返回全局共享的连接池配置。
//
// 保留此函数仅为兼容既有调用：返回值就是共享实例，调用方不应修改它。
func DefaultTransport() *http.Transport {
	return sharedTransport
}

// Do 发起一次性请求并返回原始响应，调用方负责关闭 Body。
func (u *Upstream) Do(ctx context.Context, method, url string, headers map[string]string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造上游请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用上游失败: %w", err)
	}
	return resp, nil
}

// DoStream 发起流式请求，返回原始响应（Body 未关闭）。
func (u *Upstream) DoStream(ctx context.Context, method, url string, headers map[string]string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("构造上游流式请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	for k, v := range headers {
		if v != "" {
			req.Header.Set(k, v)
		}
	}

	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用上游流式接口失败: %w", err)
	}
	return resp, nil
}

// maxSSEEventSize 单个 SSE 事件的最大载荷（1MB）。
//
// 事件以空行结束，上游若不发空行，缓冲区会无上限增长；
// 流式请求是直接读上游响应体的，必须自己设上限（非流式路径都有 io.LimitReader 兜底）。
const maxSSEEventSize = 1 << 20

// SSEScanner 逐事件读取 SSE 流，兼容 \n\n 与 \r\n\r\n 两种分隔。
type SSEScanner struct {
	reader *bufio.Reader
	buf    bytes.Buffer
}

// NewSSEScanner 创建 SSE 扫描器。
func NewSSEScanner(r io.Reader) *SSEScanner {
	return &SSEScanner{reader: bufio.NewReaderSize(r, 32*1024)}
}

// Next 返回下一个事件的 data 载荷（不含 "data:" 前缀），流结束时返回 io.EOF。
func (s *SSEScanner) Next() ([]byte, error) {
	s.buf.Reset()
	for {
		line, err := s.reader.ReadString('\n')
		trimmed := bytes.TrimRight([]byte(line), "\r\n")

		if len(trimmed) == 0 {
			if s.buf.Len() > 0 {
				return append([]byte(nil), s.buf.Bytes()...), nil
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					return nil, io.EOF
				}
				return nil, err
			}
			continue
		}

		if bytes.HasPrefix(trimmed, []byte("data:")) {
			payload := bytes.TrimSpace(trimmed[5:])
			if s.buf.Len() > 0 {
				s.buf.WriteByte('\n')
			}
			s.buf.Write(payload)
			if s.buf.Len() > maxSSEEventSize {
				return nil, fmt.Errorf("SSE 单事件超过 %d 字节，已中断读取", maxSSEEventSize)
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				if s.buf.Len() > 0 {
					return append([]byte(nil), s.buf.Bytes()...), nil
				}
				return nil, io.EOF
			}
			return nil, err
		}
	}
}
