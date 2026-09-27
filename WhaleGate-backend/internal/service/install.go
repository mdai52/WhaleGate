package service

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"github.com/whalegate/whalegate/internal/constant"
	"github.com/whalegate/whalegate/internal/model"
	"github.com/whalegate/whalegate/internal/pkg/apierr"
	"github.com/whalegate/whalegate/internal/pkg/crypto"
)

// InstallInput 首次安装入参。
type InstallInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email"`
	Nickname string `json:"nickname"`
}

// InstallResult 安装结果：管理员账号与可直接使用的令牌。
type InstallResult struct {
	User  *model.User `json:"user"`
	Token string      `json:"token"`
}

// SystemStatus 系统初始化状态（公开信息，不含敏感数据）。
type SystemStatus struct {
	// Initialized 是否已完成初始化（已有账号）。
	Initialized bool `json:"initialized"`
	// AllowRegistration 是否开放注册。
	AllowRegistration bool `json:"allow_registration"`
	// Version 服务端版本。
	Version string `json:"version"`
}

// SystemStatus 查询系统初始化状态。
func (c *Container) SystemStatus(ctx context.Context, version string) (*SystemStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var count int64
	if err := c.DB.WithContext(ctx).Model(&model.User{}).Count(&count).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	return &SystemStatus{
		Initialized:       count > 0,
		AllowRegistration: c.Config.Security.AllowRegistration,
		Version:           version,
	}, nil
}

// Install 首次安装：创建管理员账号并签发令牌。
// 仅在系统尚无账号时可用，避免被用于提权。
func (c *Container) Install(ctx context.Context, in InstallInput) (*InstallResult, error) {
	var count int64
	if err := c.DB.WithContext(ctx).Model(&model.User{}).Count(&count).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	if count > 0 {
		return nil, apierr.New(apierr.ErrForbidden, "系统已完成初始化，请直接登录")
	}

	username := strings.TrimSpace(in.Username)
	if username == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "请填写管理员账号")
	}
	if err := crypto.ValidatePassword(in.Password); err != nil {
		return nil, apierr.New(apierr.ErrInvalidParam, err.Error())
	}

	user, err := c.Register(ctx, RegisterInput{
		Username: username,
		Password: in.Password,
		Email:    strings.TrimSpace(in.Email),
		Nickname: defaultString(strings.TrimSpace(in.Nickname), "系统管理员"),
		Role:     constant.RoleAdmin,
	}, true)
	if err != nil {
		return nil, err
	}

	token, err := c.JWT.Generate(jwtClaimsFor(user))
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInternal, err)
	}

	if c.Logger != nil {
		c.Logger.Info("系统初始化完成，已创建管理员账号",
			zap.String("username", username), zap.Uint("user_id", user.ID))
	}
	return &InstallResult{User: user, Token: token}, nil
}

func defaultString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
