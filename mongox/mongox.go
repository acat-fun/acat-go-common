// Package mongox 提供 MySQL+MongoDB 跨存储一致性机制（后端整改遗留事项 2，2026-09-19）。
//
// 背景：acat-read-app-author（章节正文）与 acat-read-admin-book（书籍元数据）的
// 写用例同时落 MySQL 与 MongoDB。事务机制（db.Handle）只能保证「Mongo 写失败 →
// MySQL 回滚」；事务提交后 Mongo 侧仍可能丢失（进程崩溃、网络分区、Mongo 主从切换）。
//
// 方案（规范 §8.7「本地事务 + Outbox + 可重试消费者」，复用 t_read_search_outbox
// 的成熟模式）：
//
//   - 业务写用例在**同一 MySQL 事务**内追加 outbox 行（EventStore.Enqueue），
//     payload 携带完整文档 JSON，重放不依赖再次读库；
//   - 后台任务（Replayer）轮询 pending 行并重放 Mongo 写入（DocApplier），
//     成功标记已同步，失败累计 attempts 并保留 last_error；
//   - 重放幂等：SAVE → 按 _id upsert 整文档；DELETE → 按 _id 删除（不存在即成功）。
//
// 本包只提供机制（表名、轮询、重试、状态机）；聚合类型、文档结构与应用逻辑由
// 各服务定义（DocApplier 由服务注入）。
package mongox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"
	"unicode/utf8"
)

// 事件类型（event_type 列的取值集合）。
const (
	// EventSave 保存/覆盖写文档（按 _id upsert）。
	EventSave = "SAVE"
	// EventDelete 删除文档（按 _id，不存在即成功）。
	EventDelete = "DELETE"
)

// 已同步状态（status 列）。
const (
	// StatusPending 待同步。
	StatusPending = 0
	// StatusDone 已同步。
	StatusDone = 1
)

// MaxAttempts 是单条事件的同步尝试上限：达到上限后不再自动重试，
// 行保留（status 仍为 pending、attempts=上限）供人工处理。
// 与 t_read_search_outbox 的 selectPending 口径一致（attempts < 10）。
const MaxAttempts = 10

// DefaultErrorLength 是 last_error 的截断长度（列宽 varchar(1000)）。
const DefaultErrorLength = 1000

// DefaultReplayInterval 是 Loop 的默认轮询间隔（30 秒）。
const DefaultReplayInterval = 30 * time.Second

// Event 是一条待重放的跨存储同步事件（t_read_mongo_outbox 的一行）。
type Event struct {
	// ID 事件ID（UUID v7，由服务生成）。
	ID string
	// AggregateType 聚合类型（服务自定义，如 chapter / book_doc）。
	AggregateType string
	// AggregateID 聚合ID（章节 id 或书籍 id）。
	AggregateID string
	// EventType SAVE / DELETE。
	EventType string
	// Payload 完整文档 JSON（SAVE 事件；DELETE 为空）。
	Payload string
	// Status 0 待同步 / 1 已同步。
	Status int
	// Attempts 已尝试次数。
	Attempts int
	// LastError 最近一次同步错误（截断到 1000 字符）。
	LastError string
}

// EventStore 是 outbox 行的持久化抽象（MySQL 侧；事务感知——Enqueue 必须与
// 业务写共用同一 ctx 事务，由 db.Handle 保证）。
type EventStore interface {
	// EnqueueOutbox 在当前事务内写入一条待同步事件（INSERT status=0）。
	EnqueueOutbox(ctx context.Context, event Event) error
	// SelectPendingOutbox 取一批待同步事件：
	// status=0 AND attempts<max ORDER BY created_at ASC LIMIT n。
	SelectPendingOutbox(ctx context.Context, limit int) ([]Event, error)
	// MarkOutboxSynced 标记同步成功（status=1, synced_at=now, attempts+1）。
	MarkOutboxSynced(ctx context.Context, id string) error
	// MarkOutboxFailed 记录一次失败（attempts+1, last_error=msg）。
	MarkOutboxFailed(ctx context.Context, id string, message string) error
}

// DocApplier 把一条事件应用到 MongoDB（由各服务注入：聚合类型 → 集合与写法）。
//
// 返回 nil 即视为应用成功；实现必须幂等（重复应用同一事件结果不变）。
type DocApplier interface {
	// Apply 应用事件：SAVE → 反序列化 payload 后按 _id upsert；DELETE → 按 _id 删除。
	Apply(ctx context.Context, event Event) error
}

