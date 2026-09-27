package crypto

import (
	"strings"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	plain := "sk-upstream-secret-密钥"
	ct, err := Encrypt(plain, key)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if ct == plain {
		t.Fatal("密文不应等于明文")
	}
	got, err := Decrypt(ct, key)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if got != plain {
		t.Fatalf("解密结果不一致: %q != %q", got, plain)
	}
}

func TestEncryptDifferentCipherText(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	a, _ := Encrypt("same", key)
	b, _ := Encrypt("same", key)
	if a == b {
		t.Fatal("相同明文两次加密结果不应相同（nonce 随机）")
	}
}

func TestEncryptInvalidKeyLength(t *testing.T) {
	if _, err := Encrypt("x", []byte("too-short")); err != ErrInvalidKeyLength {
		t.Fatalf("应返回 ErrInvalidKeyLength，实际 %v", err)
	}
	if _, err := Decrypt("x", []byte("too-short")); err != ErrInvalidKeyLength {
		t.Fatalf("应返回 ErrInvalidKeyLength，实际 %v", err)
	}
}

func TestDecryptTampered(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	ct, _ := Encrypt("payload", key)
	tampered := ct[:len(ct)-2] + "xy"
	if _, err := Decrypt(tampered, key); err == nil {
		t.Fatal("篡改后的密文应解密失败")
	}
}

func TestDecryptInvalidBase64(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	if _, err := Decrypt("not-base64-!!!", key); err == nil {
		t.Fatal("非法 base64 应报错")
	} else if !strings.Contains(err.Error(), "base64") {
		t.Fatalf("错误信息应提示 base64: %v", err)
	}
}
