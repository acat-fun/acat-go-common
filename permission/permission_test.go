package permission

import (
	"context"
	"testing"

	"gitea.acat.fun/acat-fun/acat-go-common/apperr"
	"gitea.acat.fun/acat-fun/acat-go-common/satoken"
)

// newChecker 构造带内存会话的 Logic 与 Checker。
func newChecker(t *testing.T) (*Checker, *satoken.Logic) {
	t.Helper()
	logic := satoken.NewLogic(satoken.Config{
		TokenName:  "satoken",
		LoginType:  "login",
		Timeout:    satoken.DefaultTimeoutSeconds,
		IsShare:    true,
		TokenStyle: "uuid",
	}, satoken.NewMemoryStore(), nil)
	return NewChecker(logic), logic
}

func sessionWithPermissions(t *testing.T, logic *satoken.Logic, loginID string, permissions []string) *satoken.Session {
	t.Helper()
	if _, err := logic.Login(context.Background(), loginID); err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	session, err := logic.GetSession(context.Background(), loginID)
	if err != nil || session == nil {
		t.Fatalf("读取会话失败: %v", err)
	}
	session.Set("permissions", permissions)
	return session
}

// TestCheckPermissionRootBypass root（loginID=="0"）必须直接放行。
func TestCheckPermissionRootBypass(t *testing.T) {
	checker, logic := newChecker(t)
	session := sessionWithPermissions(t, logic, RootLoginID, nil)
	if err := checker.CheckPermission(session, "acat:read:admin:content:pages:delete"); err != nil {
		t.Fatalf("root 应放行任意权限码，实际: %v", err)
	}
}

// TestCheckPermissionDenied 普通用户缺少权限码时必须 403 + 「无操作权限」。
func TestCheckPermissionDenied(t *testing.T) {
	checker, logic := newChecker(t)
	session := sessionWithPermissions(t, logic, "u-1", []string{"acat:read:admin:content:files"})
	err := checker.CheckPermission(session, "acat:read:admin:content:files:delete")
	if err == nil {
		t.Fatalf("缺少权限码时必须拒绝")
	}
	appErr, ok := apperr.As(err)
	if !ok || appErr.HTTPStatus != 403 || appErr.Message != MessageForbidden {
		t.Fatalf("错误 = %#v, want 403 + %q", err, MessageForbidden)
	}
}

// TestCheckPermissionGranted 普通用户持有权限码时放行。
func TestCheckPermissionGranted(t *testing.T) {
	checker, logic := newChecker(t)
	session := sessionWithPermissions(t, logic, "u-2", []string{"a", "acat:read:admin:content:files:upload"})
	if err := checker.CheckPermission(session, "acat:read:admin:content:files:upload"); err != nil {
		t.Fatalf("持有权限码应放行，实际: %v", err)
	}
}

// TestRequireAllPermissionsAndSemantics 锁定“类级 + 方法级”AND 语义。
func TestRequireAllPermissionsAndSemantics(t *testing.T) {
	checker, logic := newChecker(t)
	session := sessionWithPermissions(t, logic, "u-3", []string{"class-code"})
	actor := &Actor{LoginID: "u-3", Session: session, checker: checker}
	if err := actor.RequireAllPermissions("class-code", "method-code"); err == nil {
		t.Fatalf("只有类级权限码时必须拒绝方法级判定")
	}
	session2 := sessionWithPermissions(t, logic, "u-4", []string{"class-code", "method-code"})
	actor2 := &Actor{LoginID: "u-4", Session: session2, checker: checker}
	if err := actor2.RequireAllPermissions("class-code", "method-code"); err != nil {
		t.Fatalf("同时持有两个权限码应放行，实际: %v", err)
	}
}

// TestRequireAnyPermissionOrSemantics 锁定 OR 语义与空列表拒绝。
func TestRequireAnyPermissionOrSemantics(t *testing.T) {
	checker, logic := newChecker(t)
	session := sessionWithPermissions(t, logic, "u-5", []string{"b"})
	actor := &Actor{LoginID: "u-5", Session: session, checker: checker}
	if err := actor.RequireAnyPermission("a", "b"); err != nil {
		t.Fatalf("OR 命中其一应放行，实际: %v", err)
	}
	if err := actor.RequireAnyPermission("a", "c"); err == nil {
		t.Fatalf("OR 全不命中必须拒绝")
	}
	if err := actor.RequireAnyPermission(); err == nil {
		t.Fatalf("空权限码列表必须拒绝")
	}
}

// TestRequirePermissionGroups 锁定「组间 AND、组内 OR」。
func TestRequirePermissionGroups(t *testing.T) {
	checker, logic := newChecker(t)
	session := sessionWithPermissions(t, logic, "u-6", []string{"class-a"})
	actor := &Actor{LoginID: "u-6", Session: session, checker: checker}
	if err := actor.RequirePermissionGroups([]string{"class-a", "class-b"}, []string{"method-a"}); err == nil {
		t.Fatalf("缺少方法级组的任一笔必须拒绝")
	}
	session2 := sessionWithPermissions(t, logic, "u-7", []string{"class-b", "method-a"})
	actor2 := &Actor{LoginID: "u-7", Session: session2, checker: checker}
	if err := actor2.RequirePermissionGroups([]string{"class-a", "class-b"}, []string{"method-a"}); err != nil {
		t.Fatalf("每组均有命中应放行，实际: %v", err)
	}
}

// TestRootBypassesPermission root 绕过所有分组判定。
func TestRootBypassesPermission(t *testing.T) {
	checker, logic := newChecker(t)
	session := sessionWithPermissions(t, logic, RootLoginID, nil)
	actor := &Actor{LoginID: RootLoginID, Session: session, checker: checker}
	if !actor.IsRoot() {
		t.Fatalf("IsRoot() 应为 true")
	}
	if err := actor.RequirePermissionGroups([]string{"a"}, []string{"b"}); err != nil {
		t.Fatalf("root 应放行，实际: %v", err)
	}
	if err := actor.RequireAnyPermission(); err != nil {
		t.Fatalf("root 应放行空列表，实际: %v", err)
	}
}

// TestNilActorAndSession nil 操作者/会话一律拒绝，不 panic。
func TestNilActorAndSession(t *testing.T) {
	var actor *Actor
	if err := actor.RequirePermission("a"); err == nil {
		t.Fatal("nil 操作者必须拒绝")
	}
	if actor.IsRoot() {
		t.Fatal("nil 操作者不是 root")
	}
	checker, _ := newChecker(t)
	if err := checker.CheckPermission(nil, "a"); err == nil {
		t.Fatal("nil 会话必须拒绝")
	}
	if LoginID(nil) != "" {
		t.Fatal("nil 会话 LoginID 应为空串")
	}
}

// TestLoginIDNumericValue 会话中 loginID 为数字时按字符串比较。
func TestLoginIDNumericValue(t *testing.T) {
	session := &satoken.Session{LoginID: float64(0)}
	if LoginID(session) != "0" {
		t.Fatalf("LoginID() = %q, 期望 \"0\"", LoginID(session))
	}
}
