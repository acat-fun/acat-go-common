// Package result 提供与 Java 侧 fun.acat.common.result.Result 完全一致的统一响应结构。
//
// 契约要点（迁移期必须保持一致，见 docs/task/2026-09-13-Java迁移阶段0-契约清单.md）：
//   - 成功：HTTP 200 + {"code":0,"message":"OK","data":...,"success":true}
//   - 业务失败：HTTP 200 + {"code":1,"message":"...","data":null,"success":false}
//   - 顶层键序固定为 code→message→data→success，与 Java 侧逐字对齐
//   - success 是布尔值且恒等于 code==0（Java: Result#success），由 OK/OKMessage/Fail/FailCode 统一维护
//   - 仅认证/权限/未找到/冲突等语义错误才改变 HTTP 状态码（见 apperr 包）
package result

// Result 是统一响应包装，字段名与 Java 版逐字对齐。
//
// Success 必须声明在 Data 之后：Go 的 encoding/json 按字段声明顺序输出键，
// 只有这个顺序才能保证键序为 Java 侧的 code→message→data→success。
type Result[T any] struct {
	Code    int    `json:"code"`    // 0=成功，非 0=业务错误
	Message string `json:"message"` // 提示消息
	Data    T      `json:"data"`    // 响应数据
	Success bool   `json:"success"` // 是否成功，恒等于 code==0（Java: Result#success）
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

// OK 返回成功响应（code=0, message="OK", success=true）。
func OK[T any](data T) Result[T] {
	return Result[T]{Code: CodeSuccess, Message: MessageOK, Data: data, Success: true}
}

// OKMessage 返回带自定义消息的成功响应（code=0, success=true）。
func OKMessage[T any](message string, data T) Result[T] {
	return Result[T]{Code: CodeSuccess, Message: message, Data: data, Success: true}
}

// Fail 返回默认失败码（1）的业务失败响应（success=false）。
func Fail(message string) Result[any] {
	return Result[any]{Code: CodeFail, Message: message, Data: nil, Success: false}
}

// FailCode 返回指定业务码的失败响应（success=false）。
func FailCode(code int, message string) Result[any] {
	return Result[any]{Code: code, Message: message, Data: nil, Success: false}
}

// IsSuccess 判断响应是否成功；语义保持 code==0，与 Java 侧 Result#isSuccess 一致。
func (r Result[T]) IsSuccess() bool { return r.Code == CodeSuccess }

// PageData 是分页返回结构，字段名与 Java 版一致。
//
// headNodeTotal 用指针以复刻 Java 语义：Java `PageData.of(total, pageIndex, pageSize, list)`
// **不设置** headNodeTotal → 序列化为 `null`；只有树分页 `PageData.ofTree(...)` 才给值
// （Java 侧唯一调用点在 `DictAdminServiceImpl` 的字典树分页，见 acat-svc-common/PageData.java:30-46）。
type PageData[T any] struct {
	Total         int64  `json:"total"`         // 总条数
	HeadNodeTotal *int64 `json:"headNodeTotal"` // 树分页根节点总数；普通分页为 null
	PageIndex     int    `json:"pageIndex"`     // 当前页码，1 基
	PageSize      int    `json:"pageSize"`      // 每页条数
	List          []T    `json:"list"`          // 数据列表
}

// NewPageData 构造**普通分页**结果（headNodeTotal=null，与 Java PageData.of 一致）；
// list 为 nil 时输出空数组，避免前端取不到数组。
func NewPageData[T any](list []T, total int64, pageIndex, pageSize int) PageData[T] {
	if list == nil {
		list = []T{}
	}
	return PageData[T]{
		Total:     total,
		PageIndex: pageIndex,
		PageSize:  pageSize,
		List:      list,
	}
}

// NewTreePageData 构造**树分页**结果（headNodeTotal 有值，与 Java PageData.ofTree 一致）。
func NewTreePageData[T any](list []T, total, headNodeTotal int64, pageIndex, pageSize int) PageData[T] {
	page := NewPageData(list, total, pageIndex, pageSize)
	page.HeadNodeTotal = &headNodeTotal
	return page
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
