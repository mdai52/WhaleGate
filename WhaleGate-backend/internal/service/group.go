package service

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/gorm"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
)

// DefaultGroupName 预置的默认分组标识，由迁移写入且不允许删除。
const DefaultGroupName = "default"

// groupCacheKey 分组表进程内缓存键。
const groupCacheKey = "key_groups"

// groupNamePattern 分组标识允许的字符：字母、数字、下划线、中划线。
var groupNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{2,64}$`)

// GroupPolicy 分组解析后的生效策略。
type GroupPolicy struct {
	// Tag 命中的分组标识；未命中时为空。
	Tag string `json:"tag"`
	// RatioMultiplier 倍率系数；未命中时为 1（不加价）。
	RatioMultiplier float64 `json:"ratio_multiplier"`
	// QPM 分组级每分钟请求上限，0 表示该层不设限。
	QPM int `json:"qpm"`
	// Concurrency 分组级并发上限，0 表示该层不设限。
	Concurrency int `json:"concurrency"`
}

// GroupInput 分组创建 / 更新入参。
type GroupInput struct {
	// Name 分组标识，仅创建时生效（更新时不允许改名）。
	Name string `json:"name"`
	// DisplayName 展示名，留空时回落到标识本身。
	DisplayName string `json:"display_name"`
	// Description 说明。
	Description string `json:"description"`
	// RatioMultiplier 倍率系数，<=0 时按 1 处理。
	RatioMultiplier float64 `json:"ratio_multiplier"`
	// QPM 分组级每分钟请求上限。
	QPM int `json:"qpm"`
	// Concurrency 分组级并发上限。
	Concurrency int `json:"concurrency"`
	// Enabled 是否启用；nil 表示启用。
	Enabled *bool `json:"enabled"`
}

// normalized 校验并归一化入参。name 由调用方给出：
// 创建时取用户输入，更新时取库中已有标识（标识不允许修改）。
func (in GroupInput) normalized(name string) (*model.KeyGroup, error) {
	name = strings.TrimSpace(name)
	if !groupNamePattern.MatchString(name) {
		return nil, apierr.New(apierr.ErrInvalidParam, "分组标识仅支持字母、数字、下划线与中划线，长度 2-64")
	}

	multiplier := in.RatioMultiplier
	if multiplier <= 0 {
		multiplier = 1
	}
	if multiplier > 1000 {
		return nil, apierr.New(apierr.ErrInvalidParam, "倍率系数不得超过 1000")
	}
	if in.QPM < 0 || in.Concurrency < 0 {
		return nil, apierr.New(apierr.ErrInvalidParam, "QPM 与并发上限不能为负数")
	}

	displayName := strings.TrimSpace(in.DisplayName)
	if displayName == "" {
		displayName = name
	}
	if utf8.RuneCountInString(displayName) > 64 {
		return nil, apierr.New(apierr.ErrInvalidParam, "分组名称不能超过 64 个字符")
	}
	description := strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(description) > 255 {
		return nil, apierr.New(apierr.ErrInvalidParam, "说明不能超过 255 个字符")
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}

	return &model.KeyGroup{
		Name:            name,
		DisplayName:     displayName,
		Description:     description,
		RatioMultiplier: multiplier,
		QPM:             in.QPM,
		Concurrency:     in.Concurrency,
		Enabled:         enabled,
	}, nil
}

// ---------------------------------------------------------------- 策略解析

// GroupPolicyFor 解析分组标识对应的生效策略。
//
// 未指定、未匹配、已删除或已禁用的分组一律返回中性策略（系数 1、不额外限流）：
// 存量密钥上的历史自由文本标签、以及分组被删除后的情形，
// 都不应该让调用直接失败。
func (c *Container) GroupPolicyFor(ctx context.Context, tag string) GroupPolicy {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return neutralGroupPolicy()
	}

	table, err := c.groupTable(ctx)
	if err != nil {
		if c.Logger != nil {
			c.Logger.Warn("读取分组策略失败，按中性策略放行", zap.String("group", tag), zap.Error(err))
		}
		return neutralGroupPolicy()
	}
	return policyFromTable(table, tag)
}

// neutralGroupPolicy 中性策略：不加价、不额外限流，等同于未分组。
func neutralGroupPolicy() GroupPolicy {
	return GroupPolicy{RatioMultiplier: 1}
}

// policyFromTable 由分组表得出生效策略；未命中或已禁用时回落中性策略。
//
// 单独抽出来是为了让"分组缺失时不能让调用失败"这条契约可以被直接测试。
func policyFromTable(table map[string]model.KeyGroup, tag string) GroupPolicy {
	group, ok := table[tag]
	if !ok || !group.Enabled {
		return neutralGroupPolicy()
	}
	multiplier := group.RatioMultiplier
	if multiplier <= 0 {
		multiplier = 1
	}
	return GroupPolicy{
		Tag:             group.Name,
		RatioMultiplier: multiplier,
		QPM:             group.QPM,
		Concurrency:     group.Concurrency,
	}
}

// groupTable 读取启用中的分组表（进程内缓存 60 秒，与倍率表同策略）。
//
// 不把分组策略折进 KeyIdentity：密钥缓存最长 24 小时，
// 折进去会让分组调整长时间不生效。
func (c *Container) groupTable(ctx context.Context) (map[string]model.KeyGroup, error) {
	if c.KeyCache != nil {
		if v, ok := c.KeyCache.Get(groupCacheKey); ok {
			if table, ok := v.(map[string]model.KeyGroup); ok {
				return table, nil
			}
		}
	}

	var list []model.KeyGroup
	if err := c.DB.WithContext(ctx).Where("enabled = ?", true).Find(&list).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	table := make(map[string]model.KeyGroup, len(list))
	for _, group := range list {
		table[group.Name] = group
	}
	if c.KeyCache != nil {
		c.KeyCache.SetWithTTL(groupCacheKey, table, 60*time.Second)
	}
	return table, nil
}

// invalidateGroupCache 分组变更后失效缓存。
func (c *Container) invalidateGroupCache() {
	if c.KeyCache != nil {
		c.KeyCache.Delete(groupCacheKey)
	}
}

// ---------------------------------------------------------------- CRUD

// ListGroups 列出全部分组（含已禁用，便于管理端查看）。
func (c *Container) ListGroups(ctx context.Context) ([]model.KeyGroup, error) {
	var list []model.KeyGroup
	if err := c.DB.WithContext(ctx).Order("id ASC").Find(&list).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return list, nil
}

// ListEnabledGroups 列出启用中的分组，供密钥表单选择使用。
func (c *Container) ListEnabledGroups(ctx context.Context) ([]model.KeyGroup, error) {
	var list []model.KeyGroup
	if err := c.DB.WithContext(ctx).Where("enabled = ?", true).Order("id ASC").Find(&list).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return list, nil
}

// EnsureGroupExists 校验分组标识存在；空标识表示不指定分组。
//
// 只在密钥新增 / 修改时校验，不影响存量密钥：
// 历史自由文本标签仍可正常调用（按中性策略），只是不能再被新建引用。
func (c *Container) EnsureGroupExists(ctx context.Context, tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil
	}
	var count int64
	if err := c.DB.WithContext(ctx).Model(&model.KeyGroup{}).
		Where("name = ?", tag).Count(&count).Error; err != nil {
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	if count == 0 {
		return apierr.New(apierr.ErrInvalidParam, "分组 "+tag+" 不存在")
	}
	return nil
}

// CreateGroup 新建分组。
//
// 若同名分组此前被软删除，直接恢复并覆盖为新配置：
// 否则标识会被历史记录长期占用，管理员无法重新创建同名分组。
func (c *Container) CreateGroup(ctx context.Context, in GroupInput) (*model.KeyGroup, error) {
	group, err := in.normalized(in.Name)
	if err != nil {
		return nil, err
	}

	var existing model.KeyGroup
	findErr := c.DB.WithContext(ctx).Unscoped().Where("name = ?", group.Name).First(&existing).Error
	switch {
	case findErr == nil && !existing.DeletedAt.Valid:
		return nil, apierr.New(apierr.ErrConflict, "分组标识 "+group.Name+" 已存在")
	case findErr == nil:
		existing.DeletedAt = gorm.DeletedAt{}
		existing.DisplayName = group.DisplayName
		existing.Description = group.Description
		existing.RatioMultiplier = group.RatioMultiplier
		existing.QPM = group.QPM
		existing.Concurrency = group.Concurrency
		existing.Enabled = group.Enabled
		if err := c.DB.WithContext(ctx).Unscoped().Save(&existing).Error; err != nil {
			return nil, apierr.Wrap(apierr.ErrDatabase, err)
		}
		c.invalidateGroupCache()
		return &existing, nil
	case !errors.Is(findErr, gorm.ErrRecordNotFound):
		return nil, apierr.Wrap(apierr.ErrDatabase, findErr)
	}

	if err := c.DB.WithContext(ctx).Create(group).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, apierr.New(apierr.ErrConflict, "分组标识 "+group.Name+" 已存在")
		}
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	c.invalidateGroupCache()
	return group, nil
}

// UpdateGroup 更新分组策略。
//
// 标识是密钥的引用键，改名会让存量密钥失联，因此只允许改展示信息与策略。
func (c *Container) UpdateGroup(ctx context.Context, id uint, in GroupInput) (*model.KeyGroup, error) {
	var group model.KeyGroup
	if err := c.DB.WithContext(ctx).First(&group, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, apierr.New(apierr.ErrNotFound, "分组不存在")
		}
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}

	next, err := in.normalized(group.Name)
	if err != nil {
		return nil, err
	}
	group.DisplayName = next.DisplayName
	group.Description = next.Description
	group.RatioMultiplier = next.RatioMultiplier
	group.QPM = next.QPM
	group.Concurrency = next.Concurrency
	group.Enabled = next.Enabled

	if err := c.DB.WithContext(ctx).Save(&group).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	c.invalidateGroupCache()
	return &group, nil
}

// DeleteGroup 删除分组（软删除）。
//
// 归属该分组的密钥不受影响，只会在下次调用时回落为中性策略，
// 避免删除配置导致线上调用直接中断。
func (c *Container) DeleteGroup(ctx context.Context, id uint) error {
	var group model.KeyGroup
	if err := c.DB.WithContext(ctx).First(&group, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return apierr.New(apierr.ErrNotFound, "分组不存在")
		}
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	if group.Name == DefaultGroupName {
		return apierr.New(apierr.ErrInvalidParam, "默认分组不可删除，可将其禁用")
	}
	if err := c.DB.WithContext(ctx).Delete(&group).Error; err != nil {
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	c.invalidateGroupCache()
	return nil
}
