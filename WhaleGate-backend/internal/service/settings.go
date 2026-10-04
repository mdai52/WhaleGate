package service

import (
	"context"
	"crypto/tls"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm/clause"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
)

// Settings 全局运行时设置。
type Settings struct {
	// SelfUseMode 自用模式：开启后所有用户调用不计费、免费使用。
	SelfUseMode bool `json:"self_use_mode"`
	// SkillsEnabled 是否启用技能注入。
	SkillsEnabled bool `json:"skills_enabled"`
	// MCPEnabled 是否启用 MCP 工具（透传或代执行）。
	MCPEnabled bool `json:"mcp_enabled"`
	// MCPMaxRounds 网关代执行时的最大工具轮次。
	MCPMaxRounds int `json:"mcp_max_rounds"`
	// TLSEnabled 是否以 HTTPS 监听（由服务端直接提供 TLS）。
	TLSEnabled bool `json:"tls_enabled"`
	// TLSCert TLS 证书（PEM），响应中回显以便下载；更新时若为空则沿用库内值。
	TLSCert string `json:"tls_cert"`
	// TLSKey TLS 私钥（PEM），响应中始终为空，避免泄露；仅更新时传入。
	TLSKey string `json:"tls_key"`
	// TLSKeySet 是否已配置私钥（用于前端展示"已配置"）。
	TLSKeySet bool `json:"tls_key_set"`
	// PasskeyEnabled 是否启用通行密钥。
	PasskeyEnabled bool `json:"passkey_enabled"`
	// PasskeyRPID 通行密钥依赖方 ID。
	PasskeyRPID string `json:"passkey_rp_id"`
	// PasskeyRPOrigins 通行密钥允许的源地址，多个以英文逗号分隔。
	PasskeyRPOrigins string `json:"passkey_rp_origins"`

	// ---------------------------------------------------------------- 绘图工作台存储

	// StudioStorageDriver 存储后端：local 本地磁盘 / s3 对象存储。
	StudioStorageDriver string `json:"studio_storage_driver"`
	// StudioLocalRoot 本地存储根目录；留空时回退到 studio.data_dir 配置。
	StudioLocalRoot string `json:"studio_local_root"`
	// StudioLocalBaseURL 本地目录已由外部 Web 服务器直接服务时的公开访问前缀。
	StudioLocalBaseURL string `json:"studio_local_base_url"`
	// StudioS3Endpoint 对象存储服务地址，需带协议。
	StudioS3Endpoint string `json:"studio_s3_endpoint"`
	// StudioS3Region 区域。
	StudioS3Region string `json:"studio_s3_region"`
	// StudioS3Bucket 桶名。
	StudioS3Bucket string `json:"studio_s3_bucket"`
	// StudioS3Prefix 桶内对象键前缀。
	StudioS3Prefix string `json:"studio_s3_prefix"`
	// StudioS3AccessKey 访问密钥 ID。
	StudioS3AccessKey string `json:"studio_s3_access_key"`
	// StudioS3SecretKey 访问密钥；响应中始终为空，仅更新时传入。
	StudioS3SecretKey string `json:"studio_s3_secret_key"`
	// StudioS3SecretKeySet 是否已配置访问密钥。
	StudioS3SecretKeySet bool `json:"studio_s3_secret_key_set"`
	// StudioS3PublicBaseURL 公开访问前缀（CDN 或桶公开域名），留空则经网关代理回读。
	StudioS3PublicBaseURL string `json:"studio_s3_public_base_url"`
	// StudioS3ForcePathStyle 是否使用路径风格寻址。
	StudioS3ForcePathStyle bool `json:"studio_s3_force_path_style"`
}

// 设置默认值。
const (
	defaultMCPMaxRounds = 5
	maxMCPRounds        = 10
)

// 设置缓存：转发链路高频读取，5 秒内复用快照，数据库异常时降级为上次快照。
var (
	settingsMu    sync.RWMutex
	settingsSnap  Settings
	settingsAt    time.Time
	settingsReady bool
)

const settingsCacheTTL = 5 * time.Second

// GetSettings 读取全局设置（穿透到数据库，用于管理端展示）。
func (c *Container) GetSettings(ctx context.Context) (*Settings, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	out := Settings{}
	rows, err := c.DB.WithContext(ctx).Model(&model.AppSetting{}).Select("setting_key", "value").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		applySetting(&out, key, value)
	}
	return &out, nil
}

