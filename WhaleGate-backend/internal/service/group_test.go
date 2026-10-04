package service

import (
	"testing"

	"github.com/whalegate/whalegate/internal/model"
)

func boolPtr(b bool) *bool { return &b }

func TestGroupInputNormalizedRejectsBadName(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"", "空标识"},
		{"a", "过短"},
		{"有中文", "非允许字符"},
		{"with space", "含空格"},
		{"tab\tx", "含制表符"},
	}
	for _, tc := range cases {
		if _, err := (GroupInput{Name: tc.name}).normalized(tc.name); err == nil {
			t.Fatalf("%s（%q）应当被拒绝", tc.want, tc.name)
		}
	}
}

func TestGroupInputNormalizedDefaults(t *testing.T) {
	group, err := (GroupInput{Name: "vip-1"}).normalized("vip-1")
	if err != nil {
		t.Fatalf("合法标识不应报错: %v", err)
	}
	// 系数缺省为 1（不加价），启用缺省为 true，展示名回落到标识
	if group.RatioMultiplier != 1 {
		t.Fatalf("倍率系数缺省应为 1，实际 %v", group.RatioMultiplier)
	}
	if !group.Enabled {
		t.Fatal("启用状态缺省应为 true")
	}
	if group.DisplayName != "vip-1" {
		t.Fatalf("展示名应回落到标识，实际 %q", group.DisplayName)
	}
}

func TestGroupInputNormalizedHonoursExplicitValues(t *testing.T) {
	group, err := (GroupInput{
		Name:            "vip",
		DisplayName:     "高优先级",
		RatioMultiplier: 2.5,
		QPM:             120,
		Concurrency:     8,
		Enabled:         boolPtr(false),
	}).normalized("vip")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if group.RatioMultiplier != 2.5 || group.QPM != 120 || group.Concurrency != 8 {
		t.Fatalf("配置未被正确写入: %+v", group)
	}
	if group.Enabled {
		t.Fatal("显式传入 false 时应为禁用")
	}
}

func TestGroupInputNormalizedRejectsNegativeLimits(t *testing.T) {
	if _, err := (GroupInput{Name: "vip", QPM: -1}).normalized("vip"); err == nil {
		t.Fatal("负数 QPM 应被拒绝")
	}
	if _, err := (GroupInput{Name: "vip", Concurrency: -1}).normalized("vip"); err == nil {
		t.Fatal("负数并发上限应被拒绝")
	}
	if _, err := (GroupInput{Name: "vip", RatioMultiplier: 1001}).normalized("vip"); err == nil {
		t.Fatal("过大的倍率系数应被拒绝")
	}
}

func TestGroupInputNormalizedZeroMultiplierFallsBackToOne(t *testing.T) {
	group, err := (GroupInput{Name: "free", RatioMultiplier: 0}).normalized("free")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if group.RatioMultiplier != 1 {
		t.Fatalf("非正系数应回落为 1，实际 %v", group.RatioMultiplier)
	}
}

// 分组缺失、被禁用时都必须回落中性策略：
// 否则删除或禁用分组会直接把存量密钥的调用打挂。
func TestPolicyFromTableFallsBackToNeutral(t *testing.T) {
	table := map[string]model.KeyGroup{
		"vip":     {Name: "vip", RatioMultiplier: 2, QPM: 100, Concurrency: 5, Enabled: true},
		"blocked": {Name: "blocked", RatioMultiplier: 9, QPM: 1, Concurrency: 1, Enabled: false},
	}

	neutral := policyFromTable(table, "not-exist")
	if neutral.RatioMultiplier != 1 || neutral.QPM != 0 || neutral.Concurrency != 0 {
		t.Fatalf("未命中分组应回落中性策略，实际 %+v", neutral)
	}
	if neutral.Tag != "" {
		t.Fatalf("中性策略不应带分组标识，实际 %q", neutral.Tag)
	}

	disabled := policyFromTable(table, "blocked")
	if disabled.RatioMultiplier != 1 || disabled.QPM != 0 || disabled.Concurrency != 0 {
		t.Fatalf("已禁用分组应回落中性策略，实际 %+v", disabled)
	}

	hit := policyFromTable(table, "vip")
	if hit.Tag != "vip" || hit.RatioMultiplier != 2 || hit.QPM != 100 || hit.Concurrency != 5 {
		t.Fatalf("命中分组应返回其配置，实际 %+v", hit)
	}
}

// 配置里出现非正系数时按不加价处理，避免除零式的地狱配置。
func TestPolicyFromTableZeroMultiplierMeansNoMarkup(t *testing.T) {
	table := map[string]model.KeyGroup{
		"odd": {Name: "odd", RatioMultiplier: 0, Enabled: true},
	}
	if got := policyFromTable(table, "odd"); got.RatioMultiplier != 1 {
		t.Fatalf("非正系数应按 1 处理，实际 %v", got.RatioMultiplier)
	}
}
