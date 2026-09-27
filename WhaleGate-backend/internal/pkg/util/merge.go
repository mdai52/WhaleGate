// Package util 提供跨包复用的小工具。
package util

// MergeMap 深度合并两个 JSON 对象，返回新对象：
//   - map 递归合并；
//   - 其余类型（含数组）由 patch 覆盖，用于"强制追加 tools / 改字段"。
func MergeMap(base, patch map[string]interface{}) map[string]interface{} {
	if len(patch) == 0 {
		return base
	}
	if base == nil {
		base = map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(base)+len(patch))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range patch {
		if sub, ok := v.(map[string]interface{}); ok {
			if origin, ok := out[k].(map[string]interface{}); ok {
				out[k] = MergeMap(origin, sub)
				continue
			}
		}
		out[k] = v
	}
	return out
}
