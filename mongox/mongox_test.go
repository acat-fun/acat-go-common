package mongox

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeStore 是 EventStore 的内存实现。
type fakeStore struct {
	events  []Event
	enqueue []Event
	synced  []string
	failed  map[string]string
	// selectErr 非空时 SelectPendingOutbox 返回它。
	selectErr error
	// applySyncedErr 第 N 次标记成功时返回错误。
	markSyncedErr error
}

func (f *fakeStore) EnqueueOutbox(_ context.Context, event Event) error {
	f.enqueue = append(f.enqueue, event)
	f.events = append(f.events, event)
	return nil
}

func (f *fakeStore) SelectPendingOutbox(_ context.Context, limit int) ([]Event, error) {
	if f.selectErr != nil {
		return nil, f.selectErr
	}
	out := make([]Event, 0, limit)
	for _, e := range f.events {
		if e.Status == StatusPending && e.Attempts < MaxAttempts && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeStore) MarkOutboxSynced(_ context.Context, id string) error {
	if f.markSyncedErr != nil {
		return f.markSyncedErr
	}
	f.synced = append(f.synced, id)
	for i := range f.events {
		if f.events[i].ID == id {
			f.events[i].Status = StatusDone
			f.events[i].Attempts++
		}
	}
	return nil
}

func (f *fakeStore) MarkOutboxFailed(_ context.Context, id string, message string) error {
	if f.failed == nil {
		f.failed = map[string]string{}
	}
	f.failed[id] = message
	for i := range f.events {
		if f.events[i].ID == id {
			f.events[i].Attempts++
			f.events[i].LastError = message
		}
	}
	return nil
}

// TestSweepSyncsPendingEvents 成功路径：待同步事件被应用并标记。
func TestSweepSyncsPendingEvents(t *testing.T) {
	store := &fakeStore{events: []Event{
		{ID: "e1", AggregateType: "chapter", AggregateID: "c1", EventType: EventSave, Payload: "{}"},
		{ID: "e2", AggregateType: "chapter", AggregateID: "c2", EventType: EventDelete},
	}}
	var applied []string
	replayer, err := NewReplayer(ReplayerOptions{
		Store:   store,
		Applier: DocApplierFunc(func(_ context.Context, e Event) error { applied = append(applied, e.ID); return nil }),
	})
	if err != nil {
		t.Fatalf("构造 Replayer 失败: %v", err)
	}
	synced, err := replayer.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep 不应失败: %v", err)
	}
	if synced != 2 || len(store.synced) != 2 {
		t.Fatalf("应同步 2 条，实际 synced=%d marks=%v", synced, store.synced)
	}
	if len(applied) != 2 || applied[0] != "e1" || applied[1] != "e2" {
		t.Fatalf("应用顺序应按 created_at，实际 %v", applied)
	}
	for _, e := range store.events {
		if e.Status != StatusDone {
			t.Fatalf("事件 %s 应为已同步", e.ID)
		}
	}
}

// TestSweepFailureIsolated 单条失败不中断整批：失败记 last_error，后续事件继续。
func TestSweepFailureIsolated(t *testing.T) {
	store := &fakeStore{events: []Event{
		{ID: "e1", AggregateType: "chapter", AggregateID: "c1", EventType: EventSave, Payload: "{}"},
		{ID: "e2", AggregateType: "chapter", AggregateID: "c2", EventType: EventSave, Payload: "{}"},
	}}
	replayer, err := NewReplayer(ReplayerOptions{
		Store: store,
		Applier: DocApplierFunc(func(_ context.Context, e Event) error {
			if e.ID == "e1" {
				return errors.New("mongo 不可达")
			}
			return nil
		}),
	})
	if err != nil {
		t.Fatalf("构造 Replayer 失败: %v", err)
	}
	synced, err := replayer.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep 不应整体失败: %v", err)
	}
	if synced != 1 {
		t.Fatalf("应同步 1 条（e2），实际 %d", synced)
	}
	if msg := store.failed["e1"]; !strings.Contains(msg, "mongo 不可达") {
		t.Fatalf("e1 应记录失败原因，实际 %q", msg)
	}
	if store.events[0].Status != StatusPending || store.events[0].Attempts != 1 {
		t.Fatalf("e1 应保持待同步且 attempts=1，实际 status=%d attempts=%d",
			store.events[0].Status, store.events[0].Attempts)
	}
}

