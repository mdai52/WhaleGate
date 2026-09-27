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

// NewUpstream 创建上游调用器。
func NewUpstream(timeout time.Duration) *Upstream {
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	return &Upstream{Client: &http.Client{
		Timeout:   timeout,
		Transport: DefaultTransport(),
	}}
}

// DefaultTransport 返回针对网关场景调优的连接池配置。
func DefaultTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        512,
		MaxIdleConnsPerHost: 64,
		MaxConnsPerHost:     256,
		IdleConnTimeout:     90 * time.Second,
		DisableCompression:  true,
		ForceAttemptHTTP2:   true,
	}
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
