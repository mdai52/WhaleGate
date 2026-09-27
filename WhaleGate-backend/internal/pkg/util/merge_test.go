package util

import "testing"

func TestMergeMapDeep(t *testing.T) {
	base := map[string]interface{}{
		"model": "m",
		"generationConfig": map[string]interface{}{
			"temperature": 0.2,
			"topP":        0.9,
		},
	}
	patch := map[string]interface{}{
		"generationConfig": map[string]interface{}{"temperature": 0.8},
	}
	out := MergeMap(base, patch)
	cfg := out["generationConfig"].(map[string]interface{})
	if cfg["temperature"] != 0.8 {
		t.Fatalf("patch 应覆盖同名键: %v", cfg)
	}
	if cfg["topP"] != 0.9 {
		t.Fatalf("未覆盖的键应保留: %v", cfg)
	}
	if out["model"] != "m" {
		t.Fatalf("base 原有键应保留: %v", out)
	}
}

func TestMergeMapArrayReplaced(t *testing.T) {
	base := map[string]interface{}{"tools": []interface{}{"a"}}
	patch := map[string]interface{}{"tools": []interface{}{"image_generation"}}
	out := MergeMap(base, patch)
	tools := out["tools"].([]interface{})
	if len(tools) != 1 || tools[0] != "image_generation" {
		t.Fatalf("数组应整体替换（强制追加语义）: %v", tools)
	}
}

func TestMergeMapNilBase(t *testing.T) {
	out := MergeMap(nil, map[string]interface{}{"a": 1})
	if out["a"] != 1 {
		t.Fatalf("nil base 应返回 patch 内容: %v", out)
	}
}

func TestMergeMapEmptyPatch(t *testing.T) {
	base := map[string]interface{}{"a": 1}
	out := MergeMap(base, nil)
	if len(out) != 1 || out["a"] != 1 {
		t.Fatalf("空 patch 应原样返回: %v", out)
	}
}
