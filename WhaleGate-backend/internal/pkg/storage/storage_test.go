package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestJoinKey(t *testing.T) {
	cases := []struct {
		parts []string
		want  string
	}{
		{[]string{"12", "0.png"}, "12/0.png"},
		{[]string{"studio/", "/12/", "/0.png"}, "studio/12/0.png"},
		{[]string{"", "12", "0.png"}, "12/0.png"},
		{[]string{"12"}, "12"},
		{nil, ""},
	}
	for _, c := range cases {
		if got := JoinKey(c.parts...); got != c.want {
			t.Fatalf("JoinKey(%v) = %q, want %q", c.parts, got, c.want)
		}
	}
}

func TestDirKey(t *testing.T) {
	if got := DirKey("studio/12/0.png"); got != "studio/12" {
		t.Fatalf("DirKey = %q", got)
	}
	if got := DirKey("0.png"); got != "" {
		t.Fatalf("无目录时应返回空，实际 %q", got)
	}
}

func TestLocalPutOpenDelete(t *testing.T) {
	root := t.TempDir()
	store := &Local{Root: root}

	obj := Object{Key: "12/0.png", ContentType: "image/png", Data: []byte("png-bytes")}
	if err := store.Put(context.Background(), obj); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "12", "0.png")); err != nil {
		t.Fatalf("文件未落盘: %v", err)
	}

	reader, err := store.Open(context.Background(), "12/0.png")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	data, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatalf("读取内容失败: %v", err)
	}
	if string(data) != "png-bytes" {
		t.Fatalf("内容不符: %s", data)
	}

	if err := store.Delete(context.Background(), "12"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "12")); !os.IsNotExist(err) {
		t.Fatal("目录应已被删除")
	}
}

func TestLocalRejectsTraversal(t *testing.T) {
	store := &Local{Root: t.TempDir()}
	if _, err := store.Open(context.Background(), "../../etc/passwd"); err != ErrInvalidKey {
		t.Fatalf("应拦截目录穿越，实际: %v", err)
	}
}

func TestLocalPublicURL(t *testing.T) {
	store := &Local{Root: t.TempDir()}
	if got := store.PublicURL("12/0.png"); got != "" {
		t.Fatalf("未配置前缀时应返回空，实际 %q", got)
	}
	store.BaseURL = "https://static.example.com/studio/"
	if got := store.PublicURL("/12/0.png"); got != "https://static.example.com/studio/12/0.png" {
		t.Fatalf("公开地址拼接错误: %q", got)
	}
}

func TestLocalEmptyRoot(t *testing.T) {
	if err := (&Local{}).Put(context.Background(), Object{Key: "1/0.png"}); err != ErrEmptyRoot {
		t.Fatalf("未配置根目录时应报错，实际: %v", err)
	}
}

func TestNewS3ValidatesConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  S3
		want error
	}{
		{"缺端点", S3{Bucket: "b", AccessKey: "ak", SecretKey: "sk"}, ErrNoEndpoint},
		{"缺桶名", S3{Endpoint: "https://s3.example.com", AccessKey: "ak", SecretKey: "sk"}, ErrNoBucket},
		{"缺密钥", S3{Endpoint: "https://s3.example.com", Bucket: "b"}, ErrNoCredentials},
		{"只填 AK", S3{Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "ak"}, ErrNoCredentials},
	}
	for _, c := range cases {
		if _, err := NewS3(c.cfg); err != c.want {
			t.Fatalf("%s: 期望 %v，实际 %v", c.name, c.want, err)
		}
	}
}

func TestNewS3BuildsClient(t *testing.T) {
	store, err := NewS3(S3{
		Endpoint:       "https://s3.us-east-1.amazonaws.com",
		Region:         "us-east-1",
		Bucket:         "whalegate",
		Prefix:         "studio/",
		AccessKey:      "ak",
		SecretKey:      "sk",
		PublicBaseURL:  "https://cdn.example.com/",
		ForcePathStyle: false,
	})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if store.Kind() != KindS3 {
		t.Fatalf("类型应为 s3，实际 %s", store.Kind())
	}
	if got := store.objectKey("12/0.png"); got != "studio/12/0.png" {
		t.Fatalf("对象键应带前缀，实际 %q", got)
	}
	if got := store.PublicURL("12/0.png"); got != "https://cdn.example.com/studio/12/0.png" {
		t.Fatalf("公开地址错误: %q", got)
	}
}

func TestS3PublicURLEmptyWithoutBase(t *testing.T) {
	store, err := NewS3(S3{Endpoint: "https://s3.example.com", Bucket: "b", AccessKey: "ak", SecretKey: "sk"})
	if err != nil {
		t.Fatalf("构建失败: %v", err)
	}
	if got := store.PublicURL("12/0.png"); got != "" {
		t.Fatalf("未配置公开前缀时应返回空，实际 %q", got)
	}
}
