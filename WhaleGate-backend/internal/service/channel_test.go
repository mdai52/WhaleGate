package service

import (
	"testing"

	"github.com/whalegate/whalegate/internal/model"
)

func TestChannelSupportsModel(t *testing.T) {
	ch := &model.Channel{}
	if err := ch.SetModelList([]string{"gpt-4o", "gpt-4o-mini"}); err != nil {
		t.Fatalf("设置模型列表失败: %v", err)
	}
	if !ch.SupportsModel("gpt-4o") {
		t.Fatal("应支持 gpt-4o")
	}
	if ch.SupportsModel("claude-3") {
		t.Fatal("不应支持未登记的模型")
	}

	wildcard := &model.Channel{}
	if !wildcard.SupportsModel("anything") {
		t.Fatal("空列表应视为通配")
	}
}

func TestChannelMapModel(t *testing.T) {
	ch := &model.Channel{ModelMapping: `{"gpt-4o":"gpt-4o-2024-08-06"}`}
	if got := ch.MapModel("gpt-4o"); got != "gpt-4o-2024-08-06" {
		t.Fatalf("映射错误: %s", got)
	}
	if got := ch.MapModel("gpt-4o-mini"); got != "gpt-4o-mini" {
		t.Fatalf("未配置映射应原样返回: %s", got)
	}

	invalid := &model.Channel{ModelMapping: `{bad json`}
	if got := invalid.MapModel("x"); got != "x" {
		t.Fatalf("非法映射应原样返回: %s", got)
	}
}

func TestChannelEffectiveWeight(t *testing.T) {
	cases := map[int]int{0: 1, -5: 1, 3: 3}
	for in, want := range cases {
		if got := (&model.Channel{Weight: in}).EffectiveWeight(); got != want {
			t.Errorf("Weight %d 期望有效权重 %d，实际 %d", in, want, got)
		}
	}
	if got := (*model.Channel)(nil).EffectiveWeight(); got != 1 {
		t.Errorf("nil 渠道应退化为 1，实际 %d", got)
	}
}

func TestWeightedPickSingle(t *testing.T) {
	ch := &model.Channel{ID: 1, Weight: 1}
	if got := weightedPick([]*model.Channel{ch}); got != ch {
		t.Fatal("单渠道应直接返回")
	}
}

func TestWeightedPickDistribution(t *testing.T) {
	low := &model.Channel{ID: 1, Weight: 1}
	high := &model.Channel{ID: 2, Weight: 9}
	counts := map[uint]int{2: 0, 1: 0}
	for i := 0; i < 2000; i++ {
		counts[weightedPick([]*model.Channel{low, high}).ID]++
	}
	if counts[1] == 0 || counts[2] == 0 {
		t.Fatalf("权重选择未覆盖全部渠道: %v", counts)
	}
	if counts[2] <= counts[1] {
		t.Fatalf("高权重渠道应被更频繁选中: %v", counts)
	}
}

func TestDefaultInt(t *testing.T) {
	if got := defaultInt(0, 7); got != 7 {
		t.Fatalf("0 应取默认值，实际 %d", got)
	}
	if got := defaultInt(3, 7); got != 3 {
		t.Fatalf("正数应保留，实际 %d", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdef", 3); got != "abc" {
		t.Fatalf("截断错误: %q", got)
	}
	if got := truncate("ab", 5); got != "ab" {
		t.Fatalf("短字符串应保持原样: %q", got)
	}
}
