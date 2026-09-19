// Package config 提供环境变量 + YAML 的配置加载能力（对应
// bootstrap.yml + Nacos 配置中心组合（迁移期 Nacos 保持兼容，Go 侧先支持本地/
// 环境变量配置（Nacos 未接入，用环境变量 + 静态上游）。
//
// 占位符规则：字符串中的 ${ENV_NAME:default} 会被替换为环境变量值，未设置时使用 default。
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ServerConfig 描述 HTTP 服务监听与超时。
type ServerConfig struct {
	// Name 服务名（对应 Nacos spring.application.name 与容器名），如 acat-admin-user。
	Name string `yaml:"name"`
	// Port 监听端口，如 9601。
	Port int `yaml:"port"`
	// ReadTimeout 读超时，默认 15s。
	ReadTimeout time.Duration `yaml:"readTimeout"`
	// WriteTimeout 写超时，默认 30s。
	WriteTimeout time.Duration `yaml:"writeTimeout"`
	// IdleTimeout 空闲超时，默认 60s。
	IdleTimeout time.Duration `yaml:"idleTimeout"`
	// ShutdownTimeout 优雅停机等待上限，默认 15s。
	ShutdownTimeout time.Duration `yaml:"shutdownTimeout"`
}

// DatabaseConfig 描述 MySQL 连接。
type DatabaseConfig struct {
	// Host 主机，默认 127.0.0.1。
	Host string `yaml:"host"`
	// Port 端口，默认 3306。
	Port int `yaml:"port"`
	// Database 库名，如 acat_user。
	Database string `yaml:"database"`
	// Username 账号。
	Username string `yaml:"username"`
	// Password 密码（只允许来自环境变量或未提交配置）。
	Password string `yaml:"password"`
	// Params 额外 DSN 参数。
	Params string `yaml:"params"`
	// MaxOpenConns 最大连接数，默认 20。
	MaxOpenConns int `yaml:"maxOpenConns"`
	// MaxIdleConns 最大空闲连接数，默认 10。
	MaxIdleConns int `yaml:"maxIdleConns"`
	// ConnMaxLifetime 连接最大存活时间，默认 30m。
	ConnMaxLifetime time.Duration `yaml:"connMaxLifetime"`
	// ConnMaxIdleTime 空闲连接回收时间，默认 5m。
	ConnMaxIdleTime time.Duration `yaml:"connMaxIdleTime"`
}

// DSN 拼装 go-sql-driver/mysql 连接串。
func (c DatabaseConfig) DSN() string {
	params := c.Params
	if params == "" {
		params = "charset=utf8mb4&parseTime=true&loc=Local&collation=utf8mb4_unicode_ci"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?%s", c.Username, c.Password, c.Host, c.Port, c.Database, params)
}

// RedisConfig 描述 Redis 连接。
type RedisConfig struct {
	// Addr 地址 host:port，默认 127.0.0.1:6379。
	Addr string `yaml:"addr"`
	// Password 密码，无密码留空。
	Password string `yaml:"password"`
	// DB 库号。
	DB int `yaml:"db"`
	// PoolSize 连接池大小，默认 20。
	PoolSize int `yaml:"poolSize"`
	// DialTimeout 建连超时，默认 5s。
	DialTimeout time.Duration `yaml:"dialTimeout"`
}

// SaTokenConfig 描述 Sa-Token 兼容参数。
type SaTokenConfig struct {
	// TokenName token 名，默认 satoken；ACAT 管理端实际为 acat-admin-token。
	TokenName string `yaml:"tokenName"`
	// LoginType 登录类型，默认 login。
	LoginType string `yaml:"loginType"`
	// Timeout 登录有效期（秒），默认 2592000（30 天），与 Sa-Token 默认一致。
	Timeout int64 `yaml:"timeout"`
	// ActiveTimeout 最低活跃频率（秒），-1 表示不校验。
	ActiveTimeout int64 `yaml:"activeTimeout"`
	// IsConcurrent 是否允许同一账号并发登录，默认 true。
	IsConcurrent bool `yaml:"isConcurrent"`
	// IsShare 多人登录时是否共用同一 token，默认 true。
	IsShare bool `yaml:"isShare"`
	// TokenStyle token 生成风格：uuid/simple-uuid/random-32/random-64/random-128/tik。
	TokenStyle string `yaml:"tokenStyle"`
	// CookieName 写入 Cookie 的名称，默认与 TokenName 一致。
	CookieName string `yaml:"cookieName"`
	// CookiePath Cookie 路径，默认 /。
	CookiePath string `yaml:"cookiePath"`
	// CookieDomain Cookie 域，默认空（当前域）。
	CookieDomain string `yaml:"cookieDomain"`
	// CookieSecure 是否仅 HTTPS 下发 Cookie。
	CookieSecure bool `yaml:"cookieSecure"`
	// CookieSameSite SameSite 取值：Lax/Strict/None，默认 Lax。
	CookieSameSite string `yaml:"cookieSameSite"`
	// CookieMaxAge Cookie 存活秒数；<=0 表示会话 Cookie。
	CookieMaxAge int `yaml:"cookieMaxAge"`
}

// DefaultSaTokenTimeoutSeconds 是 Sa-Token 默认登录有效期（30 天），
// 与 lib/backend/acat-go-common/satoken.DefaultTimeoutSeconds 保持一致。
const DefaultSaTokenTimeoutSeconds int64 = 2592000

// Config 是服务完整配置。
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	SaToken  SaTokenConfig  `yaml:"saToken"`
	// LogLevel 日志级别：debug/info/warn/error，默认 info。
	LogLevel string `yaml:"logLevel"`
}

