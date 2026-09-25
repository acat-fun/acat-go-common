// Package satoken 实现 Sa-Token v1.44.0 的登录态与权限模型，
// 供 Go 服务在同一 Redis 下共享会话。
//
// 键形状取自 Sa-Token v1.44.0：
//   - StpLogic.splicingKeyTokenValue  -> <tokenName>:<loginType>:token:<tokenValue>
//   - StpLogic.splicingKeySession     -> <tokenName>:<loginType>:session:<loginId>
//   - StpLogic.splicingKeyTokenSession-> <tokenName>:<loginType>:token-session:<tokenValue>
//   - StpLogic.splicingKeyLastActiveTime -> <tokenName>:<loginType>:last-active:<tokenValue>
//   - StpLogic.saveTokenToIdMapping   -> token 映射的 value 是 String.valueOf(loginId)
//   - SaSession 默认构造              -> dataMap 为 ConcurrentHashMap，terminalList 为 ArrayList
//
// ⚠️ 兼容性边界：Session 的 JSON 形态遵循 sa-token-jackson 约定（Jackson default typing，
// 属性形式 @class，NON_FINAL 类嵌入类型信息）。本包按该约定实现编解码，
// 用例见包内 session_codec.go 与 session_codec_test.go。
package satoken

// 与 Sa-Token 默认配置一致的常量。
const (
	// DefaultTokenName 是 Sa-Token 默认 token 名；ACAT 管理端实际取值来自部署配置。
	DefaultTokenName = "satoken"
	// DefaultLoginType 是 Sa-Token 默认登录类型。
	DefaultLoginType = "login"

	// SessionTypeAccount 对应 SaTokenConsts.SESSION_TYPE__ACCOUNT。
	SessionTypeAccount = "Account-Session"
	// SessionTypeToken 对应 SaTokenConsts.SESSION_TYPE__TOKEN。
	SessionTypeToken = "Token-Session"

	// DefaultTimeoutSeconds 是 Sa-Token 默认登录有效期（30 天）。
	DefaultTimeoutSeconds int64 = 2592000
	// NeverExpire 对应 SaTokenDao.NEVER_EXPIRE（-1）。
	NeverExpire int64 = -1
	// NotValueExpire 对应 SaTokenDao.NOT_VALUE_EXPIRE（-2）。
	NotValueExpire int64 = -2
)

// Session 内约定的数据键，由 ACAT 业务写入
const (
	// DataKeyPermissions 权限码列表（Java: StpUtil.getSession().set("permissions", ...)）。
	DataKeyPermissions = "permissions"
	// DataKeyRoles 角色标识列表。
	DataKeyRoles = "roles"
	// DataKeyUsername 用户名。
	DataKeyUsername = "username"
)

// jacksonClass 是 Jackson default typing 的类型属性名。
const jacksonClass = "@class"

const (
	javaClassSession      = "cn.dev33.satoken.session.SaSession"
	javaClassTerminalInfo = "cn.dev33.satoken.session.SaTerminalInfo"
	javaClassLinkedMap    = "java.util.LinkedHashMap"
	javaClassArrayList    = "java.util.ArrayList"
)
