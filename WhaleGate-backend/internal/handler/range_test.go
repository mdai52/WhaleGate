package handler

import "testing"

func TestParseRangeFull(t *testing.T) {
	got := parseRange("bytes=0-99", 1000)
	if !got.ok || got.start != 0 || got.end != 99 {
		t.Fatalf("区间解析错误: %+v", got)
	}
}

func TestParseRangeOpenEnded(t *testing.T) {
	// bytes=N-：到文件末尾，播放器 seek 到中段时常见。
	got := parseRange("bytes=500-", 1000)
	if !got.ok || got.start != 500 || got.end != 999 {
		t.Fatalf("开放式区间解析错误: %+v", got)
	}
}

func TestParseRangeSuffix(t *testing.T) {
	// bytes=-N：末尾 N 字节。
	got := parseRange("bytes=-100", 1000)
	if !got.ok || got.start != 900 || got.end != 999 {
		t.Fatalf("后缀区间解析错误: %+v", got)
	}
}

func TestParseRangeClampsBeyondSize(t *testing.T) {
	got := parseRange("bytes=900-5000", 1000)
	if !got.ok || got.start != 900 || got.end != 999 {
		t.Fatalf("超出长度的区间应收敛到末尾: %+v", got)
	}
}

func TestParseRangeRejectsInvalid(t *testing.T) {
	cases := []string{
		"",                // 未带 Range
		"items=0-99",      // 非 bytes 单位
		"bytes=0-99,200-", // 多区间：不支持，退回整段
		"bytes=abc-def",   // 非数字
		"bytes=1000-",     // 起点越界
		"bytes=500-400",   // 起点大于终点
		"bytes=-0",        // 零长度
	}
	for _, header := range cases {
		if got := parseRange(header, 1000); got.ok {
			t.Fatalf("非法 Range %q 应退回整段，实际 %+v", header, got)
		}
	}
}

func TestParseRangeRequiresSize(t *testing.T) {
	// 拿不到文件大小时无法应答 Range。
	if got := parseRange("bytes=0-99", 0); got.ok {
		t.Fatalf("无长度信息时不应分段: %+v", got)
	}
}
