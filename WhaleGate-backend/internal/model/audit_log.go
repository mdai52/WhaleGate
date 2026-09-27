package model

import "time"

// 审计状态。
const (
	AuditStatusSuccess = 1
	AuditStatusFailed  = 2
)

// 审计动作。
const (
	AuditLogin            = "login"
	AuditRegister         = "register"
	AuditLogout           = "logout"
	AuditChangePassword   = "change_password"
	AuditKeyCreate        = "key.create"
	AuditKeyRevoke        = "key.revoke"
	AuditKeyDelete        = "key.delete"
	AuditChannelCreate    = "channel.create"
	AuditChannelUpdate    = "channel.update"
	AuditChannelDelete    = "channel.delete"
	AuditChannelTest      = "channel.test"
	AuditQuotaAdjust      = "quota.adjust"
	AuditUserCreate       = "user.create"
	AuditUserStatus       = "user.status"
	AuditRatioUpsert      = "ratio.upsert"
	AuditRatioDelete      = "ratio.delete"
	AuditCredentialImport = "credential.import"
	AuditCredentialBind   = "credential.bind"
	AuditCredentialDelete = "credential.delete"
	AuditIdentityBind     = "identity.bind"
	AuditIdentityUnbind   = "identity.unbind"
	AuditPasskeyRegister  = "passkey.register"
	AuditPasskeyDelete    = "passkey.delete"
	AuditSettingsUpdate   = "settings.update"
)

// AuditLog 安全相关操作的审计留痕。
type AuditLog struct {
	ID         uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID     uint      `gorm:"not null;default:0;index" json:"user_id"`
	Username   string    `gorm:"size:64" json:"username,omitempty"`
	Action     string    `gorm:"size:64;not null;index" json:"action"`
	TargetType string    `gorm:"size:64" json:"target_type,omitempty"`
	TargetID   string    `gorm:"size:64" json:"target_id,omitempty"`
	Status     int       `gorm:"not null;default:1" json:"status"`
	Detail     string    `gorm:"type:text" json:"detail,omitempty"`
	IP         string    `gorm:"size:64" json:"ip,omitempty"`
	UserAgent  string    `gorm:"size:512" json:"user_agent,omitempty"`
	TraceID    string    `gorm:"size:64" json:"trace_id,omitempty"`
	CreatedAt  time.Time `gorm:"index" json:"created_at"`
}

// TableName 表名。
func (AuditLog) TableName() string { return "audit_logs" }
