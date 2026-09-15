package mysqlx

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	mysqlDriver "github.com/go-sql-driver/mysql"
)

func TestNullConversions(t *testing.T) {
	cases := []struct {
		name string
		got  any
		want any
	}{
		{"NullString 有值", NullString(sql.NullString{String: "abc", Valid: true}), ptr("abc")},
		{"NullString NULL", NullString(sql.NullString{}), (*string)(nil)},
		{"NullInt 有值", NullInt(sql.NullInt64{Int64: 7, Valid: true}), ptrInt(7)},
		{"NullInt NULL", NullInt(sql.NullInt64{}), (*int)(nil)},
		{"NullInt64 有值", NullInt64(sql.NullInt64{Int64: 7, Valid: true}), ptrInt64(7)},
		{"NullFloat 有值", NullFloat(sql.NullFloat64{Float64: 1.5, Valid: true}), ptrFloat(1.5)},
		{"NullBool 有值", NullBool(sql.NullBool{Bool: true, Valid: true}), ptrBool(true)},
		{"NullBool NULL", NullBool(sql.NullBool{}), (*bool)(nil)},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			switch want := item.want.(type) {
			case *string:
				got, ok := item.got.(*string)
				if !ok || (want == nil) != (got == nil) || (want != nil && *want != *got) {
					t.Fatalf("得到 %v，期望 %v", item.got, item.want)
				}
			case *int:
				got, ok := item.got.(*int)
				if !ok || (want == nil) != (got == nil) || (want != nil && *want != *got) {
					t.Fatalf("得到 %v，期望 %v", item.got, item.want)
				}
			case *int64:
				got, ok := item.got.(*int64)
				if !ok || (want == nil) != (got == nil) || (want != nil && *want != *got) {
					t.Fatalf("得到 %v，期望 %v", item.got, item.want)
				}
			case *float64:
				got, ok := item.got.(*float64)
				if !ok || (want == nil) != (got == nil) || (want != nil && *want != *got) {
					t.Fatalf("得到 %v，期望 %v", item.got, item.want)
				}
			case *bool:
				got, ok := item.got.(*bool)
				if !ok || (want == nil) != (got == nil) || (want != nil && *want != *got) {
					t.Fatalf("得到 %v，期望 %v", item.got, item.want)
				}
			}
		})
	}
	if NullTime(sql.NullTime{}) != nil {
		t.Fatal("NullTime(NULL) 应为 nil")
	}
}

// TestArgHelpersRejectTypedNil 指针转参数必须把 typed nil 变成真正的 NULL。
func TestArgHelpersRejectTypedNil(t *testing.T) {
	var (
		text   *string
		number *int
		big    *int64
		real   *float64
		flag   *bool
	)
	for name, got := range map[string]any{
		"Arg":      Arg(text),
		"IntArg":   IntArg(number),
		"Int64Arg": Int64Arg(big),
		"FloatArg": FloatArg(real),
		"BoolArg":  BoolArg(flag),
	} {
		if got != nil {
			t.Fatalf("%s(nil) 应为 nil，实际 %#v", name, got)
		}
	}
	value := "x"
	if Arg(&value) != "x" {
		t.Fatal("Arg 应解引用")
	}
}

func TestIsIntegrityViolation(t *testing.T) {
	cases := []struct {
		number uint16
		want   bool
	}{
		{1062, true}, // 唯一键
		{1048, true}, // 非空
		{1451, true}, // 外键
		{1146, false},
		{0, false},
	}
	for _, item := range cases {
		err := &mysqlDriver.MySQLError{Number: item.number, Message: "x"}
		_, ok := IsIntegrityViolation(err)
		if ok != item.want {
			t.Fatalf("%d 判定应为 %v", item.number, item.want)
		}
		// 包装后仍可识别（错误链遍历）。
		if _, ok := IsIntegrityViolation(errors.Join(errors.New("外层"), err)); ok != item.want {
			t.Fatalf("%d 经包装后判定应为 %v", item.number, item.want)
		}
	}
	if _, ok := IsIntegrityViolation(errors.New("普通错误")); ok {
		t.Fatal("普通错误不应判为约束冲突")
	}
}

func TestWrapNoRows(t *testing.T) {
	notFound := errors.New("记录不存在")
	if got := WrapNoRows(sql.ErrNoRows, notFound, "查询失败"); !errors.Is(got, notFound) {
		t.Fatalf("ErrNoRows 应归一化为 notFound: %v", got)
	}
	cause := errors.New("连接断开")
	got := WrapNoRows(cause, notFound, "查询失败")
	if !errors.Is(got, cause) || !strings.Contains(got.Error(), "查询失败") {
		t.Fatalf("其它错误应按 action 包装并保留 cause: %v", got)
	}
	if WrapNoRows(nil, notFound, "查询失败") != nil {
		t.Fatal("nil 应返回 nil")
	}
}

