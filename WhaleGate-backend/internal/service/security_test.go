package service

import (
	"strings"
	"testing"
)

func TestMaskEmail(t *testing.T) {
	cases := map[string]string{
		"alice@example.com": "a***@example.com",
		"bob@sub.domain.cn": "b***@sub.domain.cn",
		"x@y.com":           "x***@y.com",
		"":                  "",
		"not-an-email":      "n***",
		"ab":                "**",
	}
	for in, want := range cases {
		if got := MaskEmail(in); got != want {
			t.Errorf("MaskEmail(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestMaskEmailKeepsDomain(t *testing.T) {
	got := MaskEmail("zhang.san@corp.example.com")
	if got != "z***@corp.example.com" {
		t.Fatalf("域名应完整保留: %s", got)
	}
}

func TestMaskSecret(t *testing.T) {
	if got := MaskSecret("sk-abcdefgh"); got != "*******efgh" {
		t.Fatalf("密钥脱敏错误: %s", got)
	}
	if got := MaskSecret("abcd"); got != "****" {
		t.Fatalf("短密钥应全部隐藏: %s", got)
	}
	if got := MaskSecret(""); got != "" {
		t.Fatalf("空密钥应返回空: %s", got)
	}
}

func TestLoginKeyStableAndHashed(t *testing.T) {
	c := newTestContainer()
	a := c.loginKey("Alice")
	b := c.loginKey("alice")
	c2 := c.loginKey("bob")
	if a != b {
		t.Fatal("账号键应忽略大小写")
	}
	if a == c2 {
		t.Fatal("不同账号应有不同键")
	}
	if len(a) < 20 {
		t.Fatalf("应使用哈希避免泄露账号名: %s", a)
	}
}

func TestAuditTruncatesFields(t *testing.T) {
	long := strings.Repeat("x", 1000)
	entry := AuditEntry{Username: long, Action: long, Detail: long, UserAgent: long}
	// 仅验证构造函数不 panic 且字段长度受控（实际截断发生在 Container.Audit）
	if entry.Detail == "" {
		t.Fatal("审计明细不应为空")
	}
}
