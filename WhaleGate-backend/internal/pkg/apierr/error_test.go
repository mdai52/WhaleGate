package apierr

import (
	"errors"
	"net/http"
	"testing"
)

func TestDefineHTTPMapping(t *testing.T) {
	cases := []struct {
		code int
		http int
	}{
		{CodeBadRequest, http.StatusBadRequest},
		{CodeUnauthorized, http.StatusUnauthorized},
		{CodeForbidden, http.StatusForbidden},
		{CodeNotFound, http.StatusNotFound},
		{CodeConflict, http.StatusConflict},
		{CodeRateLimited, http.StatusTooManyRequests},
		{CodeInternal, http.StatusInternalServerError},
		{CodeUpstream, http.StatusBadGateway},
		{CodeUpstreamTimeout, http.StatusGatewayTimeout},
	}
	for _, c := range cases {
		eno := Define(c.code, "x")
		if eno.HTTP != c.http {
			t.Errorf("code %d 期望 HTTP %d，实际 %d", c.code, c.http, eno.HTTP)
		}
	}
}

func TestErrorWrapUnwrap(t *testing.T) {
	base := errors.New("底层错误")
	err := Wrap(ErrDatabase, base)
	if !errors.Is(err, base) {
		t.Fatal("应能通过 errors.Is 找到底层错误")
	}
	if !Is(err, ErrDatabase) {
		t.Fatal("Is 应按业务码匹配")
	}
	if Is(err, ErrNotFound) {
		t.Fatal("不同业务码不应匹配")
	}
}

func TestFrom(t *testing.T) {
	if got := From(nil); got != nil {
		t.Fatal("nil 错误应返回 nil")
	}
	known := New(ErrKeyExpired, "过期")
	if got := From(error(known)); got != known {
		t.Fatal("业务错误应原样返回")
	}
	unknown := From(errors.New("boom"))
	if unknown.Code != CodeInternal {
		t.Fatalf("未知错误应映射为内部错误，实际 %d", unknown.Code)
	}
}

func TestCodeOf(t *testing.T) {
	if CodeOf(nil) != CodeOK {
		t.Fatal("nil 应为成功码")
	}
	if CodeOf(New(ErrQuotaExceeded, "")) != CodeQuotaExceeded {
		t.Fatal("业务码取值错误")
	}
}

func TestErrorfAndDetail(t *testing.T) {
	err := Errorf(ErrInvalidParam, "字段 %s 非法", "name")
	if err.Detail != "字段 name 非法" {
		t.Fatalf("明细拼接错误: %s", err.Detail)
	}
	if err.Error() == "" {
		t.Fatal("Error() 不应为空")
	}
}