func TestPlaceholdersAndAppendLimit(t *testing.T) {
	if Placeholders(3) != "?, ?, ?" || Placeholders(1) != "?" || Placeholders(0) != "" {
		t.Fatalf("Placeholders 结果异常: %q %q %q", Placeholders(3), Placeholders(1), Placeholders(0))
	}
	if PlaceholdersCompact(3) != "?,?,?" || PlaceholdersCompact(1) != "?" {
		t.Fatalf("PlaceholdersCompact 结果异常: %q %q", PlaceholdersCompact(3), PlaceholdersCompact(1))
	}
	if got := AppendLimit("SELECT 1", 10, 20); got != "SELECT 1 LIMIT ? OFFSET ?" {
		t.Fatalf("limit>=0 应追加分页: %q", got)
	}
	if got := AppendLimit("SELECT 1", -1, 20); got != "SELECT 1" {
		t.Fatalf("limit<0 不应追加分页: %q", got)
	}
}

func TestInsertOmitsNullFields(t *testing.T) {
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	defer func() { _ = raw.Close() }()

	value := "标题"
	mock.ExpectExec(regexpQuote("INSERT INTO t_demo (id, title) VALUES (?, ?)")).
		WithArgs("1", "标题").WillReturnResult(sqlmock.NewResult(1, 1))

	fields := []Field{
		{Column: "id", Value: "1"},
		{Column: "title", Value: Arg(&value)},
		{Column: "subtitle", Value: Arg(nil)},
	}
	if err := Insert(context.Background(), raw, "t_demo", fields); err != nil {
		t.Fatalf("插入失败: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 期望未满足（null 列不应出现）: %v", err)
	}
}

func TestUpdateAppliesOptimisticLockAndSkipsLogicDeleteColumn(t *testing.T) {
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	defer func() { _ = raw.Close() }()

	version := 3
	mock.ExpectExec(regexpQuote(
		"UPDATE t_demo SET name = ?, version = version + 1 WHERE id = ? AND is_deleted = 0 AND version = ?")).
		WithArgs("新名", "1", 3).WillReturnResult(sqlmock.NewResult(0, 1))

	fields := []Field{
		{Column: "id", Value: "1"},
		{Column: "name", Value: "新名"},
		{Column: "is_deleted", Value: 0},
	}
	affected, err := Update(context.Background(), raw, "t_demo", "1", fields, &version)
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if affected != 1 {
		t.Fatalf("影响行数应为 1，实际 %d", affected)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 期望未满足: %v", err)
	}
}

func TestExecAffectedAndDelete(t *testing.T) {
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	defer func() { _ = raw.Close() }()

	mock.ExpectExec(regexpQuote("UPDATE t_demo SET is_deleted = 1 WHERE id = ? AND is_deleted = 0")).
		WithArgs("1").WillReturnResult(sqlmock.NewResult(0, 0))
	affected, err := Delete(context.Background(), raw, "t_demo", "1")
	if err != nil {
		t.Fatalf("逻辑删除失败: %v", err)
	}
	if affected != 0 {
		t.Fatalf("0 行应如实返回，实际 %d", affected)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 期望未满足: %v", err)
	}
}

func TestCountRows(t *testing.T) {
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	defer func() { _ = raw.Close() }()

	mock.ExpectQuery(regexpQuote("SELECT COUNT(*) FROM t_demo WHERE x = ?")).
		WithArgs(1).WillReturnRows(sqlmock.NewRows([]string{"c"}).AddRow(42))
	total, err := CountRows(context.Background(), raw, "SELECT COUNT(*) FROM t_demo WHERE x = ?", 1)
	if err != nil || total != 42 {
		t.Fatalf("计数异常: %d %v", total, err)
	}
}

func ptr(value string) *string        { return &value }
func ptrInt(value int) *int           { return &value }
func ptrInt64(value int64) *int64     { return &value }
func ptrFloat(value float64) *float64 { return &value }
func ptrBool(value bool) *bool        { return &value }

// regexpQuote 让期望 SQL 按字面量匹配（避免正则元字符干扰）。
func regexpQuote(query string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\", ".", "\\.", "+", "\\+", "*", "\\*", "?", "\\?",
		"(", "\\(", ")", "\\)", "[", "\\[", "]", "\\]", "{", "\\{", "}", "\\}",
		"^", "\\^", "$", "\\$", "|", "\\|",
	)
	return replacer.Replace(query)
}
