package service

import (
	"context"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"

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
		Initialized: count > 0,
		// 注册开关已改由数据库设置裁决（config.yaml 里的同名项不再参与），
		// 这里必须读同一份来源，否则会出现"接口说已关闭、实际仍能注册"。
		AllowRegistration: c.RegistrationAllowed(),
		Version:           version,
	}, nil
}

// installAdvisoryLockKey 首次安装使用的 PostgreSQL 顾问锁键。
const installAdvisoryLockKey = int64(0x5747_0001)

// Install 首次安装：创建管理员账号并签发令牌。
// 仅在系统尚无账号时可用，避免被用于提权。
func (c *Container) Install(ctx context.Context, in InstallInput) (*InstallResult, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" {
		return nil, apierr.New(apierr.ErrInvalidParam, "请填写管理员账号")
	}
	if err := crypto.ValidatePassword(in.Password); err != nil {
		return nil, apierr.New(apierr.ErrInvalidParam, err.Error())
	}

	var user *model.User
	// 判空与落库必须在同一个事务里：先 COUNT 再 INSERT 的话，
	// 两个并发请求会同时看到"还没有用户"，各自创建一个管理员。
	// 顾问锁随事务自动释放，保证同一时刻只有一个安装流程能进入。
	err := c.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", installAdvisoryLockKey).Error; err != nil {
			return apierr.Wrap(apierr.ErrDatabase, err)
		}
		var count int64
		if err := tx.Model(&model.User{}).Count(&count).Error; err != nil {
			return apierr.Wrap(apierr.ErrDatabase, err)
		}
		if count > 0 {
			return apierr.New(apierr.ErrForbidden, "系统已完成初始化，请直接登录")
		}
		created, err := c.registerOn(ctx, tx, RegisterInput{
			Username: username,
			Password: in.Password,
			Email:    strings.TrimSpace(in.Email),
			Nickname: defaultString(strings.TrimSpace(in.Nickname), "系统管理员"),
			Role:     constant.RoleAdmin,
		}, true)
		if err != nil {
			return err
		}
		user = created
		return nil
	})
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
