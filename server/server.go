// Package server 装配 HTTP 服务、健康探针与优雅停机。
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"47.108.230.93/acat-fun/acat-go-common/config"
	"47.108.230.93/acat-fun/acat-go-common/health"
	"47.108.230.93/acat-fun/acat-go-common/logging"
	"47.108.230.93/acat-fun/acat-go-common/middleware"
)

// Service 是可运行服务的最小契约。
type Service interface {
	// Name 服务名（日志与探针标识）。
	Name() string
	// Register 注册业务路由。
	Register(mux *http.ServeMux)
	// Health 返回需要纳入就绪探针的检查项。
	Health() map[string]health.CheckFunc
	// Close 释放服务持有资源。
	Close() error
}

// Run 启动服务并阻塞直到收到退出信号或服务失败。
//
// 启动顺序：装配中间件 → 注册路由与探针 → 监听 → 优雅停机。
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger, svc Service) error {
	if logger == nil {
		logger = logging.New(cfg.LogLevel, cfg.Server.Name)
	}
	mux := http.NewServeMux()

	registry := health.New()
	registry.Register("self", func(context.Context) error { return nil })
	if svc != nil {
		for name, check := range svc.Health() {
			registry.Register(name, check)
		}
	}
	mux.HandleFunc("/healthz", registry.LiveHandler())
	mux.HandleFunc("/readyz", registry.ReadyHandler())
	if svc != nil {
		svc.Register(mux)
	}

	handler := middleware.Chain(mux,
		middleware.Trace(),
		middleware.Recover(logger),
	)

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	runCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP 服务启动", "addr", addr, "service", cfg.Server.Name)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP 服务异常退出: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if closeErr := closeService(svc, logger); closeErr != nil {
			logger.Error("释放服务资源失败", "error", closeErr)
		}
		return err
	case <-runCtx.Done():
		logger.Info("收到退出信号，开始优雅停机", "timeout", cfg.Server.ShutdownTimeout.String())
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	shutdownErr := httpServer.Shutdown(shutdownCtx)
	if shutdownErr != nil {
		logger.Error("优雅停机超时，强制关闭", "error", shutdownErr)
		_ = httpServer.Close()
	}
	if err := closeService(svc, logger); err != nil {
		return err
	}
	if shutdownErr != nil {
		return fmt.Errorf("优雅停机失败: %w", shutdownErr)
	}
	logger.Info("服务已停机")
	return nil
}

func closeService(svc Service, logger *slog.Logger) error {
	if svc == nil {
		return nil
	}
	if err := svc.Close(); err != nil {
		return fmt.Errorf("关闭 %s 资源失败: %w", svc.Name(), err)
	}
	return nil
}

// ShutdownTimeout 暴露默认停机窗口，便于调用方展示。
func ShutdownTimeout() time.Duration { return config.Default().Server.ShutdownTimeout }