// Default 返回带默认值的配置。
func Default() Config {
	return Config{
		Server: ServerConfig{
			Name:            "acat-go-service",
			Port:            8080,
			ReadTimeout:     15 * time.Second,
			WriteTimeout:    30 * time.Second,
			IdleTimeout:     60 * time.Second,
			ShutdownTimeout: 15 * time.Second,
		},
		Database: DatabaseConfig{
			Host:            "127.0.0.1",
			Port:            3306,
			Database:        "acat_user",
			MaxOpenConns:    20,
			MaxIdleConns:    10,
			ConnMaxLifetime: 30 * time.Minute,
			ConnMaxIdleTime: 5 * time.Minute,
		},
		Redis: RedisConfig{
			Addr:        "127.0.0.1:6379",
			PoolSize:    20,
			DialTimeout: 5 * time.Second,
		},
		SaToken: SaTokenConfig{
			TokenName:      "satoken",
			LoginType:      "login",
			Timeout:        DefaultSaTokenTimeoutSeconds,
			ActiveTimeout:  -1,
			IsConcurrent:   true,
			IsShare:        true,
			TokenStyle:     "uuid",
			CookiePath:     "/",
			CookieSameSite: "Lax",
		},
		LogLevel: "info",
	}
}

// Load 读取配置文件并应用环境变量覆盖。path 为空或文件不存在时只用默认值 + 环境变量。
//
// 环境变量一律带 ACAT_ 前缀，例如 ACAT_SERVER_PORT、ACAT_DATABASE_HOST、
// ACAT_DATABASE_PASSWORD、ACAT_REDIS_ADDR、ACAT_SA_TOKEN_NAME、ACAT_LOG_LEVEL。
func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				return cfg, fmt.Errorf("读取配置文件失败 %s: %w", path, err)
			}
		} else {
			expanded, err := ExpandEnv(string(raw))
			if err != nil {
				return cfg, err
			}
			if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
				return cfg, fmt.Errorf("解析配置文件失败 %s: %w", path, err)
			}
		}
	}

	if err := applyEnv(&cfg); err != nil {
		return cfg, err
	}
	cfg.Normalize()
	return cfg, nil
}

func applyEnv(cfg *Config) error {
	setStr(&cfg.Server.Name, "ACAT_SERVER_NAME")
	setInt(&cfg.Server.Port, "ACAT_SERVER_PORT")
	setDuration(&cfg.Server.ReadTimeout, "ACAT_SERVER_READ_TIMEOUT")
	setDuration(&cfg.Server.WriteTimeout, "ACAT_SERVER_WRITE_TIMEOUT")
	setDuration(&cfg.Server.IdleTimeout, "ACAT_SERVER_IDLE_TIMEOUT")
	setDuration(&cfg.Server.ShutdownTimeout, "ACAT_SERVER_SHUTDOWN_TIMEOUT")

	setStr(&cfg.Database.Host, "ACAT_DATABASE_HOST")
	setInt(&cfg.Database.Port, "ACAT_DATABASE_PORT")
	setStr(&cfg.Database.Database, "ACAT_DATABASE_NAME")
	setStr(&cfg.Database.Username, "ACAT_DATABASE_USERNAME")
	setStr(&cfg.Database.Password, "ACAT_DATABASE_PASSWORD")
	setStr(&cfg.Database.Params, "ACAT_DATABASE_PARAMS")
	setInt(&cfg.Database.MaxOpenConns, "ACAT_DATABASE_MAX_OPEN_CONNS")
	setInt(&cfg.Database.MaxIdleConns, "ACAT_DATABASE_MAX_IDLE_CONNS")

	setStr(&cfg.Redis.Addr, "ACAT_REDIS_ADDR")
	setStr(&cfg.Redis.Password, "ACAT_REDIS_PASSWORD")
	setInt(&cfg.Redis.DB, "ACAT_REDIS_DB")
	setInt(&cfg.Redis.PoolSize, "ACAT_REDIS_POOL_SIZE")

	setStr(&cfg.SaToken.TokenName, "ACAT_SA_TOKEN_NAME")
	setStr(&cfg.SaToken.LoginType, "ACAT_SA_TOKEN_LOGIN_TYPE")
	setInt64(&cfg.SaToken.Timeout, "ACAT_SA_TOKEN_TIMEOUT")
	setInt64(&cfg.SaToken.ActiveTimeout, "ACAT_SA_TOKEN_ACTIVE_TIMEOUT")
	setBool(&cfg.SaToken.IsConcurrent, "ACAT_SA_TOKEN_IS_CONCURRENT")
	setBool(&cfg.SaToken.IsShare, "ACAT_SA_TOKEN_IS_SHARE")
	setStr(&cfg.SaToken.TokenStyle, "ACAT_SA_TOKEN_TOKEN_STYLE")
	setStr(&cfg.SaToken.CookieName, "ACAT_SA_TOKEN_COOKIE_NAME")
	setStr(&cfg.SaToken.CookiePath, "ACAT_SA_TOKEN_COOKIE_PATH")
	setStr(&cfg.SaToken.CookieDomain, "ACAT_SA_TOKEN_COOKIE_DOMAIN")
	setBool(&cfg.SaToken.CookieSecure, "ACAT_SA_TOKEN_COOKIE_SECURE")
	setStr(&cfg.SaToken.CookieSameSite, "ACAT_SA_TOKEN_COOKIE_SAME_SITE")
	setInt(&cfg.SaToken.CookieMaxAge, "ACAT_SA_TOKEN_COOKIE_MAX_AGE")

	setStr(&cfg.LogLevel, "ACAT_LOG_LEVEL")
	return nil
}

