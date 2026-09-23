// Package authn 定义登录态抽象，供 satoken（Redis 会话）与 jwtauth（JWT）共用。
//
// middleware.Auth 依赖 SessionProvider；登录/登出处理器可选依赖 Manager。
package authn

import (
	"context"

	"github.com/acat-fun/acat-go-common/satoken"
)

// SessionProvider 校验 token 并加载账号会话（认证中间件所需最小面）。
type SessionProvider interface {
	// CheckLogin 校验 token，成功返回 loginID；无效/过期返回 satoken.ErrNotFound。
	CheckLogin(ctx context.Context, token string) (loginID string, err error)
	// GetSession 按 loginID 加载会话；JWT 无状态模式可返回仅含 LoginID 的最小会话。
	GetSession(ctx context.Context, loginID string) (*satoken.Session, error)
	// TokenName 返回 token/Cookie 名（用于 Cookie 读取默认名）。
	TokenName() string
}

// Manager 在 SessionProvider 之上提供签发与注销。
type Manager interface {
	SessionProvider
	// Login 签发登录凭证并返回 token 字符串。
	Login(ctx context.Context, loginID string) (token string, err error)
	// Logout 注销当前 token。
	// satoken 模式删除 Redis 映射；jwt 模式默认无状态（no-op），除非启用黑名单。
	Logout(ctx context.Context, token string) error
	// SaveSession 持久化会话快照（权限等）；jwt 无 Redis 时可为 no-op。
	SaveSession(ctx context.Context, session *satoken.Session) error
}
