package db

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func newHandle(t *testing.T) (*Handle, sqlmock.Sqlmock) {
	t.Helper()
	raw, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("构造 sqlmock 失败: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return NewHandle(raw), mock
}

// TestWithinCommitsAndSharesTransaction 验证：成功路径提交，且 fn 内的语句走同一事务。
func TestWithinCommitsAndSharesTransaction(t *testing.T) {
	handle, mock := newHandle(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO t_a").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE t_b").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := handle.Within(context.Background(), func(ctx context.Context) error {
		if !InTransaction(ctx) {
			t.Fatal("fn 内应处于事务中")
		}
		if FromContext(ctx) == nil {
			t.Fatal("FromContext 应返回事务")
		}
		if _, err := handle.ExecContext(ctx, "INSERT INTO t_a (id) VALUES (?)", "1"); err != nil {
			return err
		}
		_, err := handle.ExecContext(ctx, "UPDATE t_b SET x = ? WHERE id = ?", 1, "2")
		return err
	})
	if err != nil {
		t.Fatalf("Within 应成功: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 期望未满足: %v", err)
	}
}

// TestWithinRollsBackAndKeepsCause 验证：失败路径回滚，且原错误链完整保留。
func TestWithinRollsBackAndKeepsCause(t *testing.T) {
	handle, mock := newHandle(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO t_a").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectRollback()

	sentinel := errors.New("业务失败")
	err := handle.Within(context.Background(), func(ctx context.Context) error {
		if _, err := handle.ExecContext(ctx, "INSERT INTO t_a (id) VALUES (?)", "1"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("应返回原错误，实际: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 期望未满足（应回滚）: %v", err)
	}
}

// TestWithinNestedJoinsOuterTransaction 验证：嵌套调用复用外层事务，不新建、不重复提交。
func TestWithinNestedJoinsOuterTransaction(t *testing.T) {
	handle, mock := newHandle(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO outer_t").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO inner_t").WillReturnResult(sqlmock.NewResult(2, 1))
	mock.ExpectCommit()

	err := handle.Within(context.Background(), func(ctx context.Context) error {
		if _, err := handle.ExecContext(ctx, "INSERT INTO outer_t (id) VALUES (?)", "1"); err != nil {
			return err
		}
		return handle.Within(ctx, func(inner context.Context) error {
			if _, err := handle.ExecContext(inner, "INSERT INTO inner_t (id) VALUES (?)", "2"); err != nil {
				return err
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("嵌套 Within 应成功: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 期望未满足（应只有一次 Begin/Commit）: %v", err)
	}
}

// TestWithinOutsideTransactionUsesPool 验证：不在事务中时直连连接池，不开启事务。
func TestWithinOutsideTransactionUsesPool(t *testing.T) {
	handle, mock := newHandle(t)
	mock.ExpectExec("UPDATE t_a").WillReturnResult(sqlmock.NewResult(0, 1))

	if InTransaction(context.Background()) {
		t.Fatal("空 context 不应处于事务中")
	}
	if _, err := handle.ExecContext(context.Background(), "UPDATE t_a SET x = 1"); err != nil {
		t.Fatalf("事务外执行应直连连接池: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL 期望未满足: %v", err)
	}
}

// TestWithinPanicRollsBackAndRepanics 验证：panic 时回滚并继续抛出。
func TestWithinPanicRollsBackAndRepanics(t *testing.T) {
	handle, mock := newHandle(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("panic 应继续抛出")
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("SQL 期望未满足（应回滚）: %v", err)
		}
	}()
	_ = handle.Within(context.Background(), func(context.Context) error {
		panic("boom")
	})
}

// TestUninitializedHandleReturnsError 验证：未初始化句柄返回错误而不是 panic。
func TestUninitializedHandleReturnsError(t *testing.T) {
	handle := NewHandle(nil)
	if _, err := handle.ExecContext(context.Background(), "SELECT 1"); !errors.Is(err, ErrHandleUninitialized) {
		t.Fatalf("ExecContext 应返回 ErrHandleUninitialized，实际: %v", err)
	}
	if _, err := handle.QueryContext(context.Background(), "SELECT 1"); !errors.Is(err, ErrHandleUninitialized) {
		t.Fatalf("QueryContext 应返回 ErrHandleUninitialized，实际: %v", err)
	}
	var value int
	if err := handle.QueryRowContext(context.Background(), "SELECT 1").Scan(&value); !errors.Is(err, ErrHandleUninitialized) {
		t.Fatalf("QueryRowContext().Scan 应返回 ErrHandleUninitialized，实际: %v", err)
	}
	if err := handle.Within(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrHandleUninitialized) {
		t.Fatalf("Within 应返回 ErrHandleUninitialized，实际: %v", err)
	}
	if handle.Raw() != nil {
		t.Fatal("Raw 应为 nil")
	}
}

// TestWithinNilFuncRejected 验证：空事务函数的显式拒绝。
func TestWithinNilFuncRejected(t *testing.T) {
	handle, _ := newHandle(t)
	if err := handle.Within(context.Background(), nil); err == nil {
		t.Fatal("nil 事务函数应被拒绝")
	}
}
