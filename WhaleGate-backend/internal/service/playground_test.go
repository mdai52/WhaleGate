package service

import (
	"encoding/json"
	"testing"
)

// 空集合必须序列化成 []，不能是 null。
// 曾因返回 null 导致前端对 null 取 length 抛错，整块预览结果渲染不出来。
func TestUnknownParamsEmptySliceNotNil(t *testing.T) {
	got := unknownParams(map[string]interface{}{"temperature": 0.5})
	if got == nil {
		t.Fatal("unknownParams 返回 nil，序列化后会是 null")
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if string(raw) != "[]" {
		t.Fatalf("空集合应序列化为 []，实际为 %s", raw)
	}
}

func TestUnknownParamsEmptyInput(t *testing.T) {
	got := unknownParams(nil)
	if got == nil {
		t.Fatal("空输入也应返回非 nil 切片")
	}
	if len(got) != 0 {
		t.Fatalf("空输入应返回空集合，实际 %v", got)
	}
}

// 直接断言线上 JSON 契约：集合字段为空时必须是 []，不能是 null。
// 这是前端渲染依赖的硬约束——null 会在取 length 时抛错。
func TestParamPreviewEmptyCollectionsMarshalAsArrays(t *testing.T) {
	out := &ParamPreview{
		Applied:  []string{},
		Dropped:  []string{},
		Excluded: []string{},
		Unknown:  unknownParams(nil),
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	for _, field := range []string{"applied", "dropped", "excluded", "unknown"} {
		got, ok := decoded[field]
		if !ok {
			t.Fatalf("字段 %s 缺失", field)
		}
		if string(got) != "[]" {
			t.Fatalf("字段 %s 应为 []，实际为 %s", field, got)
		}
	}
}

// 标准参数与可归一化的写法都不算未知，只有真正不认识的名字才提示。
func TestUnknownParamsPicksOnlyNonStandardNames(t *testing.T) {
	got := unknownParams(map[string]interface{}{
		"temperature":     0.5,                     // 标准名
		"top-k":           40,                      // 可归一化为 top_k
		"maxOutputTokens": 1024,                    // 可归一化为 max_tokens
		"logit_bias":      map[string]any{"1": -1}, // 未知
	})
	want := []string{"logit_bias"}
	if len(got) != len(want) {
		t.Fatalf("期望 %v，实际 %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("期望 %v，实际 %v", want, got)
		}
	}
}
