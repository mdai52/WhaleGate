package jwt

import (
	"testing"
	"time"

	"github.com/whalegate/whalegate/internal/pkg/apierr"
)

func TestGenerateAndParse(t *testing.T) {
	m := New("secret", "whalegate", time.Hour)
	token, err := m.Generate(Claims{UserID: 7, Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	claims, err := m.Parse(token)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if claims.UserID != 7 || claims.Username != "alice" || claims.Role != "admin" {
		t.Fatalf("载荷不一致: %+v", claims)
	}
	if claims.Issuer != "whalegate" {
		t.Fatalf("签发者错误: %s", claims.Issuer)
	}
}

func TestExpiredToken(t *testing.T) {
	m := New("secret", "whalegate", time.Hour)
	token, err := m.GenerateWithTTL(Claims{UserID: 1}, -time.Minute)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := m.Parse(token); err == nil {
		t.Fatal("过期令牌应报错")
	} else if !apierr.Is(err, apierr.ErrTokenExpired) {
		t.Fatalf("应映射为令牌过期，实际 %v", err)
	}
}

func TestWrongSecret(t *testing.T) {
	m := New("secret", "whalegate", time.Hour)
	other := New("other", "whalegate", time.Hour)
	token, _ := m.Generate(Claims{UserID: 1})
	if _, err := other.Parse(token); err == nil {
		t.Fatal("密钥不匹配时应报错")
	} else if !apierr.Is(err, apierr.ErrInvalidCredential) {
		t.Fatalf("应映射为凭证无效，实际 %v", err)
	}
}

func TestMalformedToken(t *testing.T) {
	m := New("secret", "whalegate", time.Hour)
	if _, err := m.Parse("not-a-token"); err == nil {
		t.Fatal("非法令牌应报错")
	}
}
