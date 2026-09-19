package db

import (
	"errors"
	"fmt"
	"log/slog"
)

// 本文件把「写操作影响行数」沉淀为可复用的**结果转换**工具：
//
//	Repository 只报告事实（affected + error + 查询结果），不决定业务语义；
//	Application Service 依据操作语义选择严格模式或兼容模式。
//
// 因此这里不提供「0 行一律报错」或「0 行一律成功」的封装：
//   - RequireAffected / RequireUpdated：严格模式，0 行即返回**调用方给定**的语义错误；
//   - WarnIfUnaffected：兼容模式，0 行不改变业务结果，只记录结构化 warn。
//
// 分级标准：严格模式（0 行返回调用方语义错误）与兼容模式（0 行仅记 warn）。

// ErrWriteNotApplied 是写操作未命中任何行的底层哨兵；
// 调用方未提供语义错误时作为兜底，避免 0 行被当成成功。
var ErrWriteNotApplied = errors.New("db: 写操作未命中任何行")

// WriteFact 描述一次写操作的客观结果（由 Repository 返回，供 Service 判定）。
type WriteFact struct {
	// Service 服务名，如 acat-read-admin-book。
	Service string
	// Operation 用例/操作名，如 decideApproval。
	Operation string
	// Entity 表名或实体名，如 acat_read.t_read_content_approval_order。
	Entity string
	// ID 目标主键。
	ID string
	// ExpectedVersion 乐观锁期望版本（无乐观锁时为 nil）。
	ExpectedVersion *int
	// Affected 实际影响行数。
	Affected int64
}

// LogValue 实现 slog.LogValuer：日志里以 write 分组输出全部定位字段。
func (f WriteFact) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("service", f.Service),
		slog.String("operation", f.Operation),
		slog.String("entity", f.Entity),
		slog.String("id", f.ID),
		slog.Int64("affected_rows", f.Affected),
	}
	if f.ExpectedVersion != nil {
		attrs = append(attrs, slog.Int("expected_version", *f.ExpectedVersion))
	}
	return slog.GroupValue(attrs...)
}

// RequireAffected 严格模式：目标必须存在，0 行返回调用方给定的「目标不存在」语义错误。
func RequireAffected(fact WriteFact, errTargetMissing error) error {
	return requireWrite(fact, errTargetMissing)
}

// RequireUpdated 严格模式：并发控制写必须命中，
// 0 行返回调用方给定的「状态已变化 / 乐观锁冲突」语义错误。
func RequireUpdated(fact WriteFact, errConflict error) error {
	return requireWrite(fact, errConflict)
}

// WarnIfUnaffected 兼容模式：0 行不改变业务结果，只记录结构化 warn（含 compatibility_mode）。
//
// 适用于「0 行是幂等结果」或「0 行为静默成功语义」的普通写入；
// 使用它意味着调用方**显式接受**该写不生效，而不是忽略返回值。
func WarnIfUnaffected(logger *slog.Logger, fact WriteFact) {
	if fact.Affected > 0 || logger == nil {
		return
	}
	logger.Warn("写操作未命中任何行（0 行影响），按兼容模式返回成功",
		"write", fact, "compatibility_mode", true)
}

// requireWrite 是两种严格模式的共同实现：0 行时包装调用方给定的语义错误。
func requireWrite(fact WriteFact, semantic error) error {
	if fact.Affected > 0 {
		return nil
	}
	if semantic == nil {
		semantic = ErrWriteNotApplied
	}
	target := fact.Entity
	if fact.ID != "" {
		target = fmt.Sprintf("%s id=%s", fact.Entity, fact.ID)
	}
	if fact.Operation != "" {
		target = fmt.Sprintf("%s %s", fact.Operation, target)
	}
	return fmt.Errorf("%w: %s affected=%d", semantic, target, fact.Affected)
}
