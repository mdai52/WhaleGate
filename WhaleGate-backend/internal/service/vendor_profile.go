package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/gateway/vendor"
	"go.uber.org/zap"
)

// LoadVendorProfiles 把表里的厂商档案灌入识别用的内存存储。
//
// 表为空是合法状态（例如管理员把内置档案全删了）：此时识别一律回落通用形态，
// 只是失去厂商差异适配，不会影响转发本身。
func (c *Container) LoadVendorProfiles(ctx context.Context) error {
	var rows []model.VendorProfile
	if err := c.DB.WithContext(ctx).
		Where("enabled = ?", true).
		Order("sort DESC, id ASC").
		Find(&rows).Error; err != nil {
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	list := make([]vendor.Vendor, 0, len(rows))
	for i := range rows {
		v := rows[i].Profile()
		if v == nil {
			if c.Logger != nil {
				c.Logger.Warn("厂商档案解析失败，已跳过", zap.String("code", rows[i].Code))
			}
			continue
		}
		// 关键词以 match_hosts 列为准：它是管理员在后台直接编辑的字段，
		// config 里的 hosts 由 ApplyConfig 同步写入，两者一致，这里再覆盖一次以防手工改库。
		if hosts := rows[i].Hosts(); len(hosts) > 0 {
			v.Hosts = hosts
		}
		list = append(list, *v)
	}
	vendor.Load(list)
	return nil
}

// reloadVendorProfiles 档案变更后重新灌入内存。
func (c *Container) reloadVendorProfiles(ctx context.Context) {
	if err := c.LoadVendorProfiles(ctx); err != nil && c.Logger != nil {
		c.Logger.Warn("重载厂商档案失败", zap.Error(err))
	}
}

// ListVendorProfiles 列出全部档案（含停用）。
func (c *Container) ListVendorProfiles(ctx context.Context) ([]model.VendorProfile, error) {
	var rows []model.VendorProfile
	if err := c.DB.WithContext(ctx).Order("sort DESC, id ASC").Find(&rows).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return rows, nil
}

// GetVendorProfile 读取单个档案。
func (c *Container) GetVendorProfile(ctx context.Context, id uint) (*model.VendorProfile, error) {
	var row model.VendorProfile
	if err := c.DB.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrNotFound, err)
	}
	return &row, nil
}

// VendorProfileInput 档案保存入参。
type VendorProfileInput struct {
	Code       string         `json:"code"`
	Name       string         `json:"name"`
	GroupName  string         `json:"group_name"`
	MatchHosts string         `json:"match_hosts"`
	Enabled    *bool          `json:"enabled"`
	Sort       int            `json:"sort"`
	Config     *vendor.Vendor `json:"config"`
}

// normalize 校验并补齐入参。
func (in VendorProfileInput) normalize() (VendorProfileInput, error) {
	in.Code = strings.TrimSpace(in.Code)
	if in.Code == "" {
		return in, apierr.New(apierr.ErrInvalidParam, "档案代码不能为空")
	}
	if len(in.Code) > 64 {
		return in, apierr.New(apierr.ErrInvalidParam, "档案代码不能超过 64 个字符")
	}
	if in.Config == nil {
		return in, apierr.New(apierr.ErrInvalidParam, "缺少档案详情")
	}
	// 代码与详情里的 id 必须一致：识别是按 id 回查的，两者不一致会导致查得到却匹配不上。
	in.Config.ID = in.Code
	if strings.TrimSpace(in.Config.Name) == "" {
		in.Config.Name = in.Code
	}
	return in, nil
}

// CreateVendorProfile 新建档案。
func (c *Container) CreateVendorProfile(ctx context.Context, in VendorProfileInput) (*model.VendorProfile, error) {
	in, err := in.normalize()
	if err != nil {
		return nil, err
	}
	row := &model.VendorProfile{
		Code:      in.Code,
		Enabled:   true,
		Sort:      in.Sort,
		GroupName: in.Config.Group,
	}
	if in.Enabled != nil {
		row.Enabled = *in.Enabled
	}
	if err := row.ApplyConfig(in.Config); err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	if err := c.DB.WithContext(ctx).Create(row).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, apierr.New(apierr.ErrConflict, fmt.Sprintf("档案代码 %s 已存在", in.Code))
		}
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	c.reloadVendorProfiles(ctx)
	return row, nil
}

// UpdateVendorProfile 更新档案。
func (c *Container) UpdateVendorProfile(ctx context.Context, id uint, in VendorProfileInput) (*model.VendorProfile, error) {
	row, err := c.GetVendorProfile(ctx, id)
	if err != nil {
		return nil, err
	}
	in, err = in.normalize()
	if err != nil {
		return nil, err
	}
	row.Code = in.Code
	row.Sort = in.Sort
	row.GroupName = in.Config.Group
	if in.Enabled != nil {
		row.Enabled = *in.Enabled
	}
	if err := row.ApplyConfig(in.Config); err != nil {
		return nil, apierr.Wrap(apierr.ErrInvalidParam, err)
	}
	if err := c.DB.WithContext(ctx).Save(row).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, apierr.New(apierr.ErrConflict, fmt.Sprintf("档案代码 %s 已存在", in.Code))
		}
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	c.reloadVendorProfiles(ctx)
	return row, nil
}

// DeleteVendorProfile 删除档案。
func (c *Container) DeleteVendorProfile(ctx context.Context, id uint) error {
	row, err := c.GetVendorProfile(ctx, id)
	if err != nil {
		return err
	}
	if err := c.DB.WithContext(ctx).Delete(&model.VendorProfile{}, id).Error; err != nil {
		return apierr.Wrap(apierr.ErrDatabase, err)
	}
	c.reloadVendorProfiles(ctx)
	_ = row
	return nil
}

// VendorPresets 返回可用于「接入预设」的档案（已启用），供前端下拉。
func (c *Container) VendorPresets() []vendor.Vendor {
	return vendor.All()
}
