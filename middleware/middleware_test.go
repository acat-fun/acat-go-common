package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"git.acat.fun/acat-fun/acat-go-common/apperr"
	"git.acat.fun/acat-fun/acat-go-common/logging"
	"git.acat.fun/acat-fun/acat-go-common/result"
	"git.acat.fun/acat-fun/acat-go-common/satoken"
)

func newAuthFixture(t *testing.T) (*satoken.Logic, string) {
	t.Helper()
	logic := satoken.NewLogic(satoken.Config{
		TokenName: "acat-admin-token",
		Timeout:   3600,
		Now:       func() time.Time { return time.Unix(1730000000, 0) },
	}, satoken.NewMemoryStore(), nil)
	ctx := context.Background()
	token, err := logic.Login(ctx, "0")
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	session, err := logic.GetSession(ctx, "0")
	if err != nil || session == nil {
		t.Fatalf("读取会话失败: %v", err)
	}
	session.Set(satoken.DataKeyPermissions, []string{"acat:admin:system:dicts"})
	if err := logic.SaveSession(ctx, session); err != nil {
		t.Fatalf("保存会话失败: %v", err)
	}
	return logic, token
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		session := SessionFrom(req.Context())
		loginID := ""
		if session != nil {
			loginID = session.String(satoken.DataKeyUsername)
		}
		WriteResult(w, result.OK(map[string]any{"loginType": satoken.SessionTypeAccount, "username": loginID, "token": TokenFrom(req.Context())}))
	})
}

func TestAuthAcceptsCookie(t *testing.T) {
	logic, token := newAuthFixture(t)
	handler := Auth(AuthConfig{Logic: logic})(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/admin/user/auth/bootstrap", nil)
	req.AddCookie(&http.Cookie{Name: "acat-admin-token", Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if payload["code"] != float64(0) {
		t.Errorf("业务码 = %v", payload["code"])
	}
}

func TestAuthRejectsMissingToken(t *testing.T) {
	logic, _ := newAuthFixture(t)
	handler := Auth(AuthConfig{Logic: logic})(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/admin/user/auth/bootstrap", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d, 期望 401", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if payload["message"] != MessageNotLoggedIn || payload["data"] != nil || payload["success"] != false {
		t.Errorf("响应体 = %v", payload)
	}
	// 401 文案 逐字一致。
	if payload["message"] != "未登录或登录已过期，请重新登录" {
		t.Errorf("message = %v", payload["message"])
	}
	// 错误信封同样是
	want := `{"code":401,"message":"未登录或登录已过期，请重新登录","data":null,"success":false}` + "\n"
	if rec.Body.String() != want {
		t.Errorf("响应体 = %q, 期望 %q", rec.Body.String(), want)
	}
}

func TestAuthCookieOnlyIgnoresHeader(t *testing.T) {
	logic, token := newAuthFixture(t)
	handler := Auth(AuthConfig{Logic: logic, CookieOnly: true})(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/admin/user/auth/bootstrap", nil)
	req.Header.Set(TokenHeader, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("CookieOnly 模式下请求头 token 应被忽略，状态码 = %d", rec.Code)
	}
}

func TestAuthAcceptsTokenHeader(t *testing.T) {
	logic, token := newAuthFixture(t)
	handler := Auth(AuthConfig{Logic: logic})(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/api/admin/user/auth/bootstrap", nil)
	req.Header.Set(TokenHeader, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestTraceGeneratesAndPropagatesTraceID(t *testing.T) {
	var seen string
	inner := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		seen = logging.TraceID(req.Context())
	})
	handler := Trace()(inner)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if seen == "" {
		t.Errorf("未生成 trace_id")
	}
	if got := rec.Header().Get(logging.HeaderTraceID); got != seen {
		t.Errorf("响应头 trace_id = %q, 上下文 = %q", got, seen)
	}

	// 上游传入时透传。
	req2 := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req2.Header.Set(logging.HeaderTraceID, "upstream-trace")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if seen != "upstream-trace" {
		t.Errorf("trace_id 未透传: %s", seen)
	}
}

func TestRecoverTurnsPanicInto500(t *testing.T) {
	handler := Recover(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, 期望 500", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if payload["message"] != "服务内部错误" {
		t.Errorf("响应体 = %v", payload)
	}
}

func TestWriteErrorKeepsResultShape(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(context.Background(), rec, apperr.Forbidden("无操作权限"))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if payload["code"] != float64(403) || payload["message"] != "无操作权限" || payload["data"] != nil {
		t.Errorf("响应体 = %v", payload)
	}
}

func TestCORSAllowCredentialsWithExplicitOrigin(t *testing.T) {
	handler := CORS([]string{"https://admin.acat.fun"}, true)(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/user/auth/bootstrap", nil)
	req.Header.Set("Origin", "https://admin.acat.fun")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.acat.fun" {
		t.Errorf("Allow-Origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q", got)
	}
}

func TestCORSWildcardNeverCombinesWithCredentials(t *testing.T) {
	handler := CORS([]string{"*"}, true)(okHandler())
	req := httptest.NewRequest(http.MethodGet, "/api/admin/user/auth/bootstrap", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("带凭据时不应回显通配来源，实际 %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("不应下发 Allow-Credentials，实际 %q", got)
	}
}
