// Package db 提供 MySQL 连接池装配与健康检查。
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql" // 注册 mysql driver

	"gitea.acat.fun/acat-fun/acat-go-common/config"
)

// Open 按配置创建 MySQL 连接池并探活。
func Open(ctx context.Context, cfg config.DatabaseConfig) (*sql.DB, error) {
	handle, err := sql.Open("mysql", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("打开 MySQL 连接失败: %w", err)
	}
	handle.SetMaxOpenConns(cfg.MaxOpenConns)
	handle.SetMaxIdleConns(cfg.MaxIdleConns)
	if cfg.ConnMaxLifetime > 0 {
		handle.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime > 0 {
		handle.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := handle.PingContext(pingCtx); err != nil {
		_ = handle.Close()
		return nil, fmt.Errorf("MySQL 探活失败: %w", err)
	}
	return handle, nil
}

// Ping 探活，供健康检查复用。
func Ping(ctx context.Context, handle *sql.DB) error {
	if handle == nil {
		return fmt.Errorf("MySQL 连接未初始化")
	}
	if err := handle.PingContext(ctx); err != nil {
		return fmt.Errorf("MySQL 探活失败: %w", err)
	}
	return nil
}
