package service

import (
	"context"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm/clause"

	"github.com/whalegate/whalegate/internal/model"
)

// Settings 全局运行时设置。
type Settings struct {
	// SelfUseMode 自用模式：开启后所有用户调用不计费、免费使用。
	SelfUseMode bool `json:"self_use_mode"`
}

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
	now := time.Now()
	values := map[string]string{
		model.SettingSelfUseMode: strconv.FormatBool(in.SelfUseMode),
	}
	for key, value := range values {
		row := model.AppSetting{Key: key, Value: value, UpdatedBy: userID, UpdatedAt: now}
		err := c.DB.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "setting_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
		}).Create(&row).Error
		if err != nil {
			return nil, err
		}
	}

	// 立即刷新缓存，避免最长 5 秒的生效延迟。
	settingsMu.Lock()
	settingsSnap = in
	settingsAt = time.Now()
	settingsReady = true
	settingsMu.Unlock()

	if c.Logger != nil {
		c.Logger.Info("全局设置已更新",
			zap.Bool("self_use_mode", in.SelfUseMode),
			zap.Uint("operator", userID))
	}
	return &in, nil
}

// SelfUseMode 返回自用模式是否开启（带进程内缓存，供转发链路高频调用）。
func (c *Container) SelfUseMode() bool {
	settingsMu.RLock()
	snap, at, ready := settingsSnap, settingsAt, settingsReady
	settingsMu.RUnlock()

	if ready && time.Since(at) < settingsCacheTTL {
		return snap.SelfUseMode
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cur, err := c.GetSettings(ctx)
	if err != nil {
		// 数据库异常时沿用上次快照，保证转发链路不受影响。
		if c.Logger != nil {
			c.Logger.Warn("读取全局设置失败，沿用上次快照", zap.Error(err))
		}
		return snap.SelfUseMode
	}

	settingsMu.Lock()
	settingsSnap = *cur
	settingsAt = time.Now()
	settingsReady = true
	settingsMu.Unlock()
	return cur.SelfUseMode
}

// applySetting 把单个键值应用到设置结构。
func applySetting(out *Settings, key, value string) {
	switch key {
	case model.SettingSelfUseMode:
		out.SelfUseMode = value == "true" || value == "1"
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
