// Package jwtauth 提供基于 JWT（HS256）的无状态登录态实现。
//
// 适用场景：服务无 Redis、不需要与 Java Sa-Token 共享会话时（如 devops 平台日后选用）。
// 与 satoken 模式不互通：JWT 不写 Redis token 映射；Logout 默认 no-op（无黑名单）。
// GetSession 返回仅含 LoginID 的最小会话；权限由各服务自行查库，不依赖会话快照。
package jwtauth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/acat-fun/acat-go-common/satoken"
)

// 默认值。
const (
	DefaultIssuer     = "acat"
	DefaultTTLSeconds = int64(86400) // 24h
	DefaultTokenName  = "satoken"
)

// Config JWT 模式配置。
type Config struct {
	// Secret HS256 签名密钥；不得为空。
	Secret string
	// Issuer 写入 iss；空则 DefaultIssuer。
	Issuer string
	// TTLSeconds 有效期（秒）；<=0 则 DefaultTTLSeconds。
	TTLSeconds int64
	// TokenName Cookie/逻辑名；空则 satoken（与请求头名中间件仍读 "satoken"）。
	TokenName string
	// Now 可注入时钟（测试用）。
	Now func() time.Time
}

// Normalize 补齐默认值。
func (c Config) Normalize() Config {
	if c.Issuer == "" {
		c.Issuer = DefaultIssuer
	}
	if c.TTLSeconds <= 0 {
		c.TTLSeconds = DefaultTTLSeconds
	}
	if c.TokenName == "" {
		c.TokenName = DefaultTokenName
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// Logic JWT 登录态管理器（实现 authn.Manager）。
type Logic struct {
	cfg Config
}

// NewLogic 构造 JWT Logic；secret 为空时 panic。
func NewLogic(cfg Config) *Logic {
	cfg = cfg.Normalize()
	if strings.TrimSpace(cfg.Secret) == "" {
		panic("jwtauth: secret 不能为空")
	}
	return &Logic{cfg: cfg}
}

// Config 返回归一化配置。
func (l *Logic) Config() Config { return l.cfg }

// TokenName 实现 authn.SessionProvider。
func (l *Logic) TokenName() string { return l.cfg.TokenName }

// claims 内部 JWT 声明。
type claims struct {
	jwt.RegisteredClaims
}

// Login 签发 JWT，sub=loginID。
func (l *Logic) Login(ctx context.Context, loginID string) (string, error) {
	_ = ctx
	loginID = strings.TrimSpace(loginID)
	if loginID == "" {
		return "", fmt.Errorf("jwtauth: loginId 不能为空")
	}
	now := l.cfg.Now()
	exp := now.Add(time.Duration(l.cfg.TTLSeconds) * time.Second)
	c := claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   loginID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			Issuer:    l.cfg.Issuer,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	signed, err := token.SignedString([]byte(l.cfg.Secret))
	if err != nil {
		return "", fmt.Errorf("jwtauth: 签发失败: %w", err)
	}
	return signed, nil
}

// Logout JWT 默认无状态：不撤销已签发 token（二期可加 jti 黑名单）。
func (l *Logic) Logout(ctx context.Context, token string) error {
	_ = ctx
	_ = token
	return nil
}

// CheckLogin 验签并检查过期，返回 sub 作为 loginID。
func (l *Logic) CheckLogin(ctx context.Context, tokenValue string) (string, error) {
	_ = ctx
	tokenValue = strings.TrimSpace(tokenValue)
	if tokenValue == "" {
		return "", satoken.ErrNotFound
	}
	parsed, err := jwt.ParseWithClaims(tokenValue, &claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("非法签名算法")
		}
		return []byte(l.cfg.Secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithIssuer(l.cfg.Issuer))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) || errors.Is(err, jwt.ErrTokenNotValidYet) {
			return "", satoken.ErrNotFound
		}
		return "", satoken.ErrNotFound
	}
	c, ok := parsed.Claims.(*claims)
	if !ok || !parsed.Valid || strings.TrimSpace(c.Subject) == "" {
		return "", satoken.ErrNotFound
	}
	return c.Subject, nil
}

// GetSession 返回仅含 LoginID 的最小会话（无 Redis 权限快照）。
func (l *Logic) GetSession(ctx context.Context, loginID string) (*satoken.Session, error) {
	_ = ctx
	loginID = strings.TrimSpace(loginID)
	if loginID == "" {
		return nil, nil
	}
	session := satoken.NewSession(satoken.NewSessionID())
	session.Type = satoken.SessionTypeAccount
	session.LoginType = "login"
	session.LoginID = loginID
	session.CreateTime = l.cfg.Now().UnixMilli()
	return session, nil
}

// SaveSession JWT 无 Redis 时为 no-op（权限不落会话）。
func (l *Logic) SaveSession(ctx context.Context, session *satoken.Session) error {
	_ = ctx
	_ = session
	return nil
}
