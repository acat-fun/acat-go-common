// Package health 提供存活/就绪探针注册表。
//
// 与 Java 侧 actuator/health 对应：/healthz 为存活探针（进程可用即 200），
// /readyz 为就绪探针（全部依赖检查通过才 200，否则 503）。
package health

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"
)

// CheckFunc 是单个依赖的检查函数。
type CheckFunc func(ctx context.Context) error

// Registry 维护命名检查项。
type Registry struct {
	mu     sync.RWMutex
	checks map[string]CheckFunc
	// timeout 单次检查超时，默认 3s。
	timeout time.Duration
}

// New 构造探针注册表。
func New() *Registry {
	return &Registry{checks: map[string]CheckFunc{}, timeout: 3 * time.Second}
}

// Register 注册命名检查项。
func (r *Registry) Register(name string, fn CheckFunc) {
	if name == "" || fn == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks[name] = fn
}

type statusPayload struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// LiveHandler 返回存活探针处理器。
func (r *Registry) LiveHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, statusPayload{Status: "UP"})
	}
}

// ReadyHandler 返回就绪探针处理器。
func (r *Registry) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ctx, cancel := context.WithTimeout(req.Context(), r.timeout)
		defer cancel()

		r.mu.RLock()
		names := make([]string, 0, len(r.checks))
		for name := range r.checks {
			names = append(names, name)
		}
		sort.Strings(names)
		checks := make(map[string]CheckFunc, len(r.checks))
		for name, fn := range r.checks {
			checks[name] = fn
		}
		r.mu.RUnlock()

		results := make(map[string]string, len(names))
		healthy := true
		for _, name := range names {
			if err := checks[name](ctx); err != nil {
				results[name] = err.Error()
				healthy = false
				continue
			}
			results[name] = "UP"
		}
		payload := statusPayload{Status: "UP", Checks: results}
		if !healthy {
			payload.Status = "DOWN"
			writeJSON(w, http.StatusServiceUnavailable, payload)
			return
		}
		writeJSON(w, http.StatusOK, payload)
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json;charset=UTF-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
