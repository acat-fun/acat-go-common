// Package permission 提供管理端权限判定（Sa-Token 会话快照 + root 直通）的公共实现。
//
// 背景：Java 侧每个服务都有 `StpInterfaceImpl` + `@SaCheckPermission` 注解拦截；
// 迁移到 Go 后没有注解拦截器，由各服务 httpapi 显式调用本包的 Checker/Actor 完成同样判定。
// 阶段 1/2 各服务曾各自复制一份（admin-site/account/operation/…），此处上提为公共能力。
//
// 与 Java 侧的逐字对齐点：
//   - root 固定 `loginID == "0"`（Java `StpInterfaceImpl.ROOT_LOGIN_ID`）；
//   - 其余用户读登录时写入会话的 permissions 快照（Java `StpInterfaceImpl.getPermissionList`）；
//   - 不通过返回 `apperr.Forbidden("无操作权限")`（HTTP 403，文案与 Java 一致）。
package permission

import (
	"context"
	"fmt"

	"gitea.acat.fun/acat-fun/acat-go-common/apperr"
	"gitea.acat.fun/acat-fun/acat-go-common/middleware"
	"gitea.acat.fun/acat-fun/acat-go-common/satoken"
)

// RootLoginID 是超级管理员固定 id（Java `StpInterfaceImpl.ROOT_LOGIN_ID`）。
const RootLoginID = "0"

// MessageForbidden 是权限不足的统一文案（Java `GlobalExceptionHandler` 403 文案）。
const MessageForbidden = "无操作权限"

// Checker 封装管理端权限判定：root 直接放行，其余读 Sa-Token 会话权限快照。
type Checker struct {
	logic *satoken.Logic
}

// NewChecker 构造 Checker；logic 为 nil 时任何非 root 判定都会拒绝（不 panic）。
func NewChecker(logic *satoken.Logic) *Checker { return &Checker{logic: logic} }

// CheckPermission 判定会话是否具备权限码 code。
func (c *Checker) CheckPermission(session *satoken.Session, code string) error {
	if LoginID(session) == RootLoginID {
		return nil
	}
	if c == nil || c.logic == nil || session == nil || code == "" {
		return apperr.Forbidden(MessageForbidden)
	}
	if !c.logic.CheckPermission(session, code) {
		return apperr.Forbidden(MessageForbidden)
	}
	return nil
}

// LoginID 从会话读取登录 id；会话为空或字段缺失时返回空串。
func LoginID(session *satoken.Session) string {
	if session == nil || session.LoginID == nil {
		return ""
	}
	if value, ok := session.LoginID.(string); ok {
		return value
	}
	return fmt.Sprintf("%v", session.LoginID)
}

// Actor 是当前请求的操作者视图（登录 id + 会话 + 权限判定器）。
//
// 对应 Java 侧的 StpUtil 线程上下文：Service 层用它完成 root 判定与写路径的 createBy/updateBy。
type Actor struct {
	// LoginID 当前登录用户 id；未登录为空串。
	LoginID string
	// Session 当前账号会话（可能为 nil）。
	Session *satoken.Session
	checker *Checker
}

// ActorFrom 从请求上下文（middleware.Auth 写入的会话）构造操作者。
func ActorFrom(ctx context.Context, checker *Checker) *Actor {
	session := middleware.SessionFrom(ctx)
	return &Actor{LoginID: LoginID(session), Session: session, checker: checker}
}

// IsRoot 判断当前操作者是否为超级管理员（只按 loginID=="0"，与 Java isRoot() 一致）。
func (a *Actor) IsRoot() bool {
	return a != nil && a.LoginID == RootLoginID
}

// RequirePermission 判定权限码：root 放行，其余按会话权限判定，不通过返回 403。
func (a *Actor) RequirePermission(code string) error {
	if a == nil {
		return apperr.Forbidden(MessageForbidden)
	}
	if a.IsRoot() {
		return nil
	}
	return a.checker.CheckPermission(a.Session, code)
}

// RequireAllPermissions 判定多个权限码（AND 语义）。
//
// 对应 Java 的「类级 + 方法级」双注解：Sa-Token 1.44 的 SaAnnotationStrategy
// 先校验类注解再校验方法级注解，两者都通过才放行。
func (a *Actor) RequireAllPermissions(codes ...string) error {
	for _, code := range codes {
		if err := a.RequirePermission(code); err != nil {
			return err
		}
	}
	return nil
}

// RequireAnyPermission 判定多个权限码（OR 语义）。
//
// 对应 Java `@SaCheckPermission(value = {A, B}, mode = SaMode.OR)`：
// 任一命中即通过；空列表按“无权限”处理（Java 注解 value 为空时 Sa-Token 抛异常，不会静默放行）。
func (a *Actor) RequireAnyPermission(codes ...string) error {
	if a == nil {
		return apperr.Forbidden(MessageForbidden)
	}
	if a.IsRoot() {
		return nil
	}
	if len(codes) == 0 {
		return apperr.Forbidden(MessageForbidden)
	}
	for _, code := range codes {
		if a.checker.CheckPermission(a.Session, code) == nil {
			return nil
		}
	}
	return apperr.Forbidden(MessageForbidden)
}

// RequirePermissionGroups 判定多组权限码：组间 AND、组内 OR。
//
// 对应 Java 的「类级注解 OR 列表 + 方法级注解 OR 列表」，两者都通过才放行。
func (a *Actor) RequirePermissionGroups(groups ...[]string) error {
	for _, group := range groups {
		if err := a.RequireAnyPermission(group...); err != nil {
			return err
		}
	}
	return nil
}
