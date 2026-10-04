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

// MaskAPIKey 生成用于展示的脱敏密钥：保留头部与末 4 位，中间以省略号隐藏。
//
// 只保留 6 位头部与 4 位尾部：既能辨认是哪一个密钥，又不会把密钥暴露到近乎完整。
// 旧实现仅隐藏末 4 位，列表里展示的字符串与真实密钥几乎一致，
// 用户很容易当成完整密钥复制去调用，从而拿到「凭证无效」的 401。
func MaskAPIKey(key string) string {
	const (
		headLen = 6
		tailLen = 4
	)
	if len(key) <= headLen+tailLen {
		return strings.Repeat("*", len(key))
	}
	return key[:headLen] + "..." + key[len(key)-tailLen:]
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
