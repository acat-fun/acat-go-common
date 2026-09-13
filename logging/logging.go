// Package logging 提供基于 log/slog 的结构化日志与 trace_id 上下文。
//
// 迁移期目标：与 Java 侧 logback 输出同源可查（同一 trace_id 贯穿 HTTP 与下游调用），
// 日志字段使用小写下划线，便于日志平台统一检索。
package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"strings"
)

type ctxKey string

const (
	traceIDKey ctxKey = "trace_id"
	// HeaderTraceID 是网关/前端透传 trace 的请求头名。
	HeaderTraceID = "X-Trace-Id"
)

// New 构造 slog.Logger；level 支持 debug/info/warn/error。
func New(level, serviceName string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	logger := slog.New(handler)
	if serviceName != "" {
		logger = logger.With(slog.String("service", serviceName))
	}
	return logger
}

func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WithTraceID 把 trace_id 写入上下文。
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// TraceID 读取上下文中的 trace_id，不存在返回空串。
func TraceID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceIDKey).(string); ok {
		return v
	}
	return ""
}

// NewTraceID 生成 16 字节十六进制 trace_id。
func NewTraceID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// 随机源不可用属基础设施异常，降级为空串由上游重新生成，不阻断请求。
		return ""
	}
	return hex.EncodeToString(buf)
}

// FromContext 返回带 trace_id 字段的 logger；无 trace_id 时返回原 logger。
func FromContext(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if logger == nil {
		logger = slog.Default()
	}
	if id := TraceID(ctx); id != "" {
		return logger.With(slog.String("trace_id", id))
	}
	return logger
}
