// Package storage 抽象生成图片的对象存储后端。
//
// 绘图工作台产出的图片需要长期留存（上游临时链接通常几小时失效），
// 但不同部署形态下的存储条件差异很大：单机部署可以放本地磁盘，
// 多实例或容器化部署则需要共享的对象存储。
// 本包把"写到哪里、从哪里读"收敛成一个接口，由管理员在后台选择具体实现。
package storage

import (
	"context"
	"io"
)

// 存储类型标识。落库后随作品一起保存，以便切换存储后仍能按原后端回读历史作品。
const (
	// KindLocal 本地磁盘。
	KindLocal = "local"
	// KindS3 S3 兼容对象存储（AWS S3、Cloudflare R2、阿里云 OSS、腾讯 COS、自建 MinIO 等）。
	KindS3 = "s3"
)

// Object 一个待写入的对象。
type Object struct {
	// Key 对象键，使用正斜杠分隔，形如 {prefix}/{workID}/{index}.png。
	Key string
	// ContentType 对象类型，如 image/png。
	ContentType string
	// Data 对象内容。
	Data []byte
}

// Storage 图片存储后端。
type Storage interface {
	// Kind 存储类型标识，与落库的 ImageAsset.Storage 对应。
	Kind() string
	// Put 写入对象；同名对象覆盖。
	Put(ctx context.Context, obj Object) error
	// Open 读取对象，仅在 PublicURL 返回空（无公开地址）时由服务端代理回读。
	// 调用方负责关闭返回的 ReadCloser。
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete 删除指定前缀下的全部对象，用于回收一件作品的全部图片。
	Delete(ctx context.Context, prefix string) error
	// PublicURL 返回可直接访问的地址；为空表示需要经服务端鉴权后代理回读。
	PublicURL(key string) string
}

// JoinKey 拼接对象键，统一使用正斜杠，便于各后端共用同一套键规则。
func JoinKey(parts ...string) string {
	key := ""
	for _, p := range parts {
		p = trimSlashes(p)
		if p == "" {
			continue
		}
		if key == "" {
			key = p
			continue
		}
		key += "/" + p
	}
	return key
}

// DirKey 返回对象键所在目录（去掉最后一段），用于按作品批量删除。
func DirKey(key string) string {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '/' {
			return key[:i]
		}
	}
	return ""
}

func trimSlashes(s string) string {
	start, end := 0, len(s)
	for start < end && s[start] == '/' {
		start++
	}
	for end > start && s[end-1] == '/' {
		end--
	}
	return s[start:end]
}
