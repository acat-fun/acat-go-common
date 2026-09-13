package satoken

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestLogic(t *testing.T, cfg Config) (*Logic, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	cfg.Now = func() time.Time { return time.Unix(1730000000, 0) }
	return NewLogic(cfg, store, nil), store
}

func TestKeySplicing(t *testing.T) {
	logic, _ := newTestLogic(t, Config{TokenName: "acat-admin-token", LoginType: "login"})
	if got := logic.TokenKey("abc"); got != "acat-admin-token:login:token:abc" {
		t.Errorf("TokenKey = %s", got)
	}
	if got := logic.SessionKey("0"); got != "acat-admin-token:login:session:0" {
		t.Errorf("SessionKey = %s", got)
	}
	if got := logic.TokenSessionKey("abc"); got != "acat-admin-token:login:token-session:abc" {
		t.Errorf("TokenSessionKey = %s", got)
	}
	if got := logic.LastActiveKey("abc"); got != "acat-admin-token:login:last-active:abc" {
		t.Errorf("LastActiveKey = %s", got)
	}
}

func TestLoginWritesTokenAndSession(t *testing.T) {
	logic, store := newTestLogic(t, Config{Timeout: 3600, TokenStyle: StyleSimpleUUID})
	ctx := context.Background()

	token, err := logic.Login(ctx, "0")
	if err != nil {
		t.Fatalf("Login 失败: %v", err)
	}
	if len(token) != simpleUUIDLen {
		t.Errorf("simple-uuid token 长度 = %d, 期望 %d", len(token), simpleUUIDLen)
	}

	// token -> loginId 映射的 value 必须是字符串形式的 loginId（与 Java 一致）。
	raw, err := store.Get(ctx, logic.TokenKey(token))
	if err != nil {
		t.Fatalf("token 映射未写入: %v", err)
	}
	if raw != "0" {
		t.Errorf("token 映射 value = %q, 期望 \"0\"", raw)
	}

	session, err := logic.GetSession(ctx, "0")
	if err != nil || session == nil {
		t.Fatalf("Account-Session 未写入: %v", err)
	}
	if session.Type != SessionTypeAccount {
		t.Errorf("session.type = %s", session.Type)
	}
	if len(session.TerminalList) != 1 || session.TerminalList[0].TokenValue != token {
		t.Errorf("terminalList 未记录 token: %+v", session.TerminalList)
	}

	// 校验通过并返回 loginId。
	loginID, err := logic.CheckLogin(ctx, token)
	if err != nil {
		t.Fatalf("CheckLogin 失败: %v", err)
	}
	if loginID != "0" {
		t.Errorf("CheckLogin loginId = %s", loginID)
	}
}

func TestLoginReusesTokenWhenShareEnabled(t *testing.T) {
	logic, _ := newTestLogic(t, Config{Timeout: 3600, IsConcurrent: true, IsShare: true})
	ctx := context.Background()

	first, err := logic.Login(ctx, "u-1")
	if err != nil {
		t.Fatalf("首次登录失败: %v", err)
	}
	second, err := logic.Login(ctx, "u-1")
	if err != nil {
		t.Fatalf("二次登录失败: %v", err)
	}
	if first != second {
		t.Errorf("is-share=true 时应复用 token，得到 %s / %s", first, second)
	}
}

func TestLoginReplacesOldTokenWhenNotConcurrent(t *testing.T) {
	logic, _ := newTestLogic(t, Config{Timeout: 3600, IsConcurrent: false, IsShare: false})
	ctx := context.Background()

	first, err := logic.Login(ctx, "u-2")
	if err != nil {
		t.Fatalf("首次登录失败: %v", err)
	}
	second, err := logic.Login(ctx, "u-2")
	if err != nil {
		t.Fatalf("二次登录失败: %v", err)
	}
	if first == second {
		t.Fatalf("is-concurrent=false 时应签发新 token")
	}
	if _, err := logic.CheckLogin(ctx, first); !errors.Is(err, ErrNotFound) {
		t.Errorf("旧 token 应被顶下线，实际 err=%v", err)
	}
	if _, err := logic.CheckLogin(ctx, second); err != nil {
		t.Errorf("新 token 应有效，实际 err=%v", err)
	}
}

func TestLogoutRemovesTokenAndTerminal(t *testing.T) {
	logic, store := newTestLogic(t, Config{Timeout: 3600})
	ctx := context.Background()

	token, err := logic.Login(ctx, "u-3")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if err := logic.Logout(ctx, token); err != nil {
		t.Fatalf("登出失败: %v", err)
	}
	if _, err := store.Get(ctx, logic.TokenKey(token)); !errors.Is(err, ErrNotFound) {
		t.Errorf("token 映射应被删除，实际 err=%v", err)
	}
	// 终端被清空后账号会话整体删除（与 Java 侧 logoutByTokenValue 行为一致）。
	if _, err := store.Get(ctx, logic.SessionKey("u-3")); !errors.Is(err, ErrNotFound) {
		t.Errorf("无终端的账号会话应被删除，实际 err=%v", err)
	}
}

