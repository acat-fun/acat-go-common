package apperr

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPStatusMapping(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   int
	}{
		{"nil", nil, http.StatusOK, 0},
		{"unauthorized", Unauthorized("未登录"), http.StatusUnauthorized, 401},
		{"forbidden", Forbidden("无权限"), http.StatusForbidden, 403},
		{"notfound", NotFound("不存在"), http.StatusNotFound, 404},
		{"conflict", Conflict("冲突"), http.StatusConflict, 409},
		{"badrequest", BadRequest("格式错误"), http.StatusBadRequest, 400},
		{"unprocessable", Unprocessable("校验失败"), http.StatusUnprocessableEntity, 422},
		{"unavailable", Unavailable("依赖不可用"), http.StatusServiceUnavailable, 503},
		{"plain", errors.New("普通错误"), http.StatusInternalServerError, 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HTTPStatusOf(tc.err); got != tc.status {
				t.Errorf("HTTPStatusOf = %d, 期望 %d", got, tc.status)
			}
			if got := CodeOf(tc.err); got != tc.code {
				t.Errorf("CodeOf = %d, 期望 %d", got, tc.code)
			}
		})
	}
}

func TestWrappedErrorIsPreserved(t *testing.T) {
	cause := errors.New("dial tcp 127.0.0.1:3306: connect: connection refused")
	err := Internal(cause, "查询用户失败")

	if !errors.Is(err, cause) {
		t.Errorf("errors.Is 应命中原始 cause")
	}
	if got := HTTPStatusOf(err); got != http.StatusInternalServerError {
		t.Errorf("状态码 = %d", got)
	}
	if err.Message != "查询用户失败" {
		t.Errorf("Message = %s", err.Message)
	}
	if err.Error() == "" || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("Error() 应包含 cause 内容: %s", err.Error())
	}
}

func TestAsWithWrappedChain(t *testing.T) {
	base := Forbidden("无权限")
	wrapped := fmt.Errorf("业务处理失败: %w", base)
	got, ok := As(wrapped)
	if !ok || got.HTTPStatus != http.StatusForbidden {
		t.Fatalf("As 未命中: ok=%v got=%+v", ok, got)
	}
}
