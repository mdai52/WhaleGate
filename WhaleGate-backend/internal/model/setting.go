package model

import "time"

// 全局设置键名。
const (
	// SettingSelfUseMode 自用模式：开启后所有调用不计费。
	SettingSelfUseMode = "self_use_mode"
	// SettingSkillsEnabled 是否启用技能注入。
	SettingSkillsEnabled = "skills_enabled"
	// SettingMCPEnabled 是否启用 MCP 工具。
	SettingMCPEnabled = "mcp_enabled"
	// SettingMCPMaxRounds 网关代执行的最大工具轮次。
	SettingMCPMaxRounds = "mcp_max_rounds"
	// SettingTLSEnabled 是否以 HTTPS 监听（由服务端直接提供 TLS）。
	SettingTLSEnabled = "tls_enabled"
	// SettingTLSCert TLS 证书（PEM，可公开）。
	SettingTLSCert = "tls_cert"
	// SettingTLSKey TLS 私钥（PEM，敏感，仅存储不回显）。
	SettingTLSKey = "tls_key"
	// SettingPasskeyEnabled 是否启用通行密钥。
	SettingPasskeyEnabled = "passkey_enabled"
	// SettingPasskeyRPID 通行密钥依赖方 ID（当前域名或父域）。
	SettingPasskeyRPID = "passkey_rp_id"
	// SettingPasskeyRPOrigins 通行密钥允许的源地址，多个以英文逗号分隔。
	SettingPasskeyRPOrigins = "passkey_rp_origins"

	// ---------------------------------------------------------------- 接入与工作台

	// SettingParamPassthrough 自定义参数透传：能力表之外的参数转发给上游。
	SettingParamPassthrough = "param_passthrough"
	// SettingAllowRegistration 是否开放注册（false 时仅管理员可创建用户）。
	SettingAllowRegistration = "allow_registration"
	// SettingVideoMaxSeconds 视频单次生成最大秒数。
	SettingVideoMaxSeconds = "video_max_seconds"

	// ---------------------------------------------------------------- 技能导入

	// SettingSkillsMaxUploadBytes 技能压缩包上传体积上限（字节）。
	SettingSkillsMaxUploadBytes = "skills_max_upload_bytes"
	// SettingSkillsMaxTotalBytes 解压后的总字节数上限。
	SettingSkillsMaxTotalBytes = "skills_max_total_bytes"
	// SettingSkillsMaxFileBytes 单个技能文件的大小上限。
	SettingSkillsMaxFileBytes = "skills_max_file_bytes"
	// SettingSkillsMaxEntries 压缩包内条目数上限。
	SettingSkillsMaxEntries = "skills_max_entries"

	// ---------------------------------------------------------------- 绘图工作台存储

	// SettingStudioStorageDriver 存储后端：local 本地磁盘 / s3 对象存储。
	SettingStudioStorageDriver = "studio_storage_driver"
	// SettingStudioLocalRoot 本地存储根目录；留空时回退到 studio.data_dir 配置。
	SettingStudioLocalRoot = "studio_local_root"
	// SettingStudioLocalBaseURL 本地目录已由外部 Web 服务器直接服务时的公开访问前缀。
	SettingStudioLocalBaseURL = "studio_local_base_url"
	// SettingStudioS3Endpoint 对象存储服务地址，需带协议。
	SettingStudioS3Endpoint = "studio_s3_endpoint"
	// SettingStudioS3Region 区域。
	SettingStudioS3Region = "studio_s3_region"
	// SettingStudioS3Bucket 桶名。
	SettingStudioS3Bucket = "studio_s3_bucket"
	// SettingStudioS3Prefix 桶内对象键前缀。
	SettingStudioS3Prefix = "studio_s3_prefix"
	// SettingStudioS3AccessKey 访问密钥 ID。
	SettingStudioS3AccessKey = "studio_s3_access_key"
	// SettingStudioS3SecretKey 访问密钥密文（AES-256-GCM，仅存储不回显）。
	SettingStudioS3SecretKey = "studio_s3_secret_key"
	// SettingStudioS3PublicBaseURL 公开访问前缀（CDN 或桶公开域名），留空则经网关代理回读。
	SettingStudioS3PublicBaseURL = "studio_s3_public_base_url"
	// SettingStudioS3ForcePathStyle 是否使用路径风格寻址（自建 MinIO 与部分国内厂商需要）。
	SettingStudioS3ForcePathStyle = "studio_s3_force_path_style"
)

// AppSetting 全局运行时设置，value 存放 JSON 文本。
type AppSetting struct {
	Key       string    `gorm:"column:setting_key;primaryKey;size:64" json:"key"`
	Value     string    `gorm:"column:value;type:text" json:"value"`
	UpdatedBy uint      `gorm:"column:updated_by" json:"updated_by,omitempty"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
}

// TableName 表名。
func (AppSetting) TableName() string { return "app_settings" }
