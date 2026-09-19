// Package middleware 提供服务端通用 HTTP 中间件：
// Trace（trace_id）、Recover（panic 兜底）、CORS、认证（Sa-Token 兼容）。
package middleware

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"gitea.acat.fun/acat-fun/acat-go-common/apperr"
	"gitea.acat.fun/acat-fun/acat-go-common/logging"
	"gitea.acat.fun/acat-fun/acat-go-common/result"
	"gitea.acat.fun/acat-fun/acat-go-common/satoken"
)

// TokenHeader 是前端携带 token 的请求头名（管理端 Cookie 模式下仍然保留该头兼容路径）。
const TokenHeader = "satoken"

// MessageNotLoggedIn 是未登录/会话失效的统一 401 文案，
// 与
const MessageNotLoggedIn = "未登录或登录已过期，请重新登录"

// contextKey 是中间件写入请求上下文的键类型。
type contextKey string

const (
	ctxKeySession contextKey = "satoken.session"
	ctxKeyToken   contextKey = "satoken.token"
)

// SessionFrom 读取认证中间件写入的会话；未认证返回 nil。
func SessionFrom(ctx context.Context) *satoken.Session {
	if ctx == nil {
		return nil
	}
	if v, ok := ctx.Value(ctxKeySession).(*satoken.Session); ok {
		return v
	}
	return nil
}

// TokenFrom 读取认证中间件解析出的 token 值。
func TokenFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxKeyToken).(string); ok {
		return v
	}
	return ""
}

// WithSession 把会话写入上下文（服务内部转发或测试使用）。
func WithSession(ctx context.Context, session *satoken.Session, token string) context.Context {
	if session != nil {
		ctx = context.WithValue(ctx, ctxKeySession, session)
	}
	if token != "" {
		ctx = context.WithValue(ctx, ctxKeyToken, token)
	}
	return ctx
}

// AuthConfig 配置认证中间件。
type AuthConfig struct {
	// Logic 是 Sa-Token 兼容逻辑。
	Logic *satoken.Logic
	// CookieName 允许从 Cookie 读取 token；空则与 Logic 的 token 名一致。
	CookieName string
	// CookieOnly 为 true 时不接受请求头 token（管理端 HttpOnly Cookie 模式）。
	CookieOnly bool
}

// Auth 构造认证中间件：校验 token，加载账号会话并写入上下文。
func Auth(cfg AuthConfig) func(http.Handler) http.Handler {
	cookieName := cfg.CookieName
	if cookieName == "" && cfg.Logic != nil {
		cookieName = cfg.Logic.Config().TokenName
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if cfg.Logic == nil {
				WriteError(req.Context(), w, apperr.Internal(nil, "认证组件未初始化"))
				return
			}
			token := extractToken(req, cookieName, cfg.CookieOnly)
			if token == "" {
				WriteError(req.Context(), w, apperr.Unauthorized(MessageNotLoggedIn))
				return
			}
			loginID, err := cfg.Logic.CheckLogin(req.Context(), token)
			if err != nil {
				// 存储故障与"未登录"必须区分：前者是 503，后者是 401。
				if err == satoken.ErrNotFound {
					WriteError(req.Context(), w, apperr.Unauthorized(MessageNotLoggedIn))
					return
				}
				WriteError(req.Context(), w, apperr.Unavailable("会话存储不可用: %v", err))
				return
			}
			session, err := cfg.Logic.GetSession(req.Context(), loginID)
			if err != nil {
				WriteError(req.Context(), w, apperr.Unavailable("读取会话失败: %v", err))
				return
			}
			if session == nil {
				WriteError(req.Context(), w, apperr.Unauthorized(MessageNotLoggedIn))
				return
			}
			next.ServeHTTP(w, req.WithContext(WithSession(req.Context(), session, token)))
		})
	}
}

func extractToken(req *http.Request, cookieName string, cookieOnly bool) string {
	if cookieName != "" {
		if cookie, err := req.Cookie(cookieName); err == nil && cookie.Value != "" {
			return cookie.Value
		}
	}
	if cookieOnly {
		return ""
	}
	if v := strings.TrimSpace(req.Header.Get(TokenHeader)); v != "" {
		return v
	}
	if v := strings.TrimSpace(req.Header.Get("Authorization")); v != "" {
		return strings.TrimPrefix(v, "Bearer ")
	}
	return ""
}

// WriteResult 输出统一 Result 响应（HTTP 200 + body.code）。
// 参数为 any：Go 泛型不支持协变，调用方传入任意 Result[T] 实例。
func WriteResult(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(http.StatusOK)
	writeJSONBody(w, payload)
}

// WriteError 输出语义错误：HTTP 状态码来自 apperr，响应体仍是 Result 结构。
func WriteError(ctx context.Context, w http.ResponseWriter, err error) {
	status := apperr.HTTPStatusOf(err)
	code := apperr.CodeOf(err)
	message := err.Error()
	if e, ok := apperr.As(err); ok {
		message = e.Message
	}
	if status >= http.StatusInternalServerError {
		logging.FromContext(ctx, nil).Error("请求处理失败", "status", status, "message", message, "error", err)
	}
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	writeJSONBody(w, result.FailCode(code, message))
}

// Trace 注入/透传 trace_id，并写入响应头。
func Trace() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			traceID := strings.TrimSpace(req.Header.Get(logging.HeaderTraceID))
			if traceID == "" {
				traceID = logging.NewTraceID()
			}
			if traceID != "" {
				w.Header().Set(logging.HeaderTraceID, traceID)
			}
			next.ServeHTTP(w, req.WithContext(logging.WithTraceID(req.Context(), traceID)))
		})
	}
}

// Recover 兜底 panic，避免单个请求打挂进程。
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logging.FromContext(req.Context(), logger).Error("panic 已恢复", "panic", rec)
					WriteError(req.Context(), w, apperr.Internal(nil, "服务内部错误"))
				}
			}()
			next.ServeHTTP(w, req)
		})
	}
}

// CORS 允许跨域访问；管理端同源部署下主要用于本地联调。
func CORS(allowedOrigins []string, allowCredentials bool) func(http.Handler) http.Handler {
	allowAll := false
	allowSet := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		if origin == "*" {
			allowAll = true
			continue
		}
		allowSet[origin] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			origin := req.Header.Get("Origin")
			switch {
			case origin == "":
			case allowAll && !allowCredentials:
				w.Header().Set("Access-Control-Allow-Origin", "*")
			default:
				if _, ok := allowSet[origin]; ok {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Add("Vary", "Origin")
					if allowCredentials {
						w.Header().Set("Access-Control-Allow-Credentials", "true")
					}
				}
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type,Authorization,"+TokenHeader+","+logging.HeaderTraceID)
			w.Header().Set("Access-Control-Max-Age", "600")
			if req.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, req)
		})
	}
}

// Chain 按声明顺序组合中间件（第一个最外层）。
func Chain(handler http.Handler, middlewares ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		handler = middlewares[i](handler)
	}
	return handler
}

func writeJSONBody(w http.ResponseWriter, payload any) {
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		// 响应已开始写出，只能记录日志；不吞异常也不改写状态码。
		logging.FromContext(context.Background(), nil).Error("写出响应失败", "error", err)
	}
}
