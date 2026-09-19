// Package redisx 提供 Redis 客户端装配与健康检查。
package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"47.108.230.93/acat-fun/acat-go-common/config"
)

// Open 按配置创建 Redis 客户端并探活。
func Open(ctx context.Context, cfg config.RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:        cfg.Addr,
		Password:    cfg.Password,
		DB:          cfg.DB,
		PoolSize:    cfg.PoolSize,
		DialTimeout: cfg.DialTimeout,
	})
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("Redis 探活失败: %w", err)
	}
	return client, nil
}

// Ping 探活，供健康检查复用。
func Ping(ctx context.Context, client *redis.Client) error {
	if client == nil {
		return fmt.Errorf("Redis 连接未初始化")
	}
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis 探活失败: %w", err)
	}
	return nil
}
