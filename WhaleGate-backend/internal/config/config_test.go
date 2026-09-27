package config

import (
	"path/filepath"
	"testing"
	"time"
)

func examplePath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "configs", "config.example.yaml")
}

func TestLoadExample(t *testing.T) {
	cfg, err := Load(examplePath(t))
	if err != nil {
		t.Fatalf("加载示例配置失败: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("端口解析错误: %d", cfg.Server.Port)
	}
	if cfg.Server.WriteTimeout != 600*time.Second {
		t.Errorf("写超时解析错误: %v", cfg.Server.WriteTimeout)
	}
	if cfg.Database.DBName == "" {
		t.Error("数据库名不应为空")
	}
	if cfg.Addr() != "0.0.0.0:8080" {
		t.Errorf("监听地址错误: %s", cfg.Addr())
	}
}

func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("WG_SERVER_PORT", "9090")
	t.Setenv("WG_DATABASE_HOST", "pg-primary")
	t.Setenv("WG_SECURITY_ENCRYPTION_KEY", "abcdefghijklmnopqrstuvwxyz012345")

	cfg, err := Load(examplePath(t))
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("环境变量未覆盖端口: %d", cfg.Server.Port)
	}
	if cfg.Database.Host != "pg-primary" {
		t.Errorf("环境变量未覆盖数据库主机: %s", cfg.Database.Host)
	}
}

func TestDSN(t *testing.T) {
	c := DatabaseConfig{Host: "h", Port: 5432, User: "u", Password: "p", DBName: "d", SSLMode: "disable", TimeZone: "UTC"}
	dsn := c.DSN()
	for _, want := range []string{"host=h", "port=5432", "user=u", "dbname=d"} {
		if !contains(dsn, want) {
			t.Errorf("DSN 缺少 %s: %s", want, dsn)
		}
	}
	m := c.MigrationDSN()
	if !contains(m, "postgres://u:p@h:5432/d") {
		t.Errorf("迁移 DSN 格式错误: %s", m)
	}
}

func TestValidate(t *testing.T) {
	c := &Config{}
	c.Server.Port = 8080
	c.Server.Mode = "release"
	c.Database.User = "u"
	c.Database.DBName = "d"
	c.Security.EncryptionKey = "0123456789abcdef0123456789abcdef"
	if err := c.Validate(); err != nil {
		t.Fatalf("合法配置不应报错: %v", err)
	}

	c.Server.Mode = "unknown"
	if err := c.Validate(); err == nil {
		t.Fatal("非法 mode 应报错")
	}
	c.Server.Mode = "release"

	c.Security.EncryptionKey = "short"
	if err := c.Validate(); err == nil {
		t.Fatal("非法加密密钥长度应报错")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
