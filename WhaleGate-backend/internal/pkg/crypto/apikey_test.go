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
	masked := MaskAPIKey("sk-abcdefghij")
	if !strings.Contains(masked, "...") {
		t.Fatalf("未使用省略号隐藏中段: %s", masked)
	}
	if !strings.HasSuffix(masked, "ghij") {
		t.Fatalf("应保留末 4 位便于辨认: %s", masked)
	}
	if !strings.HasPrefix(masked, "sk-abc") {
		t.Fatalf("应保留前缀: %s", masked)
	}
}

// 打码结果绝不能与真实密钥相同，也不能保留到几乎完整——
// 否则用户会误把它当成可用密钥去调用，得到「凭证无效」。
func TestMaskAPIKeyHidesMostOfKey(t *testing.T) {
	real := "sk-aB3xK9mPq7RtYuI2oP4aS6dF8gH0jK1lZ5v"
	masked := MaskAPIKey(real)
	if masked == real {
		t.Fatal("打码后不应等于明文")
	}
	if strings.Contains(real, masked) || strings.Contains(masked, real[:20]) {
		t.Fatalf("打码泄露过多明文: %s", masked)
	}
	// 可见字符（去掉省略号与前缀分隔）不应超过 10 个
	visible := strings.NewReplacer("...", "", "-", "").Replace(masked)
	if len(visible) > 10 {
		t.Fatalf("可见字符过多（%d）: %s", len(visible), masked)
	}
}

func TestMaskAPIKeyShortInput(t *testing.T) {
	if got := MaskAPIKey("sk"); got != "**" {
		t.Fatalf("过短输入应整体打码，实际: %s", got)
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
