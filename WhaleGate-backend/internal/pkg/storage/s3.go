package storage

import (
	"bytes"
	"context"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3 兼容对象存储。
//
// 覆盖 AWS S3、Cloudflare R2、阿里云 OSS、腾讯 COS、七牛与自建 MinIO 等；
// 这些服务除端点与签名实现外接口一致，因此不需要按厂商分别适配。
type S3 struct {
	// Endpoint 服务地址，需带协议，如 https://s3.us-east-1.amazonaws.com。
	Endpoint string
	// Region 区域；自建 MinIO 与部分厂商可留空，由 SDK 取默认值。
	Region string
	// Bucket 桶名。
	Bucket string
	// Prefix 对象键前缀，便于与其他业务共用同一个桶。
	Prefix string
	// AccessKey 访问密钥 ID。
	AccessKey string
	// SecretKey 访问密钥密文。
	SecretKey string
	// ForcePathStyle 使用路径风格寻址。自建 MinIO 与部分国内厂商要求开启，
	// AWS S3 与 Cloudflare R2 使用虚拟主机风格，保持关闭即可。
	ForcePathStyle bool
	// PublicBaseURL 可选的公开访问前缀（CDN 域名或桶的公开域名）。
	// 填写后图片免鉴权直连；留空则经服务端鉴权后代理回读。
	PublicBaseURL string

	client *minio.Client
}

// NewS3 构造对象存储客户端。密钥错误不会在此刻暴露，首次读写时才会报错。
func NewS3(cfg S3) (*S3, error) {
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	cfg.AccessKey = strings.TrimSpace(cfg.AccessKey)
	if cfg.Endpoint == "" {
		return nil, ErrNoEndpoint
	}
	if cfg.Bucket == "" {
		return nil, ErrNoBucket
	}
	if cfg.AccessKey == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, ErrNoCredentials
	}

	// 路径风格寻址对应 SDK 的 BucketLookupPath：自建 MinIO 与部分国内厂商只支持这种形式。
	lookup := minio.BucketLookupAuto
	if cfg.ForcePathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(strings.TrimPrefix(strings.TrimPrefix(cfg.Endpoint, "https://"), "http://"),
		&minio.Options{
			Creds:        credentials.NewStaticV4(cfg.AccessKey, strings.TrimSpace(cfg.SecretKey), ""),
			Secure:       strings.HasPrefix(cfg.Endpoint, "https://"),
			Region:       strings.TrimSpace(cfg.Region),
			BucketLookup: lookup,
		})
	if err != nil {
		return nil, err
	}
	cfg.SecretKey = strings.TrimSpace(cfg.SecretKey)
	cfg.client = client
	return &cfg, nil
}

// Kind 返回存储类型。
func (s *S3) Kind() string { return KindS3 }

// Put 上传对象。
func (s *S3) Put(ctx context.Context, obj Object) error {
	_, err := s.client.PutObject(ctx, s.Bucket, s.objectKey(obj.Key),
		bytes.NewReader(obj.Data), int64(len(obj.Data)),
		minio.PutObjectOptions{ContentType: obj.ContentType})
	return err
}

// Open 下载对象。
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.Bucket, s.objectKey(key), minio.GetObjectOptions{})
}

// Delete 删除该前缀下的全部对象。
func (s *S3) Delete(ctx context.Context, prefix string) error {
	if strings.TrimSpace(prefix) == "" {
		return nil
	}
	objects := s.client.ListObjects(ctx, s.Bucket, minio.ListObjectsOptions{
		Prefix:    s.objectKey(prefix) + "/",
		Recursive: true,
	})
	for object := range objects {
		if object.Err != nil {
			return object.Err
		}
		if err := s.client.RemoveObject(ctx, s.Bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// PublicURL 拼接公开访问前缀；未配置时返回空，交由服务端代理回读。
func (s *S3) PublicURL(key string) string {
	base := strings.TrimRight(strings.TrimSpace(s.PublicBaseURL), "/")
	if base == "" {
		return ""
	}
	return base + "/" + s.objectKey(key)
}

// objectKey 加上桶内前缀。
func (s *S3) objectKey(key string) string {
	if prefix := trimSlashes(s.Prefix); prefix != "" {
		return prefix + "/" + trimSlashes(key)
	}
	return trimSlashes(key)
}
