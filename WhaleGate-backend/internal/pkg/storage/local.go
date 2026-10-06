package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Local 本地磁盘存储。单机部署的默认选择，零外部依赖。
type Local struct {
	// Root 存储根目录，绝对路径或相对于进程工作目录。
	Root string
	// BaseURL 可选的公开访问前缀。当目录已由 Nginx 等直接以静态资源对外服务时
	// 填写（如 https://static.example.com/studio），图片即可免鉴权直连，
	// 不再占用网关带宽；留空则一律经服务端鉴权后代理回读。
	BaseURL string
}

// Kind 返回存储类型。
func (l *Local) Kind() string { return KindLocal }

// Put 写入文件，自动创建父目录。
func (l *Local) Put(_ context.Context, obj Object) error {
	abs, err := l.abs(obj.Key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return err
	}
	return os.WriteFile(abs, obj.Data, 0o640)
}

// Open 打开本地文件。
func (l *Local) Open(_ context.Context, key string) (io.ReadCloser, error) {
	abs, err := l.abs(key)
	if err != nil {
		return nil, err
	}
	return os.Open(abs)
}

// Size 读取文件大小，供视频分段回读应答 HTTP Range。
func (l *Local) Size(_ context.Context, key string) (int64, error) {
	abs, err := l.abs(key)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// OpenRange 打开本地文件并定位到指定字节区间，避免整段读取。
func (l *Local) OpenRange(_ context.Context, key string, start, end int64) (io.ReadCloser, error) {
	if start < 0 || end < start {
		return nil, ErrInvalidKey
	}
	abs, err := l.abs(key)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &sectionReader{Closer: file, Reader: io.LimitReader(file, end-start+1)}, nil
}

// sectionReader 把 io.Reader 包装成 ReadCloser，关闭时一并关闭底层文件。
type sectionReader struct {
	io.Reader
	Closer io.Closer
}

func (s *sectionReader) Close() error { return s.Closer.Close() }

// Delete 删除根目录下该前缀对应的整个目录。
func (l *Local) Delete(_ context.Context, prefix string) error {
	if strings.TrimSpace(prefix) == "" {
		return nil
	}
	abs, err := l.abs(prefix)
	if err != nil {
		return err
	}
	return os.RemoveAll(abs)
}

// PublicURL 拼接公开访问前缀；未配置时返回空，交由服务端代理回读。
func (l *Local) PublicURL(key string) string {
	base := strings.TrimRight(strings.TrimSpace(l.BaseURL), "/")
	if base == "" {
		return ""
	}
	return base + "/" + trimSlashes(key)
}

// abs 把对象键映射到本地绝对路径，并拦截目录穿越。
func (l *Local) abs(key string) (string, error) {
	root := filepath.Clean(l.Root)
	if root == "" || root == "." {
		return "", ErrEmptyRoot
	}
	rel := filepath.FromSlash(trimSlashes(key))
	if rel == "" {
		return "", ErrInvalidKey
	}
	// 键由本服务生成，仍做一次本地性校验，防止历史脏数据造成目录穿越。
	if !filepath.IsLocal(rel) {
		return "", ErrInvalidKey
	}
	return filepath.Join(root, rel), nil
}