// DocApplierFunc 函数适配器。
type DocApplierFunc func(ctx context.Context, event Event) error

// Apply 实现 DocApplier。
func (f DocApplierFunc) Apply(ctx context.Context, event Event) error { return f(ctx, event) }

// Replayer 是 outbox 后台重放器：轮询 pending → 应用 → 标记。
//
// 单条失败不中断整批（记 last_error 后继续），与既有 SearchOutboxPublisher 一致；
// 查询失败返回错误（由调用方观测）。多实例并发安全靠行级语义：标记写是幂等的，
// 重复应用（DocApplier 幂等）无副作用。
type Replayer struct {
	store     EventStore
	applier   DocApplier
	batchSize int
	// now 时间源（测试注入），默认 time.Now。
	now func() time.Time
	// logger 可选；nil 时静默。
	logger *slog.Logger
}

// ReplayerOptions 配置 Replayer。
type ReplayerOptions struct {
	// Store outbox 持久化（必需）。
	Store EventStore
	// Applier MongoDB 应用器（必需）。
	Applier DocApplier
	// Batch 每轮最多处理条数，默认 50。
	Batch int
	// Now 时间源（测试注入）。
	Now func() time.Time
	// Logger 结构化日志（可选）。
	Logger *slog.Logger
}

// NewReplayer 建立 Replayer。
func NewReplayer(opts ReplayerOptions) (*Replayer, error) {
	if opts.Store == nil {
		return nil, errors.New("mongox: Store 必须注入")
	}
	if opts.Applier == nil {
		return nil, errors.New("mongox: Applier 必须注入")
	}
	batch := opts.Batch
	if batch <= 0 {
		batch = 50
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Replayer{store: opts.Store, applier: opts.Applier, batchSize: batch, now: now, logger: opts.Logger}, nil
}

// Sweep 执行一轮重放，返回本轮成功条数。
//
// 达到尝试上限的行不会被 SelectPendingOutbox 返回（attempts<max 前置条件），
// 人工处理入口由各服务自行提供（直接查表即可）。
func (r *Replayer) Sweep(ctx context.Context) (int, error) {
	events, err := r.store.SelectPendingOutbox(ctx, r.batchSize)
	if err != nil {
		return 0, fmt.Errorf("mongox: 查询待同步事件失败: %w", err)
	}
	synced := 0
	for _, event := range events {
		if err := r.applier.Apply(ctx, event); err != nil {
			if markErr := r.store.MarkOutboxFailed(ctx, event.ID, truncate(err.Error(), DefaultErrorLength)); markErr != nil {
				return synced, fmt.Errorf("mongox: 记录失败原因出错: %w", markErr)
			}
			if r.logger != nil {
				r.logger.Warn("Mongo 同步事件重放失败",
					"eventId", event.ID,
					"aggregateType", event.AggregateType,
					"aggregateId", event.AggregateID,
					"attempts", event.Attempts+1,
					"error", err.Error())
			}
			continue
		}
		if err := r.store.MarkOutboxSynced(ctx, event.ID); err != nil {
			return synced, fmt.Errorf("mongox: 标记同步成功出错: %w", err)
		}
		synced++
	}
	return synced, nil
}

// Loop 启动轮询循环；interval <= 0 时每 30 秒一轮；ctx 取消即退出。
//
// 与服务的定时任务（如 ChapterAutoPublishTask）同一模式：由 main 装配并传入
// 生命周期 ctx，错误只记日志不退出（下轮重试）。
func (r *Replayer) Loop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := r.Sweep(ctx); err != nil && r.logger != nil {
				r.logger.Error("Mongo 同步 outbox 轮询失败", "error", err)
			}
		}
	}
}

// truncate 截断字符串到指定长度（按字节；若截断点落在多字节 rune 中间，
// 回退到 rune 边界，避免写库时产生非法 UTF-8 序列）。
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// EncodePayload 把文档序列化为 outbox payload（JSON）。
func EncodePayload(document any) (string, error) {
	data, err := json.Marshal(document)
	if err != nil {
		return "", fmt.Errorf("mongox: 序列化文档失败: %w", err)
	}
	return string(data), nil
}

// DecodePayload 把 outbox payload 反序列化回文档。
func DecodePayload(payload string, document any) error {
	if err := json.Unmarshal([]byte(payload), document); err != nil {
		return fmt.Errorf("mongox: 反序列化文档失败: %w", err)
	}
	return nil
}
