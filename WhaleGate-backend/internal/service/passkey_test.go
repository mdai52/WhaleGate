package service

import "testing"

func TestUserHandleRoundTrip(t *testing.T) {
	for _, id := range []uint{1, 42, 123456, 4294967295} {
		if got := handleToUserID(userHandle(id)); got != id {
			t.Errorf("handleToUserID(userHandle(%d)) = %d", id, got)
		}
	}
}

func TestUserHandleLength(t *testing.T) {
	if len(userHandle(1)) != 8 {
		t.Fatalf("user handle 应为 8 字节，实际 %d", len(userHandle(1)))
	}
}

func TestHandleToUserIDShort(t *testing.T) {
	if got := handleToUserID(nil); got != 0 {
		t.Fatalf("空 handle 应为 0，实际 %d", got)
	}
	if got := handleToUserID([]byte{0x01, 0x02}); got != 0x0102 {
		t.Fatalf("短 handle 解析错误: %d", got)
	}
}

func TestDisplayNameOf(t *testing.T) {
	p := &githubUser{Login: "octocat", Name: "Mona Lisa"}
	if got := displayNameOf(p); got != "Mona Lisa" {
		t.Fatalf("应优先使用 Name: %s", got)
	}
	p2 := &githubUser{Login: "octocat"}
	if got := displayNameOf(p2); got != "octocat" {
		t.Fatalf("无 Name 时应回退 Login: %s", got)
	}
}

func TestSessionIDFor(t *testing.T) {
	if sessionIDFor(7) != "7" {
		t.Fatalf("会话 ID 生成错误: %s", sessionIDFor(7))
	}
}
