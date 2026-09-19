// Package mysqlx 收纳 20 个 Go 服务各自复制的「MySQL / database-sql 机制代码」：
// 可空列与指针互转、MySQL 约束冲突识别、SQL 片段生成、MyBatis-Plus 写语义样板。
//
// 定位（规范 §8.6「公共包只抽取机制，不抽业务」）：
//   - 本包只做**结果转换与语句拼装**，不含任何业务判断，也不认识任何业务表；
//   - 0 行影响如何解释由 Application Service 决定（见 db.RequireUpdated / db.WarnIfUnaffected）；
//   - 表名、字段清单、WHERE 条件一律由调用方传入。
//
// 这些函数此前在 14 个服务的 internal/repo 里各存一份（nullString ×14、IsIntegrityViolation ×11 …），
// 修复一处要改 14 遍，故统一上收。
package mysqlx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"
)

// DBTX 是 *sql.DB 与 *sql.Tx 的公共能力子集。
//
// 各服务的 repo 层都定义过同形状接口，Go 接口是结构化的，因此既有的本地 DBTX
// 与 acat-go-common/db.Handle 都可以直接传进来，无需类型断言。
type DBTX interface {
	// ExecContext 执行不返回结果集的语句。
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	// QueryContext 执行返回结果集的查询。
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	// QueryRowContext 执行返回单行的查询。
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Scanner 抽象 *sql.Row 与 *sql.Rows 的 Scan。
type Scanner interface {
	// Scan 把当前行读入目标。
	Scan(dest ...any) error
}

// ---- 可空列 → 指针（NULL → nil

// NullString 把可空字符串列转换为 *string。
func NullString(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

// NullInt 把可空整数列转换为 *int。
func NullInt(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int64)
	return &converted
}

// NullInt64 把可空整数列转换为 *int64。
func NullInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	return &value.Int64
}

// NullFloat 把可空浮点列转换为 *float64。
func NullFloat(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

// NullBool 把可空布尔列转换为 *bool（MySQL tinyint(1)）。
func NullBool(value sql.NullBool) *bool {
	if !value.Valid {
		return nil
	}
	return &value.Bool
}

// NullTime 把可空时间列转换为 *time.Time。
func NullTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

// ---- 指针 → SQL 参数（nil → NULL）----

// Arg 把 *string 转换为 SQL 参数。
//
// 注意：必须显式判空，不能直接把指针放进 any——typed nil 装进接口后与 nil 不相等，
// 会被 MyBatis-Plus NOT_NULL 语义判成「有值」而写进 SQL。
func Arg(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// IntArg 把 *int 转换为 SQL 参数。
func IntArg(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

// Int64Arg 把 *int64 转换为 SQL 参数。
func Int64Arg(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

// FloatArg 把 *float64 转换为 SQL 参数。
func FloatArg(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

// BoolArg 把 *bool 转换为 SQL 参数。
func BoolArg(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

// TimeArg 把 *time.Time 转换为 SQL 参数。
func TimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

// ---- MySQL 约束冲突识别 ----

// IsIntegrityViolation 判断错误链中是否包含数据库约束冲突
// （唯一键 1062 / 非空 1048 / 缺列无默认值 1364 / 外键 1216/1217/1451/1452 等）。
//
// 服务层据此映射为 apperr.Internal。
func IsIntegrityViolation(err error) (*mysqlDriver.MySQLError, bool) {
	var mysqlErr *mysqlDriver.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case 1048, // ER_BAD_NULL_ERROR
			1062, // ER_DUP_ENTRY
			1364, // ER_NO_DEFAULT_FOR_FIELD
			1216, // ER_NO_REFERENCED_ROW
			1217, // ER_ROW_IS_REFERENCED
			1451, // ER_ROW_IS_REFERENCED_2
			1452: // ER_NO_REFERENCED_ROW_2
			return mysqlErr, true
		}
	}
	return nil, false
}

// WrapNoRows 把 sql.ErrNoRows 归一化为调用方给定的「未找到」错误，其余错误按 action 包装。
//
// 各服务 repo 的 wrapError 同款语义：`errors.Is(err, sql.ErrNoRows) → notFound`。
func WrapNoRows(err error, notFound error, action string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return notFound
	}
	return fmt.Errorf("%s: %w", action, err)
}

// ---- SQL 片段 ----

// Placeholders 生成 n 个 `?, ?, ?`（逗号后带空格，n <= 0 返回空串）。
//
// 保留多数服务既有 SQL 文本形态（与各服务测试断言、双跑 SQL 日志一致），
// MySQL 语义与无空格变体等价，但文本不漂移。
func Placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

// PlaceholdersCompact 生成 n 个 `?,?,?`（无空格）。
//
// 个别服务（acat-read-admin-operation 等）的既有 SQL 文本使用无空格形态，迁移后保持原样。
func PlaceholdersCompact(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// AppendLimit 按 MyBatis-Plus 语义追加分页片段：limit < 0 时不加 LIMIT。
func AppendLimit(query string, limit, offset int) string {
	if limit < 0 {
		return query
	}
	return query + " LIMIT ? OFFSET ?"
}

// AppendLimitArgs 是 AppendLimit 的旧服务形态（部分服务按 `query, limitArgs := ...` 用法）：
// limit < 0 时返回 (query, nil)，否则返回带 LIMIT 的查询与 (limit, offset) 参数。
func AppendLimitArgs(query string, limit, offset int) (string, []any) {
	if limit < 0 {
		return query, nil
	}
	return query + " LIMIT ? OFFSET ?", []any{limit, offset}
}

// AppendLimitComma 复刻个别服务（acat-read-app-comic 等）的旧形态：
// 用 MySQL 兼容的 `LIMIT offset, count` 逗号语法；offset == 0 时简化为 `LIMIT count`。
func AppendLimitComma(query string, limit, offset int) (string, []any) {
	if limit < 0 {
		return query, nil
	}
	if offset == 0 {
		return query + " LIMIT ?", []any{limit}
	}
	return query + " LIMIT ?, ?", []any{offset, limit}
}

// ---- MyBatis-Plus 写语义样板 ----

// Field 是一列（列名 + 值）；值为 nil 时按 MP NOT_NULL 策略从 SQL 中省略。
type Field struct {
	// Column 列名。
	Column string
	// Value SQL 参数；nil 表示不参与写（NOT_NULL 策略）。
	Value any
}

// Insert 复刻 MyBatis-Plus `insert(entity)`（字段策略 NOT_NULL：null 列不出现在 INSERT 中）。
func Insert(ctx context.Context, tx DBTX, table string, fields []Field) error {
	columns := make([]string, 0, len(fields))
	marks := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields))
	for _, item := range fields {
		if item.Value == nil {
			continue
		}
		columns = append(columns, item.Column)
		marks = append(marks, "?")
		args = append(args, item.Value)
	}
	if len(columns) == 0 {
		return fmt.Errorf("mysqlx.Insert: %s 没有可写入的列", table)
	}
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(columns, ", "), strings.Join(marks, ", "))
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("插入 %s 失败: %w", table, err)
	}
	return nil
}