// Normalize 补齐非法/零值字段，保证下游拿到可用配置。
func (c *Config) Normalize() {
	def := Default()
	if c.Server.Name == "" {
		c.Server.Name = def.Server.Name
	}
	if c.Server.Port <= 0 {
		c.Server.Port = def.Server.Port
	}
	if c.Server.ReadTimeout <= 0 {
		c.Server.ReadTimeout = def.Server.ReadTimeout
	}
	if c.Server.WriteTimeout <= 0 {
		c.Server.WriteTimeout = def.Server.WriteTimeout
	}
	if c.Server.IdleTimeout <= 0 {
		c.Server.IdleTimeout = def.Server.IdleTimeout
	}
	if c.Server.ShutdownTimeout <= 0 {
		c.Server.ShutdownTimeout = def.Server.ShutdownTimeout
	}
	if c.Database.MaxOpenConns <= 0 {
		c.Database.MaxOpenConns = def.Database.MaxOpenConns
	}
	if c.Database.MaxIdleConns <= 0 {
		c.Database.MaxIdleConns = def.Database.MaxIdleConns
	}
	if c.Redis.PoolSize <= 0 {
		c.Redis.PoolSize = def.Redis.PoolSize
	}
	if c.Redis.DialTimeout <= 0 {
		c.Redis.DialTimeout = def.Redis.DialTimeout
	}
	if c.SaToken.TokenName == "" {
		c.SaToken.TokenName = def.SaToken.TokenName
	}
	if c.SaToken.LoginType == "" {
		c.SaToken.LoginType = def.SaToken.LoginType
	}
	if c.SaToken.TokenStyle == "" {
		c.SaToken.TokenStyle = def.SaToken.TokenStyle
	}
	if c.SaToken.CookieName == "" {
		c.SaToken.CookieName = c.SaToken.TokenName
	}
	if c.SaToken.CookiePath == "" {
		c.SaToken.CookiePath = def.SaToken.CookiePath
	}
	if c.SaToken.CookieSameSite == "" {
		c.SaToken.CookieSameSite = def.SaToken.CookieSameSite
	}
	if c.LogLevel == "" {
		c.LogLevel = def.LogLevel
	}
}

func setStr(dst *string, env string) {
	if v, ok := os.LookupEnv(env); ok && v != "" {
		*dst = v
	}
}

func setInt(dst *int, env string) {
	if v, ok := os.LookupEnv(env); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			*dst = n
		}
	}
}

func setInt64(dst *int64, env string) {
	if v, ok := os.LookupEnv(env); ok && v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			*dst = n
		}
	}
}

func setBool(dst *bool, env string) {
	if v, ok := os.LookupEnv(env); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			*dst = b
		}
	}
}

func setDuration(dst *time.Duration, env string) {
	if v, ok := os.LookupEnv(env); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			*dst = d
		}
	}
}

// ExpandEnv 展开 ${VAR:default} 占位符；缺少默认值且变量未设置时返回错误。
func ExpandEnv(input string) (string, error) {
	var out strings.Builder
	rest := input
	for {
		start := strings.Index(rest, "${")
		if start < 0 {
			out.WriteString(rest)
			break
		}
		out.WriteString(rest[:start])
		end := strings.Index(rest[start:], "}")
		if end < 0 {
			return "", fmt.Errorf("配置占位符未闭合: %s", rest[start:])
		}
		expr := rest[start+2 : start+end]
		name, fallback, hasFallback := strings.Cut(expr, ":")
		if v, ok := os.LookupEnv(name); ok {
			out.WriteString(v)
		} else if hasFallback {
			out.WriteString(fallback)
		} else {
			return "", fmt.Errorf("配置占位符 %s 未提供环境变量且无默认值", name)
		}
		rest = rest[start+end+1:]
	}
	return out.String(), nil
}
