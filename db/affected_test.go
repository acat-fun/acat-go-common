package db

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// TestRequireUpdatedReturnsSemanticError 严格模式：0 行必须返回调用方给定的语义错误，
// 且 errors.Is 可识别（不是字符串比较）。
func TestRequireUpdatedReturnsSemanticError(t *testing.T) {
	sentinel := errors.New("并发冲突")
	fact := WriteFact{Operation: "decideApproval", Entity: "t_order", ID: "a1", Affected: 0}

	err := RequireUpdated(fact, sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatalf("应返回调用方给定的语义错误，实际: %v", err)
	}
	if !strings.Contains(err.Error(), "t_order") || !strings.Contains(err.Error(), "a1") {
		t.Fatalf("错误应包含定位信息，实际: %v", err)
	}
	if err := RequireUpdated(WriteFact{Affected: 1}, sentinel); err != nil {
		t.Fatalf("命中时不应报错: %v", err)
	}
}

// TestRequireAffectedFallsBackToSentinel 调用方未给定语义错误时使用兜底哨兵，避免 0 行被当成成功。
func TestRequireAffectedFallsBackToSentinel(t *testing.T) {
	if err := RequireAffected(WriteFact{Affected: 0}, nil); !errors.Is(err, ErrWriteNotApplied) {
		t.Fatalf("应返回 ErrWriteNotApplied，实际: %v", err)
	}
	if err := RequireAffected(WriteFact{Affected: 2}, nil); err != nil {
		t.Fatalf("命中时不应报错: %v", err)
	}
}

// TestWarnIfUnaffectedLogsStructuredFact 兼容模式：0 行不改结果，但必须留下结构化证据。
func TestWarnIfUnaffectedLogsStructuredFact(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelWarn}))
	version := 7
	WarnIfUnaffected(logger, WriteFact{
		Service: "acat-read-admin-book", Operation: "updateBook",
		Entity: "acat_read.t_read_book", ID: "b1", ExpectedVersion: &version, Affected: 0,
	})

	logged := buffer.String()
	for _, want := range []string{
		"updateBook", "acat_read.t_read_book", "b1",
		"affected_rows=0", "expected_version=7", "compatibility_mode=true",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("日志缺少字段 %q：%s", want, logged)
		}
	}
}

// TestWarnIfUnaffectedSilentOnHit 命中时不产生日志（避免正常路径刷日志）。
func TestWarnIfUnaffectedSilentOnHit(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelWarn}))
	WarnIfUnaffected(logger, WriteFact{Operation: "updateBook", Affected: 1})
	if buffer.Len() != 0 {
		t.Fatalf("命中时不应有日志: %s", buffer.String())
	}
	// nil logger 不得 panic。
	WarnIfUnaffected(nil, WriteFact{Affected: 0})
}
