package service

import (
	"context"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/crypto"
	"github.com/whalegate/whalegate/internal/pkg/storage"
)

// studioStorage 持有绘图工作台当前生效的存储后端。
//
// 对象存储客户端持有常驻连接，配置变更后需要重建实例；同时保留各类型
// 最近一次构建的实例——作品记录保存了写入时用的存储类型，切换存储后
// 历史作品仍须按原后端回读，而不是在新后端里找不到。
type studioStorage struct {
	mu      sync.RWMutex
	current storage.Storage
	byKind  map[string]storage.Storage
}

func newStudioStorage() *studioStorage {
	return &studioStorage{byKind: make(map[string]storage.Storage, 2)}
}

// Current 返回当前用于写入的存储后端。
func (p *studioStorage) Current() storage.Storage {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

// For 按类型取存储后端。该类型已不再是当前后端时，仍返回其最后构建的实例；
// 空类型（历史数据未记录存储类型）按本地磁盘处理。
func (p *studioStorage) For(kind string) storage.Storage {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if kind == "" {
		kind = storage.KindLocal
	}
	if s, ok := p.byKind[kind]; ok && s != nil {
		return s
	}
	return p.current
}

func (p *studioStorage) set(s storage.Storage) {
	if s == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = s
	p.byKind[s.Kind()] = s
}

// LoadStudioStorage 启动时从数据库加载存储配置并构建后端。
// 未配置时按本地磁盘 + 配置文件中的 studio.data_dir 兜底。
func (c *Container) LoadStudioStorage(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	secret, err := c.studioS3SecretKey(ctx)
	if err != nil && c.Logger != nil {
		c.Logger.Warn("读取对象存储密钥失败，将以未配置密钥的状态启动", zap.Error(err))
	}
	c.reloadStudioStorage(c.Settings(), secret)
}

// reloadStudioStorage 按新设置重建存储后端，管理员保存配置后即时生效。
// 构建失败时保留原后端并记日志：填错配置不该让整个绘图功能不可用。
func (c *Container) reloadStudioStorage(s Settings, secretKey string) {
	built, err := c.buildStudioStorage(&s, secretKey)
	if err != nil {
		if c.Logger != nil {
			c.Logger.Error("绘图存储后端构建失败，沿用原配置", zap.Error(err))
		}
		return
	}
	c.studioStore.set(built)
	if c.Logger != nil {
		c.Logger.Info("绘图存储后端已就绪", zap.String("driver", built.Kind()))
	}
}

// buildStudioStorage 按设置构建存储后端，secretKey 为已解密的明文密钥。
func (c *Container) buildStudioStorage(s *Settings, secretKey string) (storage.Storage, error) {
	if strings.TrimSpace(s.StudioStorageDriver) == storage.KindS3 {
		return storage.NewS3(storage.S3{
			Endpoint:       s.StudioS3Endpoint,
			Region:         s.StudioS3Region,
			Bucket:         s.StudioS3Bucket,
			Prefix:         s.StudioS3Prefix,
			AccessKey:      s.StudioS3AccessKey,
			SecretKey:      secretKey,
			ForcePathStyle: s.StudioS3ForcePathStyle,
			PublicBaseURL:  s.StudioS3PublicBaseURL,
		})
	}
	root := strings.TrimSpace(s.StudioLocalRoot)
	if root == "" {
		root = strings.TrimSpace(c.Config.Studio.DataDir)
	}
	if root == "" {
		root = "./data/studio"
	}
	return &storage.Local{Root: root, BaseURL: s.StudioLocalBaseURL}, nil
}

// studioS3SecretKey 读取并解密库内的对象存储密钥；未配置时返回空字符串。
func (c *Container) studioS3SecretKey(ctx context.Context) (string, error) {
	value, ok := c.getRawSetting(ctx, model.SettingStudioS3SecretKey)
	if !ok || strings.TrimSpace(value) == "" {
		return "", nil
	}
	plain, err := crypto.Decrypt(value, c.encryptionKey())
	if err != nil {
		return "", apierr.Wrap(apierr.ErrCrypto, err)
	}
	return plain, nil
}

// resolveStudioStorage 校验存储配置并加密密钥，供 UpdateSettings 落库使用。
// 返回归一后的驱动、明文密钥（用于即时构建后端）与密文（用于落库）。
func (c *Container) resolveStudioStorage(ctx context.Context, in *Settings) (driver, secretKey, cipher string, err error) {
	driver = strings.TrimSpace(in.StudioStorageDriver)
	if driver == "" {
		driver = storage.KindLocal
	}
	switch driver {
	case storage.KindLocal, storage.KindS3:
	default:
		return "", "", "", apierr.Errorf(apierr.ErrInvalidParam, "不支持的存储类型：%s", driver)
	}

	secretKey = strings.TrimSpace(in.StudioS3SecretKey)
	if secretKey == "" {
		// 管理员只改其它字段时不应丢失已保存的密钥，此处沿用库内密文。
		secretKey, err = c.studioS3SecretKey(ctx)
		if err != nil {
			return "", "", "", err
		}
	}
	if driver == storage.KindS3 {
		switch {
		case strings.TrimSpace(in.StudioS3Endpoint) == "":
			return "", "", "", apierr.New(apierr.ErrInvalidParam, "使用对象存储时需填写服务地址")
		case strings.TrimSpace(in.StudioS3Bucket) == "":
			return "", "", "", apierr.New(apierr.ErrInvalidParam, "使用对象存储时需填写桶名")
		case strings.TrimSpace(in.StudioS3AccessKey) == "" || secretKey == "":
			return "", "", "", apierr.New(apierr.ErrInvalidParam, "使用对象存储时需填写访问密钥 ID 与密钥")
		}
	}

	if secretKey == "" {
		return driver, "", "", nil
	}
	cipher, err = crypto.Encrypt(secretKey, c.encryptionKey())
	if err != nil {
		return "", "", "", apierr.Wrap(apierr.ErrCrypto, err)
	}
	return driver, secretKey, cipher, nil
}
