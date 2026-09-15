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

// TestBusinessCodesAndCause 统一业务码 + cause 链：前端只看 message，内部仍可 errors.Is 定位。
func TestBusinessCodesAndCause(t *testing.T) {
	cases := []struct {
		name string
		err  *Business
		code int
	}{
		{"资源状态已变化", StateChanged("状态已变化"), 40901},
		{"乐观锁冲突", OptimisticLock("并发冲突"), 40902},
		{"目标不存在", TargetMissing("目标不存在"), 40401},
		{"操作已完成", OperationCompleted("审批单已处理"), 40903},
		{"默认业务失败保持 code=1", NewBusiness("书籍不存在"), BusinessCodeFail},
	}
	for _, item := range cases {
		if item.err.Code != item.code {
			t.Fatalf("%s 业务码应为 %d，实际 %d", item.name, item.code, item.err.Code)
		}
		if _, ok := IsBusiness(item.err); !ok {
			t.Fatalf("%s 应被 IsBusiness 识别", item.name)
		}
	}

	// cause 保留：errors.Is 能穿透到内部哨兵，但响应仍用 Message。
	sentinel := errors.New("version mismatch")
	err := OperationCompleted("审批单已处理").WithCause(fmt.Errorf("decideApproval: %w", sentinel))
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithCause 后应可 errors.Is 命中原因: %v", err)
	}
	if err.Message != "审批单已处理" {
		t.Fatalf("Message 不应被 cause 影响: %s", err.Message)
	}
	// 经 fmt.Errorf 包装后仍可识别为业务错误，且能取到原因。
	wrapped := fmt.Errorf("service: %w", err)
	business, ok := IsBusiness(wrapped)
	if !ok || business.Code != CodeOperationCompleted {
		t.Fatalf("包装后应仍识别为业务失败: ok=%v %+v", ok, business)
	}
	if !errors.Is(wrapped, sentinel) {
		t.Fatalf("包装后应仍可 errors.Is 命中原因: %v", wrapped)
	}
}
