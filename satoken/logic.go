package satoken

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Config 是 Logic 的 Sa-Token 兼容配置。
type Config struct {
	// TokenName token 名，默认 satoken。
	TokenName string
	// LoginType 登录类型，默认 login。
	LoginType string
	// Timeout 登录有效期（秒），默认 30 天。
	Timeout int64
	// ActiveTimeout 最低活跃频率（秒），<=0 表示不校验。
	ActiveTimeout int64
	// IsConcurrent 是否允许并发登录；false 时同账号旧会话被顶下线。
	IsConcurrent bool
	// IsShare 并发登录时是否复用同一 token。
	IsShare bool
	// TokenStyle token 生成风格，默认 uuid。
	TokenStyle string
	// Now 注入时钟，测试用；nil 时使用 time.Now。
	Now func() time.Time
}

// Normalize 补齐默认值。
func (c Config) Normalize() Config {
	if c.TokenName == "" {
		c.TokenName = DefaultTokenName
	}
	if c.LoginType == "" {
		c.LoginType = DefaultLoginType
	}
	if c.Timeout == 0 {
		c.Timeout = DefaultTimeoutSeconds
	}
	if c.TokenStyle == "" {
		c.TokenStyle = StyleUUID
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// Logic 是 Sa-Token StpLogic 的 Go 实现（登录态与权限判定）。
type Logic struct {
	cfg    Config
	store  Store
	logger Logger
}

// NewLogic 构造 Logic。
func NewLogic(cfg Config, store Store, logger Logger) *Logic {
	if store == nil {
		panic("satoken: store 不能为空")
	}
	return &Logic{cfg: cfg.Normalize(), store: store, logger: logger}
}

// Config 返回归一化后的配置。
func (l *Logic) Config() Config { return l.cfg }

// TokenName 返回配置的 token 名（实现 authn.SessionProvider）。
func (l *Logic) TokenName() string { return l.cfg.TokenName }

// Store 返回底层存储，供健康检查与服务复用。
func (l *Logic) Store() Store { return l.store }

// ---- key 拼接（与 StpLogic.splicingKey* 逐字对齐）----

// TokenKey 返回 token -> loginId 映射的 Redis key。
func (l *Logic) TokenKey(tokenValue string) string {
	return l.cfg.TokenName + ":" + l.cfg.LoginType + ":token:" + tokenValue
}

// SessionKey 返回 Account-Session 的 Redis key。
func (l *Logic) SessionKey(loginID string) string {
	return l.cfg.TokenName + ":" + l.cfg.LoginType + ":session:" + loginID
}

// TokenSessionKey 返回 Token-Session 的 Redis key。
func (l *Logic) TokenSessionKey(tokenValue string) string {
	return l.cfg.TokenName + ":" + l.cfg.LoginType + ":token-session:" + tokenValue
}

// LastActiveKey 返回最后活跃时间的 Redis key。
func (l *Logic) LastActiveKey(tokenValue string) string {
	return l.cfg.TokenName + ":" + l.cfg.LoginType + ":last-active:" + tokenValue
}

// ---- 登录 ----

// Login 执行登录并返回 token 值，等价于 StpUtil.login(loginId)。
func (l *Logic) Login(ctx context.Context, loginID string) (string, error) {
	if strings.TrimSpace(loginID) == "" {
		return "", fmt.Errorf("satoken: loginId 不能为空")
	}
	if !l.cfg.IsConcurrent {
		if err := l.replaced(ctx, loginID); err != nil {
			return "", err
		}
	}
	if l.cfg.IsShare {
		if existing, err := l.TokenValueByLoginID(ctx, loginID); err == nil && existing != "" {
			// 与 Sa-Token 复用旧 token 的行为一致：续期后直接返回。
			if err := l.renew(ctx, existing, loginID); err != nil {
				return "", err
			}
			return existing, nil
		} else if err != nil && !errors.Is(err, ErrNotFound) {
			return "", err
		}
	}

	tokenValue, err := NewTokenValue(l.cfg.TokenStyle)
	if err != nil {
		return "", err
	}
	session, err := l.getSessionByLoginID(ctx, loginID, true)
	if err != nil {
		return "", err
	}
	session.Type = SessionTypeAccount
	session.LoginType = l.cfg.LoginType
	session.LoginID = loginID
	session.AddTerminal(TerminalInfo{
		TokenValue: tokenValue,
		CreateTime: l.cfg.Now().UnixMilli(),
	})
	if err := l.saveSession(ctx, session, l.cfg.Timeout); err != nil {
		return "", err
	}
	if err := l.store.Set(ctx, l.TokenKey(tokenValue), loginID, l.cfg.Timeout); err != nil {
		return "", err
	}
	if err := l.setLastActiveToNow(ctx, tokenValue); err != nil {
		return "", err
	}
	return tokenValue, nil
}

// renew 续期既有 token 及其会话。
func (l *Logic) renew(ctx context.Context, tokenValue, loginID string) error {
	if err := l.store.Set(ctx, l.TokenKey(tokenValue), loginID, l.cfg.Timeout); err != nil {
		return err
	}
	session, err := l.getSessionByID(ctx, l.SessionKey(loginID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if err := l.saveSession(ctx, session, l.cfg.Timeout); err != nil {
		return err
	}
	return l.setLastActiveToNow(ctx, tokenValue)
}

// Logout 注销指定 token，等价于 StpUtil.logout()（按 token）。
func (l *Logic) Logout(ctx context.Context, tokenValue string) error {
	loginID, err := l.LoginIDByToken(ctx, tokenValue)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if err := l.store.Delete(ctx, l.TokenKey(tokenValue)); err != nil {
		return err
	}
	if err := l.store.Delete(ctx, l.LastActiveKey(tokenValue)); err != nil {
		return err
	}
	if err := l.store.Delete(ctx, l.TokenSessionKey(tokenValue)); err != nil {
		return err
	}
	if loginID == "" {
		return nil
	}
	session, err := l.getSessionByID(ctx, l.SessionKey(loginID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	session.RemoveTerminal(tokenValue)
	if len(session.TerminalList) == 0 {
		return l.store.Delete(ctx, l.SessionKey(loginID))
	}
	return l.saveSession(ctx, session, l.cfg.Timeout)
}

// replaced 把该账号此前的全部 token 标记为被顶下线（删除映射与会话终端）。
func (l *Logic) replaced(ctx context.Context, loginID string) error {
	session, err := l.getSessionByID(ctx, l.SessionKey(loginID))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	for _, terminal := range session.TerminalList {
		if terminal.TokenValue == "" {
			continue
		}
		if err := l.store.Delete(ctx, l.TokenKey(terminal.TokenValue)); err != nil {
			return err
		}
		if err := l.store.Delete(ctx, l.LastActiveKey(terminal.TokenValue)); err != nil {
			return err
		}
	}
	session.TerminalList = nil
	return l.saveSession(ctx, session, l.cfg.Timeout)
}

// ---- token 解析与校验 ----

// LoginIDByToken 通过 token 查登录 id；不存在返回 ErrNotFound。
func (l *Logic) LoginIDByToken(ctx context.Context, tokenValue string) (string, error) {
	if tokenValue == "" {
		return "", ErrNotFound
	}
	value, err := l.store.Get(ctx, l.TokenKey(tokenValue))
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", ErrNotFound
	}
	return value, nil
}

// TokenValueByLoginID 查询账号当前 token（Account-Session 的首个终端），
// 等价于 StpUtil.getTokenValueByLoginId。
func (l *Logic) TokenValueByLoginID(ctx context.Context, loginID string) (string, error) {
	session, err := l.getSessionByID(ctx, l.SessionKey(loginID))
	if err != nil {
		return "", err
	}
	for _, terminal := range session.TerminalList {
		if terminal.TokenValue != "" {
			return terminal.TokenValue, nil
		}
	}
	return "", ErrNotFound
}

// CheckLogin 校验 token 有效性，返回登录 id；无效返回 ErrNotFound。
func (l *Logic) CheckLogin(ctx context.Context, tokenValue string) (string, error) {
	loginID, err := l.LoginIDByToken(ctx, tokenValue)
	if err != nil {
		return "", err
	}
	if l.cfg.ActiveTimeout > 0 {
		last, err := l.store.Get(ctx, l.LastActiveKey(tokenValue))
		if err == nil && strings.TrimSpace(last) != "" {
			seconds, convErr := parseInt64(last)
			if convErr == nil {
				if l.cfg.Now().UnixMilli()-seconds > l.cfg.ActiveTimeout*1000 {
					// 超过最低活跃频率：会话失效（与 Sa-Token 判定一致）。
					if logoutErr := l.Logout(ctx, tokenValue); logoutErr != nil {
						return "", logoutErr
					}
					return "", ErrNotFound
				}
			}
		}
		if err := l.setLastActiveToNow(ctx, tokenValue); err != nil {
			return "", err
		}
	}
	return loginID, nil
}

// GetSession 返回账号会话；不存在时返回 nil。
func (l *Logic) GetSession(ctx context.Context, loginID string) (*Session, error) {
	session, err := l.getSessionByID(ctx, l.SessionKey(loginID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return session, nil
}

// SaveSession 覆盖写入账号会话（业务在登录后写 permissions/roles 等数据）。
func (l *Logic) SaveSession(ctx context.Context, session *Session) error {
	if session == nil {
		return fmt.Errorf("satoken: session 不能为空")
	}
	if session.ID == "" {
		session.ID = NewSessionID()
	}
	if session.LoginType == "" {
		session.LoginType = l.cfg.LoginType
	}
	if session.Type == "" {
		session.Type = SessionTypeAccount
	}
	return l.saveSession(ctx, session, l.cfg.Timeout)
}

// CheckPermission 判定权限码；与 Java 侧 StpInterfaceImpl 一致：
// root（loginId=="0"）由调用方直接判真，本方法不做特权绕过。
func (l *Logic) CheckPermission(session *Session, code string) bool {
	if session == nil || code == "" {
		return false
	}
	for _, granted := range session.StringList(DataKeyPermissions) {
		if granted == code {
			return true
		}
	}
	return false
}

// CheckRole 判定角色标识。
func (l *Logic) CheckRole(session *Session, role string) bool {
	if session == nil || role == "" {
		return false
	}
	for _, granted := range session.StringList(DataKeyRoles) {
		if granted == role {
			return true
		}
	}
	return false
}

// ---- 内部方法 ----

func (l *Logic) setLastActiveToNow(ctx context.Context, tokenValue string) error {
	if l.cfg.ActiveTimeout <= 0 {
		return nil
	}
	return l.store.Set(ctx, l.LastActiveKey(tokenValue), fmt.Sprintf("%d", l.cfg.Now().UnixMilli()), l.cfg.Timeout)
}

func (l *Logic) getSessionByLoginID(ctx context.Context, loginID string, create bool) (*Session, error) {
	key := l.SessionKey(loginID)
	session, err := l.getSessionByID(ctx, key)
	if err == nil {
		return session, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if !create {
		return nil, ErrNotFound
	}
	return NewSession(NewSessionID()), nil
}

func (l *Logic) getSessionByID(ctx context.Context, key string) (*Session, error) {
	raw, err := l.store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	session, err := unmarshalSession([]byte(raw))
	if err != nil {
		return nil, err
	}
	if session.DataMap == nil {
		session.DataMap = map[string]any{}
	}
	return session, nil
}

func (l *Logic) saveSession(ctx context.Context, session *Session, timeoutSeconds int64) error {
	payload, err := marshalSession(session)
	if err != nil {
		return err
	}
	return l.store.Set(ctx, l.SessionKey(loginIDOf(session)), string(payload), timeoutSeconds)
}

func loginIDOf(session *Session) string {
	if session == nil {
		return ""
	}
	switch v := session.LoginID.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func parseInt64(raw string) (int64, error) {
	var out int64
	_, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &out)
	if err != nil {
		return 0, fmt.Errorf("satoken: 解析整数失败 %q: %w", raw, err)
	}
	return out, nil
}