func TestLogoutKeepsOtherSessions(t *testing.T) {
	logic, _ := newTestLogic(t, Config{Timeout: 3600, IsConcurrent: true, IsShare: false})
	ctx := context.Background()

	first, err := logic.Login(ctx, "u-4")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	second, err := logic.Login(ctx, "u-4")
	if err != nil {
		t.Fatalf("二次登录失败: %v", err)
	}
	if err := logic.Logout(ctx, first); err != nil {
		t.Fatalf("登出失败: %v", err)
	}
	session, err := logic.GetSession(ctx, "u-4")
	if err != nil || session == nil {
		t.Fatalf("会话应保留: %v", err)
	}
	if len(session.TerminalList) != 1 || session.TerminalList[0].TokenValue != second {
		t.Errorf("应只移除已登出终端: %+v", session.TerminalList)
	}
}

func TestActiveTimeoutExpiresSession(t *testing.T) {
	logic, _ := newTestLogic(t, Config{Timeout: 3600, ActiveTimeout: 10})
	ctx := context.Background()

	token, err := logic.Login(ctx, "u-5")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}

	// 时钟前移超过 active-timeout。
	logic.cfg.Now = func() time.Time { return time.Unix(1730000000+30, 0) }
	if _, err := logic.CheckLogin(ctx, token); !errors.Is(err, ErrNotFound) {
		t.Errorf("超过最低活跃频率应失效，实际 err=%v", err)
	}
}

func TestCheckPermissionExactMatch(t *testing.T) {
	session := NewSession("s")
	session.Set(DataKeyPermissions, []string{"acat:admin:system:dicts"})

	logic, _ := newTestLogic(t, Config{})
	if !logic.CheckPermission(session, "acat:admin:system:dicts") {
		t.Errorf("精确权限码应通过")
	}
	// 框架侧 hasPermission 只做精确匹配，不做前缀/通配。
	if logic.CheckPermission(session, "acat:admin:system") {
		t.Errorf("前缀不应通过")
	}
	if logic.CheckPermission(session, "*") {
		t.Errorf("通配符不应通过（与前端 hasPermission 语义一致）")
	}
	if logic.CheckPermission(nil, "acat:admin:system:dicts") {
		t.Errorf("空会话不应通过")
	}
}

func TestNewTokenValueStyles(t *testing.T) {
	cases := []struct {
		style  string
		length int
	}{
		{StyleUUID, 36},
		{StyleSimpleUUID, 32},
		{StyleRandom32, 32},
		{StyleRandom64, 64},
		{StyleRandom128, 128},
		{StyleTik, 36}, // 2_14_16__ = 2+1+14+1+16+2
	}
	for _, tc := range cases {
		got, err := NewTokenValue(tc.style)
		if err != nil {
			t.Fatalf("style %s 生成失败: %v", tc.style, err)
		}
		if len(got) != tc.length {
			t.Errorf("style %s 长度 = %d, 期望 %d (%s)", tc.style, len(got), tc.length, got)
		}
	}
	if _, err := NewTokenValue("unknown-style"); err == nil {
		t.Errorf("非法 style 应返回错误")
	}
}

func TestStoreSetTimeoutSemantics(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	// timeout=0 不写入（SaTokenDao.set 语义）。
	if err := store.Set(ctx, "k0", "v", 0); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	if _, err := store.Get(ctx, "k0"); !errors.Is(err, ErrNotFound) {
		t.Errorf("timeout=0 不应写入，实际 err=%v", err)
	}

	// NeverExpire 永久保存。
	if err := store.Set(ctx, "k1", "v", NeverExpire); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	expire, err := store.Expire(ctx, "k1")
	if err != nil {
		t.Fatalf("Expire 失败: %v", err)
	}
	if expire != NeverExpire {
		t.Errorf("NeverExpire 键剩余时间 = %d", expire)
	}

	// 过期键读取返回 ErrNotFound。
	if err := store.Set(ctx, "k2", "v", 1); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	store.now = func() time.Time { return time.Now().Add(2 * time.Second) }
	if _, err := store.Get(ctx, "k2"); !errors.Is(err, ErrNotFound) {
		t.Errorf("过期键应返回 ErrNotFound，实际 err=%v", err)
	}
}
