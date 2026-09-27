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

// BootstrapAdminUsername 初始化管理员账号名。
const BootstrapAdminUsername = "admin"

// BootstrapResult 初始化结果；Password 只用于首次打印，不落库、不返回给 API。
type BootstrapResult struct {
	User     *model.User
	Password string
	Created  bool
}

// BootstrapAdmin 首次安装时创建管理员账号。
//
// 安全约定：
//  1. 仅在 users 表为空时创建，因此随机密码只会生成一次；
//  2. 明文密码不写入数据库（只保存 bcrypt 摘要），也不通过任何接口返回，仅由调用方打印到日志；
//  3. 创建后标记 must_change_password，首次登录必须改密。
func (c *Container) BootstrapAdmin(ctx context.Context) (*BootstrapResult, error) {
	var count int64
	if err := c.DB.WithContext(ctx).Model(&model.User{}).Count(&count).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	if count > 0 {
		return &BootstrapResult{Created: false}, nil
	}

	password, err := crypto.GeneratePassword(c.Config.Security.BootstrapPasswordLength)
	if err != nil {
		return nil, apierr.Wrap(apierr.ErrInternal, err)
	}

	user, err := c.Register(ctx, RegisterInput{
		Username: BootstrapAdminUsername,
		Password: password,
		Nickname: "系统管理员",
		Role:     constant.RoleAdmin,
	}, true)
	if err != nil {
		return nil, err
	}

	if err := c.DB.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", user.ID).Update("must_change_password", true).Error; err != nil {
		return nil, apierr.Wrap(apierr.ErrDatabase, err)
	}
	user.MustChangePassword = true

	if c.Logger != nil {
		c.Logger.Warn("初始化完成：已创建管理员账号，初始密码仅在此处显示一次，登录后请立即修改",
			zap.String("username", BootstrapAdminUsername),
			zap.String("password", password),
			zap.String("hint", "docker compose logs app | grep -i password"))
	}

	return &BootstrapResult{User: user, Password: password, Created: true}, nil
}

// ChangePassword 修改当前账号密码，成功后清除"必须改密"标记并返回新令牌
// （旧令牌仍带 must_change 标记，签发新令牌可无缝继续使用）。
func (c *Container) ChangePassword(ctx context.Context, userID uint, oldPassword, newPassword string) (string, error) {
	if userID == 0 {
		return "", apierr.New(apierr.ErrUnauthorized, "请先登录")
	}
	if err := crypto.ValidatePassword(newPassword); err != nil {
		return "", apierr.New(apierr.ErrInvalidParam, "新"+err.Error())
	}
	if strings.TrimSpace(oldPassword) == "" {
		return "", apierr.New(apierr.ErrInvalidParam, "请输入原密码")
	}

	var user model.User
	if err := c.DB.WithContext(ctx).First(&user, userID).Error; err != nil {
		return "", apierr.Wrap(apierr.ErrNotFound, err)
	}
	if err := comparePassword(user.PasswordHash, oldPassword); err != nil {
		return "", apierr.New(apierr.ErrInvalidCredential, "原密码不正确")
	}
	if newPassword == oldPassword {
		return "", apierr.New(apierr.ErrInvalidParam, "新密码不能与原密码相同")
	}

	hash, err := bcryptHash(newPassword)
	if err != nil {
		return "", apierr.Wrap(apierr.ErrInternal, err)
	}
	if err := c.DB.WithContext(ctx).Model(&model.User{}).Where("id = ?", userID).
		Updates(map[string]interface{}{
			"password_hash":        string(hash),
			"must_change_password": false,
		}).Error; err != nil {
		return "", apierr.Wrap(apierr.ErrDatabase, err)
	}

	user.MustChangePassword = false
	token, err := c.JWT.Generate(jwtClaimsFor(&user))
	if err != nil {
		return "", apierr.Wrap(apierr.ErrInternal, err)
	}

	if c.Logger != nil {
		c.Logger.Info("用户已修改密码", zap.Uint("user_id", userID))
	}
	return token, nil
}
