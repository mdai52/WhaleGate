package service

import (
	"testing"
	"time"

	"github.com/whalegate/whalegate/internal/config"
	"github.com/whalegate/whalegate/internal/constant"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
)

func TestKeyIdentityUsableNow(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()

	cases := []struct {
		name string
		idn  *KeyIdentity
		want bool
	}{
		{"正常", &KeyIdentity{KeyID: 1, Status: 1, UserStatus: 1}, true},
		{"密钥不存在", &KeyIdentity{}, false},
		{"密钥已禁用", &KeyIdentity{KeyID: 1, Status: 2, UserStatus: 1}, false},
		{"密钥已吊销", &KeyIdentity{KeyID: 1, Status: 1, UserStatus: 1, Revoked: true}, false},
		{"用户已禁用", &KeyIdentity{KeyID: 1, Status: 1, UserStatus: 2}, false},
		{"未过期", &KeyIdentity{KeyID: 1, Status: 1, UserStatus: 1, ExpiresAtUnix: future}, true},
		{"已过期", &KeyIdentity{KeyID: 1, Status: 1, UserStatus: 1, ExpiresAtUnix: past}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.idn.UsableNow(); got != c.want {
				t.Fatalf("期望 %v，实际 %v", c.want, got)
			}
		})
	}
}

func TestKeyIdentityRejectErrno(t *testing.T) {
	future := time.Now().Add(time.Hour).Unix()
	past := time.Now().Add(-time.Hour).Unix()

	cases := []struct {
		name string
		idn  *KeyIdentity
		want int
	}{
		{"可用", &KeyIdentity{KeyID: 1, Status: 1, UserStatus: 1}, apierr.CodeOK},
		{"不存在", &KeyIdentity{}, apierr.CodeInvalidCredential},
		{"已吊销", &KeyIdentity{KeyID: 1, Status: 1, UserStatus: 1, Revoked: true}, apierr.CodeKeyRevoked},
		{"已禁用", &KeyIdentity{KeyID: 1, Status: 2, UserStatus: 1}, apierr.CodeKeyRevoked},
		{"已过期", &KeyIdentity{KeyID: 1, Status: 1, UserStatus: 1, ExpiresAtUnix: past}, apierr.CodeKeyExpired},
		{"用户禁用", &KeyIdentity{KeyID: 1, Status: 1, UserStatus: 2}, apierr.CodeUserDisabled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.idn.RejectErrno().Code; got != c.want {
				t.Fatalf("期望码 %d，实际 %d", c.want, got)
			}
		})
	}
	if c := (&KeyIdentity{KeyID: 1, Status: 1, UserStatus: 1, ExpiresAtUnix: future}).RejectErrno(); c.Code != apierr.CodeOK {
		t.Fatal("未过期密钥不应被拒绝")
	}
}

func TestKeyCacheTTL(t *testing.T) {
	c := &Container{Config: &config.Config{Security: config.SecurityConfig{JWTTTL: time.Hour}}}

	if got := c.keyCacheTTL(&KeyIdentity{KeyID: 1}); got != 24*time.Hour {
		t.Fatalf("无过期时间应为 24h，实际 %v", got)
	}
	if got := c.keyCacheTTL(&KeyIdentity{}); got != 30*time.Second {
		t.Fatalf("不存在的密钥应负缓存 30s，实际 %v", got)
	}
	soon := time.Now().Add(2 * time.Minute).Unix()
	if got := c.keyCacheTTL(&KeyIdentity{KeyID: 1, ExpiresAtUnix: soon}); got > 3*time.Minute {
		t.Fatalf("缓存时长不应超过过期时间: %v", got)
	}
	past := time.Now().Add(-time.Hour).Unix()
	if got := c.keyCacheTTL(&KeyIdentity{KeyID: 1, ExpiresAtUnix: past}); got != time.Minute {
		t.Fatalf("已过期时应退化为最小 TTL，实际 %v", got)
	}
}

func TestPrefix(t *testing.T) {
	c := &Container{Config: &config.Config{Redis: config.RedisConfig{KeyPrefix: "wgtest"}}}
	if c.prefix() != "wgtest" {
		t.Fatalf("前缀错误: %s", c.prefix())
	}
	empty := &Container{}
	if empty.prefix() != "wg" {
		t.Fatalf("默认前缀错误: %s", empty.prefix())
	}
}

func TestNormalizePage(t *testing.T) {
	page, size := normalizePage(0, 0)
	if page != constant.DefaultPage || size != constant.DefaultPageSize {
		t.Fatalf("默认值错误: %d/%d", page, size)
	}
	page, size = normalizePage(2, 100000)
	if page != 2 || size != constant.MaxPageSize {
		t.Fatalf("上限约束错误: %d/%d", page, size)
	}
}
