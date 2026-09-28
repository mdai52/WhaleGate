package service

import (
	"context"
	"crypto/tls"
	"errors"
	"os"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/model"
)

// tlsCertProvider 持有当前生效的 TLS 证书与启用状态。
// 通过 atomic.Pointer 实现证书热替换，无需重启即可轮换证书；
// 但监听模式（HTTP/HTTPS）在启动时确定，切换需进程重启。
type tlsCertProvider struct {
	enabled atomic.Bool
	cert    atomic.Pointer[tls.Certificate]
}

func newTLSCertProvider() *tlsCertProvider { return &tlsCertProvider{} }

func (p *tlsCertProvider) set(cert *tls.Certificate, enabled bool) {
	if cert != nil {
		p.cert.Store(cert)
	}
	p.enabled.Store(enabled)
}

// IsEnabled 是否以 HTTPS 监听。
func (p *tlsCertProvider) IsEnabled() bool { return p.enabled.Load() }

// GetCertificate 供 tls.Config.GetCertificate 使用，动态返回当前证书。
func (p *tlsCertProvider) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	c := p.cert.Load()
	if c == nil {
		return nil, errors.New("no certificate configured")
	}
	return c, nil
}

// TLSIsEnabled 返回是否以 HTTPS 监听。
func (c *Container) TLSIsEnabled() bool { return c.tlsProvider.IsEnabled() }

// GetCertificate 供 HTTPS 监听动态取证书。
func (c *Container) GetCertificate(chi *tls.ClientHelloInfo) (*tls.Certificate, error) {
	return c.tlsProvider.GetCertificate(chi)
}

// RestartChan 返回自重启信号通道。
func (c *Container) RestartChan() <-chan struct{} { return c.restartCh }

// requestRestart 触发一次进程自重启（非阻塞）。
func (c *Container) requestRestart() {
	select {
	case c.restartCh <- struct{}{}:
	default:
	}
}

// getRawSetting 读取单条设置的原始字符串值。
func (c *Container) getRawSetting(ctx context.Context, key string) (string, bool) {
	if c.DB == nil {
		return "", false
	}
	var row model.AppSetting
	if err := c.DB.WithContext(ctx).Where("setting_key = ?", key).First(&row).Error; err != nil {
		return "", false
	}
	return row.Value, true
}

// LoadTLSSettings 启动时从「数据库优先、配置文件兜底」加载 TLS 配置，
// 设置证书提供器与安全响应头，使服务以正确的协议监听。
func (c *Container) LoadTLSSettings(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	enabled := c.Config.Server.TLS.Enabled
	certPEM, keyPEM := "", ""
	if enabled {
		if b, err := os.ReadFile(c.Config.Server.TLS.CertFile); err == nil {
			certPEM = string(b)
		}
		if b, err := os.ReadFile(c.Config.Server.TLS.KeyFile); err == nil {
			keyPEM = string(b)
		}
	}
	if c.DB != nil {
		rows, err := c.DB.WithContext(ctx).Model(&model.AppSetting{}).
			Where("setting_key IN ?", []string{model.SettingTLSEnabled, model.SettingTLSCert, model.SettingTLSKey}).
			Select("setting_key", "value").Rows()
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var k, v string
				if rows.Scan(&k, &v) != nil {
					continue
				}
				switch k {
				case model.SettingTLSEnabled:
					enabled = v == "true" || v == "1"
				case model.SettingTLSCert:
					if v != "" {
						certPEM = v
					}
				case model.SettingTLSKey:
					if v != "" {
						keyPEM = v
					}
				}
			}
		} else if c.Logger != nil {
			c.Logger.Warn("读取 TLS 设置失败，回退至配置文件", zap.Error(err))
		}
	}
	if enabled && certPEM != "" && keyPEM != "" {
		if cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM)); err == nil {
			c.tlsProvider.set(&cert, true)
			c.Config.Security.SecureHeaders = true
			if c.Config.Security.HSTSMaxAge <= 0 {
				c.Config.Security.HSTSMaxAge = 31536000
			}
			if c.Logger != nil {
				c.Logger.Info("已加载 TLS 设置，将以 HTTPS 监听")
			}
		} else {
			c.tlsProvider.set(nil, false)
			if c.Logger != nil {
				c.Logger.Error("TLS 证书/私钥无效，回退为 HTTP 监听", zap.Error(err))
			}
		}
	} else {
		c.tlsProvider.set(nil, false)
	}
}
