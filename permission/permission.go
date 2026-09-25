// Package permission 提供管理端权限判定（Sa-Token 会话快照 + root 角色直通）的公共实现。
//
// 背景：管理端各服务都需要权限拦截，由各服务 httpapi 显式调用本包的 Checker/Actor 完成判定；
// admin-site / account / operation 等服务共用本实现。
//
// 对齐点：
//   - 超级管理员统一按角色判定：会话 roles 快照包含 RootRoleID（"root"）即视为超管，直接放行；
//   - 其余用户读登录时写入会话的 permissions 快照；
//   - 不通过返回 `apperr.Forbidden("无操作权限")`（HTTP 403）。
//
// root 角色判定依赖登录服务（acat-admin-user）在 buildBootstrap 时把角色码写入会话
// （satoken.DataKeyRoles）；角色表 t_acat_role 中超管角色行的 id 与 code 同为字面量 "root"。
package permission

import (
	"context"
	"fmt"

	"github.com/acat-fun/acat-go-common/apperr"
	"github.com/acat-fun/acat-go-common/middleware"
	"github.com/acat-fun/acat-go-common/satoken"
)

// RootRoleID 是超级管理员角色的 id 与角色码（t_acat_role.id = t_acat_role.code = "root"）。
//
// 统一口径：超管判定只看「用户是否绑定了 id 为 root 的角色」，不再按登录 id 特判。
const RootRoleID = "root"

// RootLoginID 是历史口径下超级管理员固定登录 id（worker 表 id="0"）。
//
// Deprecated: 超管判定已统一为角色判定（会话 roles 含 RootRoleID）；本常量仅供
// 兼容与数据订正脚本引用，新代码不要使用。
const RootLoginID = "0"

// MessageForbidden 是权限不足的统一文案。
const MessageForbidden = "无操作权限"

// Checker 封装管理端权限判定：root 角色直接放行，其余读 Sa-Token 会话权限快照。
type Checker struct {
	logic *satoken.Logic
}

// NewChecker 构造 Checker；logic 为 nil 时任何非 root 判定都会拒绝（不 panic）。
func NewChecker(logic *satoken.Logic) *Checker { return &Checker{logic: logic} }

// CheckPermission 判定会话是否具备权限码 code。
func (c *Checker) CheckPermission(session *satoken.Session, code string) error {
	if IsRootSession(session) {
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

// IsRootSession 判断会话是否绑定了超级管理员角色（会话 roles 快照含 "root"）。
//
// 这是超管判定的唯一权威口径；角色快照由登录服务在 buildBootstrap 时写入
// （satoken.DataKeyRoles）。会话为 nil 或未写 roles 时一律不是超管。
func IsRootSession(session *satoken.Session) bool {
	if session == nil {
		return false
	}
	for _, role := range session.StringList(satoken.DataKeyRoles) {
		if role == RootRoleID {
			return true
		}
	}
	return false
}

// Actor 是当前请求的操作者视图（登录 id + 会话 + 权限判定器）。
//
// 等价于请求上下文：Service 层用它完成 root 判定与写路径的 createBy/updateBy。
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

// IsRoot 判断当前操作者是否为超级管理员（会话角色含 "root"）。
func (a *Actor) IsRoot() bool {
	return a != nil && IsRootSession(a.Session)
}

// RequirePermission 判定权限码：root 角色放行，其余按会话权限判定，不通过返回 403。
func (a *Actor) RequirePermission(code string) error {
	if a == nil {
		return apperr.Forbidden(MessageForbidden)
	}
	if a.IsRoot() {
		return nil
	}
	if a.checker == nil {
		return apperr.Forbidden(MessageForbidden)
	}
	return a.checker.CheckPermission(a.Session, code)
}

// RequireAllPermissions 判定多个权限码（AND 语义）。
//
// 类级 + 方法级双注解组合。
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
// OR 权限模式（任一命中即通过）：
// 任一命中即通过；空列表按“无权限”处理。
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
	if a.checker == nil {
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
// 对应
func (a *Actor) RequirePermissionGroups(groups ...[]string) error {
	for _, group := range groups {
		if err := a.RequireAnyPermission(group...); err != nil {
			return err
		}
	}
	return nil
}