// TestSweepSelectError 查询失败返回错误（调用方可观测）。
func TestSweepSelectError(t *testing.T) {
	store := &fakeStore{selectErr: errors.New("连接拒绝")}
	replayer, err := NewReplayer(ReplayerOptions{
		Store:   store,
		Applier: DocApplierFunc(func(_ context.Context, _ Event) error { return nil }),
	})
	if err != nil {
		t.Fatalf("构造 Replayer 失败: %v", err)
	}
	if _, err := replayer.Sweep(context.Background()); err == nil {
		t.Fatalf("查询失败应返回错误")
	}
}

// TestSweepSkipsExhausted 达到尝试上限的事件不再被选取。
func TestSweepSkipsExhausted(t *testing.T) {
	store := &fakeStore{events: []Event{
		{ID: "e1", AggregateType: "chapter", AggregateID: "c1", EventType: EventSave, Attempts: MaxAttempts},
	}}
	replayer, err := NewReplayer(ReplayerOptions{
		Store:   store,
		Applier: DocApplierFunc(func(_ context.Context, _ Event) error { return nil }),
	})
	if err != nil {
		t.Fatalf("构造 Replayer 失败: %v", err)
	}
	synced, err := replayer.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep 不应失败: %v", err)
	}
	if synced != 0 {
		t.Fatalf("达到上限的事件不应被处理，实际 %d", synced)
	}
}

// TestSweepMarkError 标记失败中断并返回错误（不丢事件）。
func TestSweepMarkError(t *testing.T) {
	store := &fakeStore{
		events:        []Event{{ID: "e1", AggregateType: "chapter", AggregateID: "c1", EventType: EventSave}},
		markSyncedErr: errors.New("标记失败"),
	}
	replayer, err := NewReplayer(ReplayerOptions{
		Store:   store,
		Applier: DocApplierFunc(func(_ context.Context, _ Event) error { return nil }),
	})
	if err != nil {
		t.Fatalf("构造 Replayer 失败: %v", err)
	}
	if _, err := replayer.Sweep(context.Background()); err == nil {
		t.Fatalf("标记失败应返回错误")
	}
}

// TestNewReplayerValidation 构造校验：Store/Applier 缺一即报错。
func TestNewReplayerValidation(t *testing.T) {
	if _, err := NewReplayer(ReplayerOptions{}); err == nil {
		t.Fatalf("缺 Store 应报错")
	}
	if _, err := NewReplayer(ReplayerOptions{Store: &fakeStore{}}); err == nil {
		t.Fatalf("缺 Applier 应报错")
	}
}

// TestPayloadCodec payload 编解码往返。
func TestPayloadCodec(t *testing.T) {
	type doc struct {
		ID    string `json:"_id"`
		Title string `json:"title"`
	}
	encoded, err := EncodePayload(doc{ID: "c1", Title: "第一章"})
	if err != nil {
		t.Fatalf("编码失败: %v", err)
	}
	var decoded doc
	if err := DecodePayload(encoded, &decoded); err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	if decoded.ID != "c1" || decoded.Title != "第一章" {
		t.Fatalf("往返结果错误: %+v", decoded)
	}
}

// TestTruncateUTF8Safe 截断不产生非法 UTF-8。
func TestTruncateUTF8Safe(t *testing.T) {
	s := strings.Repeat("段", 600) // 每字 3 字节，共 1800 字节
	cut := truncate(s, DefaultErrorLength)
	if len(cut) > DefaultErrorLength {
		t.Fatalf("截断后长度超限: %d", len(cut))
	}
	// 非法 UTF-8 检测：strings.ToValidUTF8 应返回原串。
	if strings.ToValidUTF8(cut, "") != cut {
		t.Fatalf("截断产生了非法 UTF-8")
	}
}
