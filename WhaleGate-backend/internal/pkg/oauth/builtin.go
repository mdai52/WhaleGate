package oauth

// 内置主流 AI 服务供应商预设。
//
// client_id 来自各厂商官方 CLI 公开内嵌值（与开源社区通用做法一致）；
// 配置文件 oauth.providers 中同 ID 的声明会覆盖内置值，端点变更时无需改代码。
func init() {
	register(&Provider{
		ID:            "kimi-cn",
		Name:          "Kimi 国内站（kimi.com）",
		Description:   "通过设备授权登录 kimi.com 国内站，自动获取并保存 kimi-ai 认证文件；国际站账号请使用独立的 kimi 国际站登录入口。",
		Flow:          FlowDeviceCode,
		DeviceAuthURL: "https://auth.kimi.com/api/oauth/device_authorization",
		TokenURL:      "https://auth.kimi.com/api/oauth/token",
		ClientID:      "17e03f7a-456d-4a49-b48a-cf9ec1d3c6d4",
	})

	register(&Provider{
		ID:            "kimi-intl",
		Name:          "Kimi 国际站（kimi.ai）",
		Description:   "通过设备授权登录 kimi.ai 国际站，自动获取并保存独立的 kimi-ai 认证文件；国内站账号请使用独立的 kimi 国内站登录入口。",
		Flow:          FlowDeviceCode,
		DeviceAuthURL: "https://auth.kimi.ai/api/oauth/device_authorization",
		TokenURL:      "https://auth.kimi.ai/api/oauth/token",
		ClientID:      "17e03f7a-456d-4a49-b48a-cf9ec1d3c6d4",
	})

	register(&Provider{
		ID:          "codex",
		Name:        "Codex OAuth",
		Description: "通过 OAuth 授权登录 OpenAI Codex 服务，授权后从浏览器地址栏复制授权码粘贴回来，自动获取并保存认证文件。",
		Flow:        FlowAuthorizationCode,
		AuthURL:     "https://auth.openai.com/oauth/authorize",
		TokenURL:    "https://auth.openai.com/oauth/token",
		ClientID:    "app_EMoioEEJgrjBhLbBQ4Zxdnsn",
		Scopes:      []string{"openid", "profile", "email", "offline_access"},
		UsesPKCE:    true,
	})

	register(&Provider{
		ID:          "anthropic",
		Name:        "Anthropic OAuth",
		Description: "通过 OAuth 授权获取 Anthropic（Claude）服务凭证，授权完成后页面会显示授权码，复制粘贴回来即可自动保存认证文件。",
		Flow:        FlowAuthorizationCode,
		AuthURL:     "https://claude.ai/oauth/authorize",
		TokenURL:    "https://console.anthropic.com/v1/oauth/token",
		ClientID:    "9d1c250a-e61b-44e9-88ed-5944d39283f2",
		Scopes:      []string{"org:create_api_key", "user:profile", "user:inference"},
		UsesPKCE:    true,
		TokenBody:   BodyJSON,
	})

	register(&Provider{
		ID:           "gemini",
		Name:         "Google（Gemini / Antigravity）OAuth",
		Description:  "通过 OAuth 登录 Google 服务（Gemini CLI 同源凭证），自动获取并保存认证文件，可用于 Gemini 系渠道绑定。",
		Flow:         FlowAuthorizationCode,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		ClientID:     "681255809395-oo8ft2oprdrnp9e3aqf6av3hmdib135j.apps.googleusercontent.com",
		ClientSecret: "GocSPY-aS3hPdD2pv8nXfUzjSSkjQIY8bT",
		Scopes: []string{
			"https://www.googleapis.com/auth/cloud-platform",
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
			"openid",
		},
		UsesPKCE: true,
	})
}
