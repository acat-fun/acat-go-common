// Package satoken 复刻 Sa-Token v1.44.0 的登录态与权限模型，用于 Go 服务与 Java 服务
// 在同一 Redis 下共享会话（迁移期认证兼容层）。
//
// 复刻来源（Sa-Token v1.44.0 源码）：
//   - StpLogic.splicingKeyTokenValue  -> <tokenName>:<loginType>:token:<tokenValue>
//   - StpLogic.splicingKeySession     -> <tokenName>:<loginType>:session:<loginId>
//   - StpLogic.splicingKeyTokenSession-> <tokenName>:<loginType>:token-session:<tokenValue>
//   - StpLogic.splicingKeyLastActiveTime -> <tokenName>:<loginType>:last-active:<tokenValue>
//   - StpLogic.saveTokenToIdMapping   -> token 映射的 value 是 String.valueOf(loginId)
//   - SaSession 默认构造              -> dataMap 为 ConcurrentHashMap，terminalList 为 ArrayList
//
// ⚠️ 兼容性边界：Session 的 JSON 形态由 sa-token-jackson（Jackson default typing，
// 属性形式 @class，NON_FINAL 类嵌入类型信息）产生。本包按该约定实现编解码，
// 但**尚未在真实 Redis 上与 Java 侧做交叉验证**，验证清单见包内 session_codec.go 注释与
// docs/task/2026-09-13-Java迁移阶段0-认证与权限机制.md。
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

// Session 内约定的数据键，由 ACAT 业务写入，Java 侧 StpInterfaceImpl 读取。
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

// Java 侧类型全限定名，用于生成/解析 Jackson 多态 JSON。
const (
	javaClassSession      = "cn.dev33.satoken.session.SaSession"
	javaClassTerminalInfo = "cn.dev33.satoken.session.SaTerminalInfo"
	javaClassLinkedMap    = "java.util.LinkedHashMap"
	javaClassArrayList    = "java.util.ArrayList"
)