// UpdateSettings 更新全局设置（仅管理员可调用）。
func (c *Container) UpdateSettings(ctx context.Context, userID uint, in Settings) (*Settings, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// TLS 配置：私钥/证书为空时沿用库内已有值；启用时必须两者齐备且有效。
	enabled := in.TLSEnabled
	cert := in.TLSCert
	key := in.TLSKey
	if cert == "" {
		if v, ok := c.getRawSetting(ctx, model.SettingTLSCert); ok {
			cert = v
		}
	}
	if key == "" {
		if v, ok := c.getRawSetting(ctx, model.SettingTLSKey); ok {
			key = v
		}
	}
	if enabled {
		if cert == "" || key == "" {
			return nil, apierr.New(apierr.ErrInvalidParam, "启用 HTTPS 需同时提供证书与私钥")
		}
		if _, err := tls.X509KeyPair([]byte(cert), []byte(key)); err != nil {
			return nil, apierr.New(apierr.ErrInvalidParam, "证书或私钥无效: "+err.Error())
		}
	}

	if in.MCPMaxRounds <= 0 {
		in.MCPMaxRounds = defaultMCPMaxRounds
	}
	if in.MCPMaxRounds > maxMCPRounds {
		in.MCPMaxRounds = maxMCPRounds
	}

	// 通行密钥配置校验。
	if in.PasskeyEnabled && strings.TrimSpace(in.PasskeyRPID) == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "启用通行密钥时需填写依赖方 ID（RP ID）")
	}

	// 绘图工作台存储：驱动须为 local / s3；选 s3 时端点、桶与密钥缺一不可。
	driver, secretKey, secretCipher, err := c.resolveStudioStorage(ctx, &in)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	values := map[string]string{
		model.SettingSelfUseMode:            strconv.FormatBool(in.SelfUseMode),
		model.SettingSkillsEnabled:          strconv.FormatBool(in.SkillsEnabled),
		model.SettingMCPEnabled:             strconv.FormatBool(in.MCPEnabled),
		model.SettingMCPMaxRounds:           strconv.Itoa(in.MCPMaxRounds),
		model.SettingTLSEnabled:             strconv.FormatBool(enabled),
		model.SettingTLSCert:                cert,
		model.SettingTLSKey:                 key,
		model.SettingPasskeyEnabled:         strconv.FormatBool(in.PasskeyEnabled),
		model.SettingPasskeyRPID:            strings.TrimSpace(in.PasskeyRPID),
		model.SettingPasskeyRPOrigins:       strings.TrimSpace(in.PasskeyRPOrigins),
		model.SettingStudioStorageDriver:    driver,
		model.SettingStudioLocalRoot:        strings.TrimSpace(in.StudioLocalRoot),
		model.SettingStudioLocalBaseURL:     strings.TrimSpace(in.StudioLocalBaseURL),
		model.SettingStudioS3Endpoint:       strings.TrimSpace(in.StudioS3Endpoint),
		model.SettingStudioS3Region:         strings.TrimSpace(in.StudioS3Region),
		model.SettingStudioS3Bucket:         strings.TrimSpace(in.StudioS3Bucket),
		model.SettingStudioS3Prefix:         strings.TrimSpace(in.StudioS3Prefix),
		model.SettingStudioS3AccessKey:      strings.TrimSpace(in.StudioS3AccessKey),
		model.SettingStudioS3SecretKey:      secretCipher,
		model.SettingStudioS3PublicBaseURL:  strings.TrimSpace(in.StudioS3PublicBaseURL),
		model.SettingStudioS3ForcePathStyle: strconv.FormatBool(in.StudioS3ForcePathStyle),
	}
	for k, v := range values {
		row := model.AppSetting{Key: k, Value: v, UpdatedBy: userID, UpdatedAt: now}
		err := c.DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "setting_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
		}).Create(&row).Error
		if err != nil {
			return nil, err
		}
	}

	// 刷新缓存快照（私钥与对象存储密钥均不回显）。
	out := in
	out.TLSEnabled = enabled
	out.TLSCert = cert
	out.TLSKey = ""
	out.TLSKeySet = key != ""
	out.StudioStorageDriver = driver
	out.StudioS3SecretKey = ""
	out.StudioS3SecretKeySet = secretKey != ""
	settingsMu.Lock()
	settingsSnap = out
	settingsAt = time.Now()
	settingsReady = true
	settingsMu.Unlock()

	// 仅更换证书时热加载，无需重启即可生效。
	if enabled && cert != "" && key != "" {
		if certParsed, err := tls.X509KeyPair([]byte(cert), []byte(key)); err == nil {
			c.tlsProvider.set(&certParsed, true)
		}
	} else {
		c.tlsProvider.set(nil, false)
	}

	// 监听模式（HTTP<->HTTPS）变化需进程重启才能生效。以当前运行态为准，
	// 避免 GetSettings 偶发失败时漏触发重启。
	if c.TLSIsEnabled() != enabled {
		c.requestRestart()
	}

	// 通行密钥配置变更实时生效，无需重启。
	c.rebuildWebAuthn(ctx)

	// 存储后端保持常驻连接，配置变更后即时重建，无需重启。
	c.reloadStudioStorage(out, secretKey)

	if c.Logger != nil {
		c.Logger.Info("全局设置已更新",
			zap.Bool("self_use_mode", in.SelfUseMode),
			zap.Bool("tls_enabled", enabled),
			zap.String("studio_storage_driver", driver),
			zap.Uint("operator", userID))
	}
	return &out, nil
}

