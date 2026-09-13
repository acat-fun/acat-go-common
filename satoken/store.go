package satoken

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrNotFound 表示键不存在或已过期，对应 SaTokenDao 返回 null。
var ErrNotFound = errors.New("satoken: 键不存在")

// Store 抽象 SaTokenDao 的键值持久层。
//
// 迁移期实现选型：生产用 RedisStore（与 Java 侧共用同一批 key），
// 单元测试用 MemoryStore；接口只有 6 个方法，替换后端不影响业务代码。
type Store interface {
	// Get 读取值；不存在返回 ErrNotFound。
	Get(ctx context.Context, key string) (string, error)
	// Set 写入值；timeoutSeconds 为 NeverExpire(-1) 时永久保存，
	// 为 0 或 NotValueExpire(-2) 时不写入（与 SaTokenDao.set 语义一致）。
	Set(ctx context.Context, key, value string, timeoutSeconds int64) error
	// Delete 删除键，键不存在不报错。
	Delete(ctx context.Context, key string) error
	// Expire 返回剩余秒数：>0 有效，NeverExpire(-1) 永久，NotValueExpire(-2) 不存在。
	Expire(ctx context.Context, key string) (int64, error)
	// SetExpire 修改过期时间。
	SetExpire(ctx context.Context, key string, timeoutSeconds int64) error
	// Close 释放连接。
	Close() error
}

// Logger 是最小日志接口，避免公共库绑定具体日志实现。
type Logger interface {
	Printf(format string, args ...any)
}

// MemoryStore 是进程内 Store 实现，仅用于单元测试与单实例开发。
//
// ⚠️ 多实例部署下内存会话无法共享，生产环境必须使用 RedisStore。
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]memoryEntry
	now  func() time.Time
}

type memoryEntry struct {
	value     string
	expiresAt time.Time
	forever   bool
}

// NewMemoryStore 构造内存 Store。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string]memoryEntry{}, now: time.Now}
}

// Get 实现 Store。
func (s *MemoryStore) Get(ctx context.Context, key string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	s.mu.RLock()
	entry, ok := s.data[key]
	s.mu.RUnlock()
	if !ok {
		return "", ErrNotFound
	}
	if !entry.forever && s.now().After(entry.expiresAt) {
		s.mu.Lock()
		delete(s.data, key)
		s.mu.Unlock()
		return "", ErrNotFound
	}
	return entry.value, nil
}

// Set 实现 Store。
func (s *MemoryStore) Set(ctx context.Context, key, value string, timeoutSeconds int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if timeoutSeconds == 0 || timeoutSeconds <= NotValueExpire {
		return nil
	}
	entry := memoryEntry{value: value}
	if timeoutSeconds == NeverExpire {
		entry.forever = true
	} else {
		entry.expiresAt = s.now().Add(time.Duration(timeoutSeconds) * time.Second)
	}
	s.mu.Lock()
	s.data[key] = entry
	s.mu.Unlock()
	return nil
}

// Delete 实现 Store。
func (s *MemoryStore) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	delete(s.data, key)
	s.mu.Unlock()
	return nil
}

// Expire 实现 Store。
func (s *MemoryStore) Expire(ctx context.Context, key string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.RLock()
	entry, ok := s.data[key]
	s.mu.RUnlock()
	if !ok {
		return NotValueExpire, nil
	}
	if entry.forever {
		return NeverExpire, nil
	}
	remaining := entry.expiresAt.Sub(s.now())
	if remaining <= 0 {
		s.mu.Lock()
		delete(s.data, key)
		s.mu.Unlock()
		return NotValueExpire, nil
	}
	return int64(remaining.Seconds()), nil
}

// SetExpire 实现 Store。
func (s *MemoryStore) SetExpire(ctx context.Context, key string, timeoutSeconds int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.data[key]
	if !ok {
		return nil
	}
	if timeoutSeconds == NeverExpire {
		entry.forever = true
		entry.expiresAt = time.Time{}
	} else if timeoutSeconds > 0 {
		entry.forever = false
		entry.expiresAt = s.now().Add(time.Duration(timeoutSeconds) * time.Second)
	}
	s.data[key] = entry
	return nil
}

// Close 实现 Store。
func (s *MemoryStore) Close() error { return nil }

// Keys 返回当前全部键，仅用于测试与兼容性自检。
func (s *MemoryStore) Keys() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.data))
	for k := range s.data {
		out = append(out, k)
	}
	return out
}

// expireToTimeout 把剩余秒数转换为写入时使用的 timeout 语义。
func expireToTimeout(expireSeconds int64) (int64, error) {
	switch {
	case expireSeconds == NeverExpire:
		return NeverExpire, nil
	case expireSeconds == NotValueExpire:
		return 0, fmt.Errorf("satoken: 目标键不存在，无法续期")
	case expireSeconds > 0:
		return expireSeconds, nil
	default:
		return 0, fmt.Errorf("satoken: 非法的剩余时间 %d", expireSeconds)
	}
}
