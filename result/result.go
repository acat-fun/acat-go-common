// Package result 提供与 Java 侧 fun.acat.common.result.Result 完全一致的统一响应结构。
//
// 契约要点（迁移期必须保持一致，见 docs/task/2026-09-13-Java迁移阶段0-契约清单.md）：
//   - 成功：HTTP 200 + {"code":0,"message":"OK","data":...}
//   - 业务失败：HTTP 200 + {"code":1,"message":"...","data":null}
//   - 仅认证/权限/未找到/冲突等语义错误才改变 HTTP 状态码（见 apperr 包）
package result

// Result 是统一响应包装，字段名与 Java 版逐字对齐。
type Result[T any] struct {
	Code    int    `json:"code"`    // 0=成功，非 0=业务错误
	Message string `json:"message"` // 提示消息
	Data    T      `json:"data"`    // 响应数据
}

// 与 Java 侧一致的业务码。
const (
	// CodeSuccess 成功。Java: Result.success -> code=0
	CodeSuccess = 0
	// CodeFail 默认业务失败码。Java: Result.fail(message) -> code=1
	CodeFail = 1
	// MessageOK 成功默认消息。Java: Result.success -> "OK"
	MessageOK = "OK"
)

// OK 返回成功响应（code=0, message="OK"）。
func OK[T any](data T) Result[T] {
	return Result[T]{Code: CodeSuccess, Message: MessageOK, Data: data}
}

// OKMessage 返回带自定义消息的成功响应。
func OKMessage[T any](message string, data T) Result[T] {
	return Result[T]{Code: CodeSuccess, Message: message, Data: data}
}

// Fail 返回默认失败码（1）的业务失败响应。
func Fail(message string) Result[any] {
	return Result[any]{Code: CodeFail, Message: message, Data: nil}
}

// FailCode 返回指定业务码的失败响应。
func FailCode(code int, message string) Result[any] {
	return Result[any]{Code: code, Message: message, Data: nil}
}

// IsSuccess 判断响应是否成功。
func (r Result[T]) IsSuccess() bool { return r.Code == CodeSuccess }

// PageData 是分页返回结构，字段名与 Java 版一致。
//
// 注意：Java 侧字段名是 list（不是 records），且额外包含 headNodeTotal。
type PageData[T any] struct {
	Total         int64 `json:"total"`         // 总条数
	HeadNodeTotal int64 `json:"headNodeTotal"` // 顶层节点数（树形分页使用，扁平列表等于 total）
	PageIndex     int   `json:"pageIndex"`     // 当前页码，1 基
	PageSize      int   `json:"pageSize"`      // 每页条数
	List          []T   `json:"list"`          // 数据列表
}

// NewPageData 构造分页结果；list 为 nil 时输出空数组，避免前端取不到数组。
func NewPageData[T any](list []T, total int64, pageIndex, pageSize int) PageData[T] {
	if list == nil {
		list = []T{}
	}
	return PageData[T]{
		Total:         total,
		HeadNodeTotal: total,
		PageIndex:     pageIndex,
		PageSize:      pageSize,
		List:          list,
	}
}

// 与 Java 侧一致的分页默认值。
const (
	// DefaultPageIndex 默认页码（1 基）。
	DefaultPageIndex = 1
	// DefaultPageSize 默认每页条数。
	DefaultPageSize = 10
	// MaxPageSize 分页上限。Java 侧 PageParam 的 @Max(100) 因 Controller 未加 @Valid 实际不生效，
	// 迁移期 Go 侧按同样口径不强制拦截，仅在应用层提供 NormalizePage 供新接口使用。
	MaxPageSize = 100
)

// NormalizePage 归一化分页参数：页码最小 1，页大小落在 [1, MaxPageSize]。
func NormalizePage(pageIndex, pageSize int) (int, int) {
	if pageIndex < 1 {
		pageIndex = DefaultPageIndex
	}
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return pageIndex, pageSize
}

// Offset 计算 SQL offset。
func Offset(pageIndex, pageSize int) int {
	if pageIndex < 1 {
		pageIndex = DefaultPageIndex
	}
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	return (pageIndex - 1) * pageSize
}
