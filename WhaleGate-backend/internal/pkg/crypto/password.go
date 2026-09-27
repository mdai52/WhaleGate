package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ErrPasswordTooShort 密码长度不足。
var ErrPasswordTooShort = errors.New("密码长度不足")

// MinPasswordLength 最小密码长度。
const MinPasswordLength = 8

// GeneratePassword 生成 n 位随机强密码（URL 安全字符集：大小写字母、数字、-、_）。
// 用于初始化管理员账号；明文只应出现在日志中一次。
func GeneratePassword(n int) (string, error) {
	if n < MinPasswordLength {
		n = MinPasswordLength
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机数失败: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(buf)
	if len(encoded) > n {
		encoded = encoded[:n]
	}
	return encoded, nil
}

// ValidatePassword 校验密码强度。
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("密码不能为空白字符")
	}
	return nil
}
