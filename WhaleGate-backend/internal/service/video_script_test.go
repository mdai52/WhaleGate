package service

import "testing"

// 脚本解析要能吃下各家模型的输出形态：裸 JSON、代码块、思考过程、尾随逗号、截断。
func TestParseScriptPayload(t *testing.T) {
	ok := `{"title":"t","scenes":[{"index":1,"title":"a","visual":"v","narration":"n","camera":"c","seconds":4,"prompt":"p"}]}`

	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"裸 JSON", ok, false},
		{"带前后缀说明", "好的，以下是脚本：\n" + ok + "\n希望有帮助", false},
		{"markdown 代码块", "```json\n" + ok + "\n```", false},
		{"思考过程在前", "<think>用户想要一个活动预告片，我先想想节奏……</think>\n" + ok, false},
		{"只有思考没有正文", "<think>想了一半就停了", true},
		{"尾随逗号", `{"title":"t","scenes":[{"index":1,"seconds":4,},],}`, false},
		{"截断在数组元素后", `{"title":"t","scenes":[{"index":1,"seconds":4},{"index":2,"seconds":5`, false},
		{"截断在字段值中间", `{"title":"t","scenes":[{"index":1,"narration":"说到一半`, true},
		{"完全不是 JSON", "我无法输出该内容", true},
		{"空输出", "   ", true},
		{"没有镜头", `{"title":"t","scenes":[]}`, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload, err := parseScriptPayload(c.input)
			if c.wantErr {
				if err == nil {
					t.Fatalf("期望失败，实际解析出 %d 个镜头", len(payload.Scenes))
				}
				return
			}
			if err != nil {
				t.Fatalf("期望成功，实际 %v", err)
			}
			if len(payload.Scenes) == 0 {
				t.Fatal("镜头数不应为 0")
			}
		})
	}
}

func TestParseScriptPayloadErrorMessage(t *testing.T) {
	_, err := parseScriptPayload("输出被截断了，没有 JSON")
	if err == nil {
		t.Fatal("应返回错误")
	}
	if !contains(err.Error(), "JSON") {
		t.Fatalf("错误信息应提示 JSON 问题，实际 %q", err.Error())
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
