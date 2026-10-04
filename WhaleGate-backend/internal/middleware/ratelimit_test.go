package middleware

import "testing"

// 生效上限的优先级契约：密钥级 > 分组级 > 全局默认。
// 每一层的 0 都表示"该层不设限"，因此逐层回落。
func TestEffectiveLimitPrecedence(t *testing.T) {
	cases := []struct {
		name                     string
		keyValue, groupValue, fb int
		want                     int
	}{
		{"密钥配置优先于分组", 10, 100, 1000, 10},
		{"密钥未配置时继承分组", 0, 100, 1000, 100},
		{"密钥与分组都未配置时用全局默认", 0, 0, 1000, 1000},
		{"分组配置优先于全局默认", 0, 5, 1000, 5},
		{"负数视为未配置", -1, 100, 1000, 100},
		{"全为负数时回落到全局默认", -1, -1, 1000, 1000},
	}
	for _, tc := range cases {
		if got := effectiveLimit(tc.keyValue, tc.groupValue, tc.fb); got != tc.want {
			t.Fatalf("%s：期望 %d，实际 %d", tc.name, tc.want, got)
		}
	}
}
