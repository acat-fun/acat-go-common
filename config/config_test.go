package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandEnvWithDefault(t *testing.T) {
	t.Setenv("ACAT_TEST_HOST", "10.0.0.1")
	got, err := ExpandEnv("host: ${ACAT_TEST_HOST:127.0.0.1}\nport: ${ACAT_TEST_MISSING:3306}")
	if err != nil {
		t.Fatalf("ExpandEnv 失败: %v", err)
	}
	if !strings.Contains(got, "host: 10.0.0.1") || !strings.Contains(got, "port: 3306") {
		t.Errorf("展开结果 = %q", got)
	}
}

func TestExpandEnvMissingWithoutDefault(t *testing.T) {
	if _, err := ExpandEnv("password: ${ACAT_TEST_NO_SUCH_ENV}"); err == nil {
		t.Errorf("缺少默认值时应报错")
	}
}

func TestLoadYamlAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
server:
  name: acat-admin-user
  port: 9601
database:
  host: ${ACAT_TEST_DB_HOST:127.0.0.1}
  port: 3306
  name: acat_user
  username: root
  password: "${ACAT_TEST_DB_PASSWORD:123456}"
redis:
  addr: redis:6379
saToken:
  tokenName: acat-admin-token
  timeout: 7200
logLevel: debug
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	t.Setenv("ACAT_TEST_DB_HOST", "mysql")
	t.Setenv("ACAT_SERVER_PORT", "19601")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.Server.Name != "acat-admin-user" {
		t.Errorf("server.name = %s", cfg.Server.Name)
	}
	if cfg.Server.Port != 19601 {
		t.Errorf("环境变量未覆盖端口: %d", cfg.Server.Port)
	}
	if cfg.Database.Host != "mysql" {
		t.Errorf("占位符未展开: %s", cfg.Database.Host)
	}
	if cfg.Database.Port != 3306 || cfg.Database.Username != "root" {
		t.Errorf("database 配置异常: %+v", cfg.Database)
	}
	if cfg.Redis.Addr != "redis:6379" {
		t.Errorf("redis.addr = %s", cfg.Redis.Addr)
	}
	if cfg.SaToken.TokenName != "acat-admin-token" || cfg.SaToken.Timeout != 7200 {
		t.Errorf("saToken 配置异常: %+v", cfg.SaToken)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("logLevel = %s", cfg.LogLevel)
	}
	// CookieName 未显式配置时跟随 tokenName。
	if cfg.SaToken.CookieName != "acat-admin-token" {
		t.Errorf("cookieName 默认值 = %s", cfg.SaToken.CookieName)
	}
}

func TestLoadMissingFileFallsBackToDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "not-exists.yaml"))
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.Server.Port != Default().Server.Port {
		t.Errorf("默认端口 = %d", cfg.Server.Port)
	}
	if cfg.SaToken.TokenName != "satoken" || cfg.SaToken.LoginType != "login" {
		t.Errorf("默认 Sa-Token 配置异常: %+v", cfg.SaToken)
	}
	if cfg.SaToken.Timeout != DefaultSaTokenTimeoutSeconds {
		t.Errorf("默认 timeout = %d", cfg.SaToken.Timeout)
	}
}

func TestNormalizeFillsZeroValues(t *testing.T) {
	cfg := Config{}
	cfg.Normalize()
	if cfg.Server.ReadTimeout == 0 || cfg.Server.ShutdownTimeout == 0 {
		t.Errorf("超时默认值未补齐: %+v", cfg.Server)
	}
	if cfg.Database.MaxOpenConns == 0 || cfg.Redis.PoolSize == 0 {
		t.Errorf("连接池默认值未补齐: %+v", cfg.Database)
	}
	if cfg.SaToken.CookiePath != "/" || cfg.SaToken.CookieSameSite != "Lax" {
		t.Errorf("Cookie 默认值未补齐: %+v", cfg.SaToken)
	}
}

func TestDSN(t *testing.T) {
	cfg := DatabaseConfig{
		Host:     "mysql",
		Port:     3306,
		Database: "acat_user",
		Username: "root",
		Password: "123456",
	}
	dsn := cfg.DSN()
	want := "root:123456@tcp(mysql:3306)/acat_user?charset=utf8mb4&parseTime=true&loc=Local&collation=utf8mb4_unicode_ci"
	if dsn != want {
		t.Errorf("DSN = %s, 期望 %s", dsn, want)
	}
}
