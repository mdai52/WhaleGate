// Package jwt 封装管理端/用户门户登录令牌的签发与校验。
package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/whalegate/whalegate/internal/pkg/apierr"
)

// Claims 是鲸闸登录令牌的载荷。
type Claims struct {
	UserID   uint   `json:"uid"`
	Username string `json:"username"`
	Role     string `json:"role"`
	// MustChange 持有该令牌的用户仍需修改初始密码。
	MustChange bool `json:"mchg,omitempty"`
	jwt.RegisteredClaims
}

// Manager 负责签发与校验令牌。
type Manager struct {
	secret []byte
	issuer string
	ttl    time.Duration
}

// New 创建 Manager。
func New(secret, issuer string, ttl time.Duration) *Manager {
	return &Manager{secret: []byte(secret), issuer: issuer, ttl: ttl}
}

// Generate 签发令牌。
func (m *Manager) Generate(c Claims) (string, error) {
	now := time.Now()
	c.Issuer = m.issuer
	c.IssuedAt = jwt.NewNumericDate(now)
	c.NotBefore = jwt.NewNumericDate(now)
	c.ExpiresAt = jwt.NewNumericDate(now.Add(m.ttl))
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(m.secret)
}

// GenerateWithTTL 按自定义有效期签发令牌。
func (m *Manager) GenerateWithTTL(c Claims, ttl time.Duration) (string, error) {
	now := time.Now()
	c.Issuer = m.issuer
	c.IssuedAt = jwt.NewNumericDate(now)
	c.NotBefore = jwt.NewNumericDate(now)
	c.ExpiresAt = jwt.NewNumericDate(now.Add(ttl))
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(m.secret)
}

// Parse 校验并解析令牌。过期与无效均映射为业务错误码。
func (m *Manager) Parse(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(*jwt.Token) (interface{}, error) {
		return m.secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, apierr.Wrap(apierr.ErrTokenExpired, err)
		}
		return nil, apierr.Wrap(apierr.ErrInvalidCredential, err)
	}
	if !token.Valid {
		return nil, apierr.New(apierr.ErrInvalidCredential, "令牌校验未通过")
	}
	return claims, nil
}

// TTL 返回默认有效期。
func (m *Manager) TTL() time.Duration { return m.ttl }
