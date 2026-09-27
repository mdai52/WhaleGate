package crypto

import (
	"strings"
	"testing"
)

func TestGenerateAPIKey(t *testing.T) {
	key, err := GenerateAPIKey("sk-", 32)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if !strings.HasPrefix(key, "sk-") {
		t.Fatalf("缺少前缀: %s", key)
	}
	if len(key) < 40 {
		t.Fatalf("随机部分过短: %s", key)
	}
}

func TestGenerateAPIKeyUnique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		key, err := GenerateAPIKey("sk-", 32)
		if err != nil {
			t.Fatalf("生成失败: %v", err)
		}
		if _, ok := seen[key]; ok {
			t.Fatalf("出现重复密钥: %s", key)
		}
		seen[key] = struct{}{}
	}
}

func TestHashAPIKeyStable(t *testing.T) {
	a := HashAPIKey("sk-abc")
	b := HashAPIKey("sk-abc")
	if a != b {
		t.Fatalf("哈希不稳定: %s != %s", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("SHA-256 十六进制长度应为 64，实际 %d", len(a))
	}
	if a == HashAPIKey("sk-abd") {
		t.Fatal("不同密钥不应产生相同哈希")
	}
}

func TestMaskAPIKey(t *testing.T) {
	masked := MaskAPIKey("sk-abcdefgh")
	if !strings.HasSuffix(masked, "****") {
		t.Fatalf("尾部未脱敏: %s", masked)
	}
	if strings.Contains(masked[len(masked)-4:], "e") {
		t.Fatalf("尾部 4 位未隐藏: %s", masked)
	}
}

func TestConstantTimeEqual(t *testing.T) {
	if !ConstantTimeEqual("sk-same", "sk-same") {
		t.Fatal("相同字符串应判定相等")
	}
	if ConstantTimeEqual("sk-a", "sk-b") {
		t.Fatal("不同字符串不应判定相等")
	}
}