// Settings 返回全局设置快照（带进程内缓存，供转发链路高频调用）。
func (c *Container) Settings() Settings {
	settingsMu.RLock()
	snap, at, ready := settingsSnap, settingsAt, settingsReady
	settingsMu.RUnlock()

	if ready && time.Since(at) < settingsCacheTTL {
		return snap
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cur, err := c.GetSettings(ctx)
	if err != nil {
		// 数据库异常时沿用上次快照，保证转发链路不受影响。
		if c.Logger != nil {
			c.Logger.Warn("读取全局设置失败，沿用上次快照", zap.Error(err))
		}
		return snap
	}
	if cur.MCPMaxRounds <= 0 {
		cur.MCPMaxRounds = defaultMCPMaxRounds
	}

	settingsMu.Lock()
	settingsSnap = *cur
	settingsAt = time.Now()
	settingsReady = true
	settingsMu.Unlock()
	return *cur
}

// SelfUseMode 返回自用模式是否开启。
func (c *Container) SelfUseMode() bool { return c.Settings().SelfUseMode }

// applySetting 把单个键值应用到设置结构。
func applySetting(out *Settings, key, value string) {
	switch key {
	case model.SettingSelfUseMode:
		out.SelfUseMode = value == "true" || value == "1"
	case model.SettingSkillsEnabled:
		out.SkillsEnabled = value == "true" || value == "1"
	case model.SettingMCPEnabled:
		out.MCPEnabled = value == "true" || value == "1"
	case model.SettingMCPMaxRounds:
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			out.MCPMaxRounds = n
		}
	case model.SettingTLSEnabled:
		out.TLSEnabled = value == "true" || value == "1"
	case model.SettingTLSCert:
		out.TLSCert = value
	case model.SettingTLSKey:
		out.TLSKeySet = value != ""
	case model.SettingPasskeyEnabled:
		out.PasskeyEnabled = value == "true" || value == "1"
	case model.SettingPasskeyRPID:
		out.PasskeyRPID = value
	case model.SettingPasskeyRPOrigins:
		out.PasskeyRPOrigins = value
	case model.SettingStudioStorageDriver:
		out.StudioStorageDriver = value
	case model.SettingStudioLocalRoot:
		out.StudioLocalRoot = value
	case model.SettingStudioLocalBaseURL:
		out.StudioLocalBaseURL = value
	case model.SettingStudioS3Endpoint:
		out.StudioS3Endpoint = value
	case model.SettingStudioS3Region:
		out.StudioS3Region = value
	case model.SettingStudioS3Bucket:
		out.StudioS3Bucket = value
	case model.SettingStudioS3Prefix:
		out.StudioS3Prefix = value
	case model.SettingStudioS3AccessKey:
		out.StudioS3AccessKey = value
	case model.SettingStudioS3SecretKey:
		// 密钥只回显"是否已配置"，明文永不返回给客户端。
		out.StudioS3SecretKeySet = value != ""
	case model.SettingStudioS3PublicBaseURL:
		out.StudioS3PublicBaseURL = value
	case model.SettingStudioS3ForcePathStyle:
		out.StudioS3ForcePathStyle = value == "true" || value == "1"
	}
}

// SettingsSnapshot 返回当前缓存快照，便于管理端快速展示与测试。
func SettingsSnapshot() (Settings, bool) {
	settingsMu.RLock()
	defer settingsMu.RUnlock()
	return settingsSnap, settingsReady
}

// resetSettingsCache 清空设置缓存（仅供测试）。
func resetSettingsCache() {
	settingsMu.Lock()
	settingsSnap = Settings{}
	settingsAt = time.Time{}
	settingsReady = false
	settingsMu.Unlock()
}
