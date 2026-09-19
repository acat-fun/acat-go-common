package apperr

import (
	"errors"
	"fmt"
)

// 本文件定义"业务失败"语义：对应
// HTTP 200 + body.code≠0。业务失败与基础设施异常必须分开，
// 后者（数据库/Redis/网络）要按 5xx 抛出并保留 cause。

// MessageInvalidCredential 等业务提示语由各服务定义；这里只提供类型与构造器。

// Business 表示可预期的业务失败（HTTP 200 + 业务码）。
type Business struct {
	// Code 业务码，默认 1。
	Code int
	// Message 面向调用方的提示。
	Message string
	// cause 保留内部原因（如并发冲突哨兵），只进日志与错误链，不出现在响应体。
	cause error
}

// BusinessCodeFail 是默认业务失败码。
const BusinessCodeFail = 1

// 统一业务错误码（对外 body.code；HTTP 状态保持 200。
//
// 写入语义分级约定：
//   - 既有失败路径沿用 code=1 + 既有文案，前端与调用方零感知；
//   - 新增的**严格模式**失败（并发冲突、状态已变化等曾静默成功的情形）
//     使用下列专用码，便于前端与监控区分。
const (
	// CodeStateChanged 资源状态已变化（40901）：目标存在，但当前状态不允许本次操作。
	CodeStateChanged = 40901
	// CodeOptimisticLock 乐观锁冲突（40902）：带 version 条件的更新未命中任何行。
	CodeOptimisticLock = 40902
	// CodeTargetMissing 目标不存在（40401）：本次操作期望的目标行不存在。
	CodeTargetMissing = 40401
	// CodeOperationCompleted 操作已完成（40903）：重复请求，结果与首次一致。
	CodeOperationCompleted = 40903
)

// Error 实现 error 接口；附加 cause 时同时输出内部原因，便于日志定位。
func (b *Business) Error() string {
	if b == nil {
		return ""
	}
	if b.cause != nil {
		return fmt.Sprintf("%s (code=%d): %v", b.Message, b.Code, b.cause)
	}
	return b.Message
}

// Unwrap 暴露内部原因，配合 errors.Is/errors.As 使用。
func (b *Business) Unwrap() error {
	if b == nil {
		return nil
	}
	return b.cause
}

// WithCause 附加内部原因（保留错误链）并返回自身。
func (b *Business) WithCause(err error) *Business {
	if b != nil {
		b.cause = err
	}
	return b
}

// NewBusiness 构造业务失败（code=1）。
func NewBusiness(message string) *Business {
	return &Business{Code: BusinessCodeFail, Message: message}
}

// NewBusinessCode 构造指定业务码的业务失败。
func NewBusinessCode(code int, message string) *Business {
	return &Business{Code: code, Message: message}
}

// StateChanged 构造「资源状态已变化」（40901）。
func StateChanged(message string) *Business {
	return NewBusinessCode(CodeStateChanged, message)
}

// OptimisticLock 构造「乐观锁冲突」（40902）。
func OptimisticLock(message string) *Business {
	return NewBusinessCode(CodeOptimisticLock, message)
}

// TargetMissing 构造「目标不存在」（40401）。
func TargetMissing(message string) *Business {
	return NewBusinessCode(CodeTargetMissing, message)
}

// OperationCompleted 构造「操作已完成」（40903）。
func OperationCompleted(message string) *Business {
	return NewBusinessCode(CodeOperationCompleted, message)
}

// IsBusiness 判断错误是否为业务失败，并返回该错误。
func IsBusiness(err error) (*Business, bool) {
	if err == nil {
		return nil, false
	}
	var target *Business
	if errors.As(err, &target) {
		return target, true
	}
	return nil, false
}