// Update 复刻 MyBatis-Plus `updateById(entity)`：
//   - SET 只包含非 null 列（NOT_NULL 更新策略），`is_deleted` 永不进入 SET（逻辑删除列）；
//   - version 非 nil 时复刻 OptimisticLockerInnerInterceptor：SET 追加 `version = version + 1`，
//     WHERE 追加 `version = ?`；
//   - WHERE 追加逻辑删除条件 `is_deleted = 0`（@TableLogic）。
func Update(ctx context.Context, tx DBTX, table, id string, fields []Field, version *int) (int64, error) {
	sets := make([]string, 0, len(fields)+1)
	args := make([]any, 0, len(fields)+2)
	for _, item := range fields {
		if item.Value == nil || item.Column == "id" || item.Column == "is_deleted" {
			continue
		}
		sets = append(sets, item.Column+" = ?")
		args = append(args, item.Value)
	}
	if version != nil {
		sets = append(sets, "version = version + 1")
	}
	if len(sets) == 0 {
		return 0, nil
	}
	query := fmt.Sprintf("UPDATE %s SET %s WHERE id = ? AND is_deleted = 0", table, strings.Join(sets, ", "))
	args = append(args, id)
	if version != nil {
		query += " AND version = ?"
		args = append(args, *version)
	}
	return ExecAffected(ctx, tx, query, args...)
}

// UpdateColumn 复刻「单列显式写」（含把列显式写成 NULL 的场景，例如解绑）。
func UpdateColumn(ctx context.Context, tx DBTX, table, id, column string, value any) (int64, error) {
	query := fmt.Sprintf("UPDATE %s SET %s = ? WHERE id = ? AND is_deleted = 0", table, column)
	return ExecAffected(ctx, tx, query, value, id)
}

// Delete 复刻 MyBatis-Plus `deleteById(id)` 的逻辑删除语句。
func Delete(ctx context.Context, tx DBTX, table, id string) (int64, error) {
	query := fmt.Sprintf("UPDATE %s SET is_deleted = 1 WHERE id = ? AND is_deleted = 0", table)
	return ExecAffected(ctx, tx, query, id)
}

// ExecAffected 执行写语句并返回影响行数（规范 §8.8.1：Repository 只报事实）。
func ExecAffected(ctx context.Context, tx DBTX, query string, args ...any) (int64, error) {
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("读取影响行数失败: %w", err)
	}
	return affected, nil
}

// CountRows 执行 count 查询并返回总数。
func CountRows(ctx context.Context, tx DBTX, query string, args ...any) (int64, error) {
	var total int64
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}
