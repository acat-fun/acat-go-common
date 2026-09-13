// Package apperr 定义"需要改变 HTTP 状态码"的语义错误，与 Java 侧 GlobalExceptionHandler 对齐。
//
// Java 侧行为（lib/backend/acat-svc-common）：业务失败一律 HTTP 200 + body.code≠0；
// 只有下列语义错误才改 HTTP 状态码，并且响应体仍是 Result 结构。
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// HTTP 状态码常量，避免各服务硬编码。
const (
	StatusUnauthorized  = http.StatusUnauthorized        // 401 未登录
	StatusForbidden     = http.StatusForbidden           // 403 无权限
	StatusNotFound      = http.StatusNotFound            // 404 资源不存在
	StatusConflict      = http.StatusConflict            // 409 冲突
	StatusUnprocessable = http.StatusUnprocessableEntity // 422 参数校验失败
	StatusBadRequest    = http.StatusBadRequest          // 400 请求格式错误
	StatusUnavailable   = http.StatusServiceUnavailable  // 503 依赖不可用
	StatusInternal      = http.StatusInternalServerError // 500 未知错误
)

// Error 是带 HTTP 状态码与业务码的错误。
type Error struct {
	// HTTPStatus 决定响应状态行；默认 200 时由调用方按业务失败处理。
	HTTPStatus int
	// Code 是 body 中的业务码；默认 1，与 Result.fail 一致。
	Code int
	// Message 面向调用方的提示消息。
	Message string
	// cause 保留原始错误，便于日志与 %w 链。
	cause error
}

// Error 实现 error 接口。
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.cause != nil {
		return fmt.Sprintf("apperr: http=%d code=%d message=%s: %v", e.HTTPStatus, e.Code, e.Message, e.cause)
	}
	return fmt.Sprintf("apperr: http=%d code=%d message=%s", e.HTTPStatus, e.Code, e.Message)
}

// Unwrap 暴露原始错误，配合 errors.Is/As 使用。
func (e *Error) Unwrap() error { return e.cause }

// WithCause 附加原始 cause 并返回自身，便于链式构造。
func (e *Error) WithCause(err error) *Error {
	e.cause = err
	return e
}

func newf(status, code int, format string, args ...any) *Error {
	return &Error{HTTPStatus: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// Unauthorized 构造 401 未登录错误。
func Unauthorized(format string, args ...any) *Error {
	return newf(StatusUnauthorized, 401, format, args...)
}

// Forbidden 构造 403 无权限错误。
func Forbidden(format string, args ...any) *Error {
	return newf(StatusForbidden, 403, format, args...)
}

// NotFound 构造 404 错误。
func NotFound(format string, args ...any) *Error {
	return newf(StatusNotFound, 404, format, args...)
}

// Conflict 构造 409 冲突错误。
func Conflict(format string, args ...any) *Error {
	return newf(StatusConflict, 409, format, args...)
}

// BadRequest 构造 400 请求格式错误。
func BadRequest(format string, args ...any) *Error {
	return newf(StatusBadRequest, 400, format, args...)
}

// Unprocessable 构造 422 参数校验失败。
func Unprocessable(format string, args ...any) *Error {
	return newf(StatusUnprocessable, 422, format, args...)
}

// Unavailable 构造 503 依赖不可用错误。
func Unavailable(format string, args ...any) *Error {
	return newf(StatusUnavailable, 503, format, args...)
}

// Internal 构造 500 未知错误；基础设施异常必须保留 cause。
func Internal(cause error, format string, args ...any) *Error {
	return newf(StatusInternal, 500, format, args...).WithCause(cause)
}

// As 提取 *Error；不是该类型时返回 nil,false。
func As(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}

// HTTPStatusOf 返回错误应使用的 HTTP 状态码；普通错误按 500 处理，nil 按 200 处理。
func HTTPStatusOf(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if e, ok := As(err); ok {
		return e.HTTPStatus
	}
	return StatusInternal
}

// CodeOf 返回错误应使用的业务码；普通错误按 500 处理，nil 按 0 处理。
func CodeOf(err error) int {
	if err == nil {
		return 0
	}
	if e, ok := As(err); ok {
		return e.Code
	}
	return 500
}
