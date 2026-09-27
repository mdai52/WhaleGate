// Package crypto 提供 API Key 生成/哈希与渠道密钥的对称加解密。
package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// GenerateAPIKey 生成形如 sk-<随机串> 的明文密钥。
// n 为随机字节数，编码后长度约为 4*ceil(n/3)。
func GenerateAPIKey(prefix string, n int) (string, error) {
	if n <= 0 {
		n = 32
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机数失败: %w", err)
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// HashAPIKey 计算密钥的 SHA-256 摘要（十六进制），用于数据库唯一索引与缓存键。
func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// MaskAPIKey 生成用于展示的脱敏密钥：保留前缀与后 4 位。
func MaskAPIKey(key string) string {
	const visibleTail = 4
	if len(key) <= visibleTail {
		return strings.Repeat("*", len(key))
	}
	return key[:len(key)-visibleTail] + strings.Repeat("*", visibleTail)
}

// ConstantTimeEqual 以常量时间比较两个字符串，避免时序侧信道。
func ConstantTimeEqual(a, b string) bool {
	ha := sha256.Sum256([]byte(a))
	hb := sha256.Sum256([]byte(b))
	var diff byte
	for i := range ha {
		diff |= ha[i] ^ hb[i]
	}
	return diff == 0
}
