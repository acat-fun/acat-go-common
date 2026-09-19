package satoken

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore 是基于 go-redis 的 Store 实现
// 同一 Redis 实例与同一批 key，实现双实现共享登录态。
type RedisStore struct {
	client *redis.Client
	// timeout 单次命令超时，默认 3s。
	timeout time.Duration
}

// RedisStoreOption 配置 RedisStore。
type RedisStoreOption func(*RedisStore)

// WithRedisTimeout 设置单次命令超时。
func WithRedisTimeout(d time.Duration) RedisStoreOption {
	return func(s *RedisStore) {
		if d > 0 {
			s.timeout = d
		}
	}
}

// NewRedisStore 构造 Redis Store。
func NewRedisStore(client *redis.Client, opts ...RedisStoreOption) *RedisStore {
	store := &RedisStore{client: client, timeout: 3 * time.Second}
	for _, opt := range opts {
		opt(store)
	}
	return store
}

func (s *RedisStore) ctx(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, s.timeout)
}

// Get 实现 Store。
func (s *RedisStore) Get(ctx context.Context, key string) (string, error) {
	c, cancel := s.ctx(ctx)
	defer cancel()
	value, err := s.client.Get(c, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("satoken: redis get %s 失败: %w", key, err)
	}
	return value, nil
}

// Set 实现 Store。
func (s *RedisStore) Set(ctx context.Context, key, value string, timeoutSeconds int64) error {
	if timeoutSeconds == 0 || timeoutSeconds <= NotValueExpire {
		return nil
	}
	c, cancel := s.ctx(ctx)
	defer cancel()
	var err error
	if timeoutSeconds == NeverExpire {
		err = s.client.Set(c, key, value, 0).Err()
	} else {
		err = s.client.Set(c, key, value, time.Duration(timeoutSeconds)*time.Second).Err()
	}
	if err != nil {
		return fmt.Errorf("satoken: redis set %s 失败: %w", key, err)
	}
	return nil
}

// Delete 实现 Store。
func (s *RedisStore) Delete(ctx context.Context, key string) error {
	c, cancel := s.ctx(ctx)
	defer cancel()
	if err := s.client.Del(c, key).Err(); err != nil {
		return fmt.Errorf("satoken: redis del %s 失败: %w", key, err)
	}
	return nil
}

// Expire 实现 Store。
func (s *RedisStore) Expire(ctx context.Context, key string) (int64, error) {
	c, cancel := s.ctx(ctx)
	defer cancel()
	duration, err := s.client.TTL(c, key).Result()
	if err != nil {
		return 0, fmt.Errorf("satoken: redis ttl %s 失败: %w", key, err)
	}
	// go-redis 用 -1ns / -2ns 表达"无过期时间 / key 不存在"，统一转换为秒语义。
	switch {
	case duration == -2*time.Nanosecond:
		return NotValueExpire, nil
	case duration == -1*time.Nanosecond, duration == -1:
		return NeverExpire, nil
	case duration <= 0:
		return NotValueExpire, nil
	default:
		return int64(duration.Seconds()), nil
	}
}

// SetExpire 实现 Store。
func (s *RedisStore) SetExpire(ctx context.Context, key string, timeoutSeconds int64) error {
	c, cancel := s.ctx(ctx)
	defer cancel()
	if timeoutSeconds == NeverExpire {
		// 与 SaTokenDaoForRedisTemplate.updateTimeout 一致：先读值再无限期写回。
		value, err := s.Get(ctx, key)
		if err != nil {
			return err
		}
		return s.Set(ctx, key, value, NeverExpire)
	}
	if timeoutSeconds <= 0 {
		return nil
	}
	if err := s.client.Expire(c, key, time.Duration(timeoutSeconds)*time.Second).Err(); err != nil {
		return fmt.Errorf("satoken: redis expire %s 失败: %w", key, err)
	}
	return nil
}

// Close 实现 Store。
func (s *RedisStore) Close() error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

// Ping 检查 Redis 连通性，供健康探针使用。
func (s *RedisStore) Ping(ctx context.Context) error {
	c, cancel := s.ctx(ctx)
	defer cancel()
	if err := s.client.Ping(c).Err(); err != nil {
		return fmt.Errorf("satoken: redis ping 失败: %w", err)
	}
	return nil
}

// ScanKeys 扫描匹配前缀的 key，仅用于兼容性自检（生产路径不调用）。
func (s *RedisStore) ScanKeys(ctx context.Context, pattern string, limit int) ([]string, error) {
	c, cancel := s.ctx(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 100
	}
	var (
		cursor uint64
		out    []string
	)
	for {
		keys, next, err := s.client.Scan(c, cursor, pattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("satoken: redis scan %s 失败: %w", pattern, err)
		}
		out = append(out, keys...)
		if next == 0 || len(out) >= limit {
			break
		}
		cursor = next
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
