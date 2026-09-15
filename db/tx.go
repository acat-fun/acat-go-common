package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
)

// ErrHandleUninitialized 表示数据库句柄未初始化（NewHandle(nil) 或零值 Handle）。
var ErrHandleUninitialized = errors.New("db: 数据库句柄未初始化")

// Tx 是「事务内可用的数据库能力子集」，`*sql.Tx` 与 `*Handle` 都满足它。
//
// 各业务服务的 repo 层已各自定义了同形状的 DBTX 接口（ExecContext/QueryContext/
// QueryRowContext），因此这里保持同一形状：repo 无需改动即可同时接受连接池与事务。
type Tx interface {
	// ExecContext 执行不返回结果集的语句。
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	// QueryContext 执行返回结果集的查询。
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	// QueryRowContext 执行返回单行的查询。
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// txContextKey 是事务在 context 中的键（未导出，禁止外部构造）。
type txContextKey struct{}

// Handle 是**事务感知**的数据库句柄：
//   - 不在事务中时，三个方法直连底层连接池；
//   - 处于 Within 打开的事务中时，自动复用 ctx 里的事务。
//
// 这样 repo 层不必写「取事务还是取连接池」的分支，事务边界完全由 service 层通过
// ctx 决定（对应规范 §8.7「事务由 Application Service 开启并传递 context.Context，
// Repository 不得隐式创建独立事务」）。
type Handle struct {
	raw *sql.DB
}

// NewHandle 包装连接池；raw 为 nil 时返回的句柄所有操作返回 ErrHandleUninitialized（不 panic）。
func NewHandle(raw *sql.DB) *Handle { return &Handle{raw: raw} }

// Raw 返回底层连接池（健康检查、连接池指标等场景使用）。
func (h *Handle) Raw() *sql.DB {
	if h == nil {
		return nil
	}
	return h.raw
}

// ExecContext 实现 Tx：优先复用 ctx 中的事务。
func (h *Handle) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if tx := FromContext(ctx); tx != nil {
		return tx.ExecContext(ctx, query, args...)
	}
	if h == nil || h.raw == nil {
		return nil, ErrHandleUninitialized
	}
	return h.raw.ExecContext(ctx, query, args...)
}

// QueryContext 实现 Tx：优先复用 ctx 中的事务。
func (h *Handle) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if tx := FromContext(ctx); tx != nil {
		return tx.QueryContext(ctx, query, args...)
	}
	if h == nil || h.raw == nil {
		return nil, ErrHandleUninitialized
	}
	return h.raw.QueryContext(ctx, query, args...)
}

// QueryRowContext 实现 Tx：优先复用 ctx 中的事务。
//
// 未初始化句柄返回「Scan 时报 ErrHandleUninitialized」的 Row（sql.Row 无法直接表达错误）。
func (h *Handle) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if tx := FromContext(ctx); tx != nil {
		return tx.QueryRowContext(ctx, query, args...)
	}
	if h == nil || h.raw == nil {
		return uninitializedDB.QueryRowContext(ctx, query, args...)
	}
	return h.raw.QueryRowContext(ctx, query, args...)
}

// FromContext 返回 ctx 中正在进行的事务；不在事务中返回 nil。
func FromContext(ctx context.Context) Tx {
	if ctx == nil {
		return nil
	}
	tx, ok := ctx.Value(txContextKey{}).(Tx)
	if !ok {
		return nil
	}
	return tx
}

// InTransaction 报告 ctx 是否处于事务中。
func InTransaction(ctx context.Context) bool { return FromContext(ctx) != nil }

// Within 在一个数据库事务中执行 fn，并把事务放进传入 fn 的 ctx：
//
//	err := handle.Within(ctx, func(ctx context.Context) error {
//	    if err := books.Update(ctx, book); err != nil { return err }
//	    return approvals.InsertResult(ctx, result) // 同一事务
//	})
//
// 语义：
//   - fn 返回 nil → 提交；返回错误 → 回滚，并把原错误（保留 cause）返回；
//   - fn panic → 回滚后继续 panic（不吞异常）；
//   - **嵌套调用加入外层事务**（不新建、不使用 savepoint）：内层返回错误会让外层整体回滚；
//   - 回滚本身失败时会与业务错误一起返回（errors.Join），不静默丢弃。
func (h *Handle) Within(ctx context.Context, fn func(ctx context.Context) error) error {
	if fn == nil {
		return errors.New("db: Within 需要非空的事务函数")
	}
	if InTransaction(ctx) {
		// 已在事务中：直接执行，保证同一 ctx 下的多次调用共用一个事务。
		return fn(ctx)
	}
	if h == nil || h.raw == nil {
		return fmt.Errorf("开启事务失败: %w", ErrHandleUninitialized)
	}
	tx, err := h.raw.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败: %w", err)
	}
	txCtx := context.WithValue(ctx, txContextKey{}, Tx(tx))

	var panicValue any
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				panicValue = recovered
			}
		}()
		err = fn(txCtx)
	}()

	if panicValue != nil {
		_ = tx.Rollback()
		panic(panicValue)
	}
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return errors.Join(err, fmt.Errorf("事务回滚失败: %w", rollbackErr))
		}
		return err
	}
	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("提交事务失败: %w", commitErr)
	}
	return nil
}

// ---- 未初始化句柄的错误载体（让 QueryRowContext().Scan 返回错误而不是 panic）----

// errConnector 是一个永远连接失败的 driver.Connector。
type errConnector struct{ err error }

// Connect 恒失败。
func (c errConnector) Connect(context.Context) (driver.Conn, error) { return nil, c.err }

// Driver 返回配套 driver。
func (c errConnector) Driver() driver.Driver { return errDriver{err: c.err} }

// errDriver 是一个永远打开失败的 driver.Driver。
type errDriver struct{ err error }

// Open 恒失败。
func (d errDriver) Open(string) (driver.Conn, error) { return nil, d.err }

// uninitializedDB 只在句柄未初始化时被使用，保证返回错误而不是 panic。
var uninitializedDB = sql.OpenDB(errConnector{err: ErrHandleUninitialized})
