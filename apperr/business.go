package apperr

// 本文件定义"业务失败"语义：Java 侧 Result.fail / ServiceResult.fail 对应
// HTTP 200 + body.code≠0。业务失败与基础设施异常必须分开，
// 后者（数据库/Redis/网络）要按 5xx 抛出并保留 cause。

// MessageInvalidCredential 等业务提示语由各服务定义；这里只提供类型与构造器。

// Business 表示可预期的业务失败（HTTP 200 + 业务码）。
type Business struct {
	// Code 业务码，默认 1。
	Code int
	// Message 面向调用方的提示。
	Message string
}

// BusinessCodeFail 是 Java 侧 Result.fail 的默认业务码。
const BusinessCodeFail = 1

// Error 实现 error 接口。
func (b *Business) Error() string {
	if b == nil {
		return ""
	}
	return b.Message
}

// NewBusiness 构造业务失败（code=1）。
func NewBusiness(message string) *Business {
	return &Business{Code: BusinessCodeFail, Message: message}
}

// NewBusinessCode 构造指定业务码的业务失败。
func NewBusinessCode(code int, message string) *Business {
	return &Business{Code: code, Message: message}
}

// IsBusiness 判断错误是否为业务失败，并返回该错误。
func IsBusiness(err error) (*Business, bool) {
	if err == nil {
		return nil, false
	}
	var target *Business
	if asError(err, &target) {
		return target, true
	}
	return nil, false
}

// asError 是 errors.As 的薄封装，避免本文件依赖 errors 包的命名冲突。
func asError(err error, target **Business) bool {
	for err != nil {
		if b, ok := err.(*Business); ok {
			*target = b
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}
