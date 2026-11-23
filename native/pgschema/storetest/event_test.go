package storetest_test

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAOPMessageRoundTripSkipsDeltas(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	createSession(t, ctx, f.store, "s1")
	created := time.Date(2026, 7, 19, 1, 2, 3, 0, time.UTC)
	user := &aop.Event{
		Id: "e-user", EmittedAt: timestamppb.New(created), SessionId: "s1", Emitter: "operator",
		Payload: &aop.Event_Message{Message: &aop.Message{Id: "m1", Role: "user", Content: []*aop.Content{aop.Text("hello")}}},
	}
	_ = types.SetWebMessage(user, &types.WebMessageMetadata{Code: "x"})
	if err := f.store.AddAOPEvent(ctx, "s1", user); err != nil {
		t.Fatal(err)
	}
	assistant := messageEvent("e-message", "s1", "hi there")
	if err := f.store.AddAOPEvent(ctx, "s1", assistant); err != nil {
		t.Fatal(err)
	}
	delta := &aop.Event{Id: "e-delta", SessionId: "s1", Payload: &aop.Event_MessageDelta{MessageDelta: &aop.MessageDelta{
		MessageId: "m-1", Value: &aop.MessageDelta_Text{Text: "hi"},
	}}}
	if cursor, persisted, err := f.store.AppendAOPEvent(ctx, "s1", delta); err != nil || persisted || cursor != 0 {
		t.Fatalf("delta append = %d %v %v", cursor, persisted, err)
	}
	if cursor, persisted, err := f.store.AppendAOPEvent(ctx, "s1", nil); err != nil || persisted || cursor != 0 {
		t.Fatalf("nil append = %d %v %v", cursor, persisted, err)
	}
	events, err := f.store.ListAOPEvents(ctx, "s1", 10)
	if err != nil || len(events) != 2 {
		t.Fatalf("events = %v, %v", events, err)
	}
	if !proto.Equal(events[0], user) || !proto.Equal(events[1], assistant) {
		t.Fatalf("events not preserved: %v", events)
	}
	meta, ok, err := types.GetWebMessage(events[0])
	if err != nil || !ok || meta.GetCode() != "x" {
		t.Fatalf("web metadata = %v %v %v", meta, ok, err)
	}
	var createdAt string
	if err := f.raw.QueryRowContext(ctx, `SELECT created_at FROM chat_aop_events WHERE event_id = 'e-user'`).Scan(&createdAt); err != nil {
		t.Fatal(err)
	}
	if createdAt != "2026-07-19T01:02:03Z" {
		t.Fatalf("created_at = %q（应取 emitted_at，RFC3339Nano UTC）", createdAt)
	}
}

func TestAppendAOPEventIsIdempotentByEventID(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	createSession(t, ctx, f.store, "s1")
	event := messageEvent("event-retry", "s1", "once")
	cursor, persisted, err := f.store.AppendAOPEvent(ctx, "s1", event)
	if err != nil || !persisted || cursor != 1 {
		t.Fatalf("first append = %d %v %v", cursor, persisted, err)
	}
	again, persisted, err := f.store.AppendAOPEvent(ctx, "s1", proto.Clone(event).(*aop.Event))
	if err != nil || persisted || again != cursor {
		t.Fatalf("retry append = %d %v %v", again, persisted, err)
	}
	if n := countRows(t, f.raw, `SELECT count(*) FROM chat_aop_events WHERE session_id = 's1'`); n != 1 {
		t.Fatalf("rows = %d", n)
	}
	// 游标按会话独立递增。
	createSession(t, ctx, f.store, "s2")
	for i, want := range []int64{1, 2, 3} {
		got, _, err := f.store.AppendAOPEvent(ctx, "s2", messageEvent(fmt.Sprintf("s2-%d", i), "s2", "x"))
		if err != nil || got != want {
			t.Fatalf("s2 cursor = %d, %v; want %d", got, err, want)
		}
	}
	got, _, err := f.store.AppendAOPEvent(ctx, "s1", messageEvent("s1-next", "s1", "x"))
	if err != nil || got != 2 {
		t.Fatalf("s1 next cursor = %d, %v", got, err)
	}
}

func TestAppendAOPEventValidation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, _, err := f.store.AppendAOPEvent(ctx, "s1", &aop.Event{SessionId: "s1", Payload: &aop.Event_Status{Status: &aop.Status{State: "ready"}}}); err == nil {
		t.Fatal("accepted event without id")
	}
	if _, _, err := f.store.AppendAOPEvent(ctx, " ", messageEvent("e", "s1", "x")); err == nil {
		t.Fatal("accepted empty session id")
	}
	if err := f.store.AddAOPEvent(ctx, "missing", messageEvent("orphan", "missing", "x")); err == nil {
		t.Fatal("AddAOPEvent created an orphan event")
	}
	if n := countRows(t, f.raw, `SELECT count(*) FROM chat_aop_events`); n != 0 {
		t.Fatalf("orphan rows = %d", n)
	}
}

func turnEvent(id, session, turn string, started bool) *aop.Event {
	event := &aop.Event{Id: id, EmittedAt: timestamppb.Now(), SessionId: session, TurnId: turn, Emitter: "cyber"}
	if started {
		event.Payload = &aop.Event_TurnStarted{TurnStarted: &aop.TurnStarted{}}
	} else {
		event.Payload = &aop.Event_TurnEnded{TurnEnded: &aop.TurnEnded{StopReason: "end_turn"}}
	}
	return event
}

func TestTurnTerminalDetection(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	createSession(t, ctx, f.store, "s1")
	mustAppend := func(event *aop.Event) (int64, bool) {
		t.Helper()
		cursor, persisted, err := f.store.AppendAOPEvent(ctx, "s1", event)
		if err != nil {
			t.Fatal(err)
		}
		return cursor, persisted
	}
	mustAppend(turnEvent("t1-start", "s1", "t1", true))
	if ended, err := f.store.HasTurnEnded(ctx, "s1", "t1"); err != nil || ended {
		t.Fatalf("HasTurnEnded before end = %v %v", ended, err)
	}
	mustAppend(messageEvent("t1-msg", "s1", "x"))
	endCursor, persisted := mustAppend(turnEvent("t1-end", "s1", "t1", false))
	if !persisted || endCursor != 3 {
		t.Fatalf("end = %d %v", endCursor, persisted)
	}
	if ended, err := f.store.HasTurnEnded(ctx, "s1", "t1"); err != nil || !ended {
		t.Fatalf("HasTurnEnded = %v %v", ended, err)
	}
	// 第二个终止事件（不同 event id）不落库，返回已有终止游标。
	again, persisted := mustAppend(turnEvent("t1-end-dup", "s1", "t1", false))
	if persisted || again != endCursor {
		t.Fatalf("duplicate terminal = %d %v", again, persisted)
	}
	// 来源会话不同的终止事件不算本会话的终止。
	foreign, persisted := mustAppend(turnEvent("t1-end-sub", "child", "t1", false))
	if !persisted || foreign != 4 {
		t.Fatalf("foreign terminal = %d %v", foreign, persisted)
	}
	// 同一 turn 重新开始后，之前的终止不再算数。
	mustAppend(turnEvent("t1-restart", "s1", "t1", true))
	if ended, err := f.store.HasTurnEnded(ctx, "s1", "t1"); err != nil || ended {
		t.Fatalf("HasTurnEnded after restart = %v %v", ended, err)
	}
	if ended, err := f.store.HasTurnEnded(ctx, "s1", "other"); err != nil || ended {
		t.Fatalf("unknown turn = %v %v", ended, err)
	}
	if n := countRows(t, f.raw, `SELECT count(*) FROM chat_aop_events WHERE session_id = 's1'`); n != 5 {
		t.Fatalf("rows = %d, want 5", n)
	}
}

func TestMaxAOPEventSeq(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	createSession(t, ctx, f.store, "s1")
	if seq, err := f.store.MaxAOPEventSeq(ctx, "s1"); err != nil || seq != 0 {
		t.Fatalf("empty max seq = %d %v", seq, err)
	}
	for i, seq := range []uint64{3, 9, 5} {
		event := messageEvent(fmt.Sprintf("e%d", i), "s1", "x")
		event.Seq = seq
		if err := f.store.AddAOPEvent(ctx, "s1", event); err != nil {
			t.Fatal(err)
		}
	}
	if seq, err := f.store.MaxAOPEventSeq(ctx, "s1"); err != nil || seq != 9 {
		t.Fatalf("max seq = %d %v", seq, err)
	}
}

func cursors(t *testing.T, items []*aop.EventDelivery) []int64 {
	t.Helper()
	out := make([]int64, 0, len(items))
	for _, item := range items {
		c, err := strconv.ParseInt(item.GetCursor(), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func TestListAOPEventPageAndAfter(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	createSession(t, ctx, f.store, "s1")
	// 游标 1..6；游标 3 的事件没有 session_id（存储但列出时被过滤）。
	for i := 1; i <= 6; i++ {
		event := messageEvent(fmt.Sprintf("e%d", i), "s1", "x")
		if i == 3 {
			event.SessionId = ""
		}
		if cursor, _, err := f.store.AppendAOPEvent(ctx, "s1", event); err != nil || cursor != int64(i) {
			t.Fatalf("append %d = %d %v", i, cursor, err)
		}
	}
	page, next, err := f.store.ListAOPEventPage(ctx, "s1", 0, 2)
	if err != nil || fmt.Sprint(cursors(t, page)) != "[5 6]" || next != 5 {
		t.Fatalf("page1 = %v next=%d err=%v", cursors(t, page), next, err)
	}
	page, next, err = f.store.ListAOPEventPage(ctx, "s1", next, 2)
	if err != nil || fmt.Sprint(cursors(t, page)) != "[4]" || next != 4 {
		t.Fatalf("page2 = %v next=%d err=%v（游标 3 被过滤）", cursors(t, page), next, err)
	}
	page, next, err = f.store.ListAOPEventPage(ctx, "s1", next, 2)
	if err != nil || fmt.Sprint(cursors(t, page)) != "[2]" || next != 2 {
		t.Fatalf("page3 = %v next=%d err=%v", cursors(t, page), next, err)
	}
	page, next, err = f.store.ListAOPEventPage(ctx, "s1", next, 2)
	if err != nil || fmt.Sprint(cursors(t, page)) != "[1]" || next != 0 {
		t.Fatalf("page4 = %v next=%d err=%v", cursors(t, page), next, err)
	}
	all, err := f.store.ListAOPEvents(ctx, "s1", 0)
	if err != nil || len(all) != 5 {
		t.Fatalf("ListAOPEvents = %d %v", len(all), err)
	}

	after, next, err := f.store.ListAOPEventsAfter(ctx, "s1", 0, 3)
	if err != nil || fmt.Sprint(cursors(t, after)) != "[1 2]" || next != 3 {
		t.Fatalf("after page1 = %v next=%d err=%v（游标越过被过滤的行）", cursors(t, after), next, err)
	}
	after, next, err = f.store.ListAOPEventsAfter(ctx, "s1", next, 3)
	if err != nil || fmt.Sprint(cursors(t, after)) != "[4 5 6]" || next != 6 {
		t.Fatalf("after page2 = %v next=%d err=%v", cursors(t, after), next, err)
	}
	after, next, err = f.store.ListAOPEventsAfter(ctx, "s1", next, 3)
	if err != nil || len(after) != 0 || next != 0 {
		t.Fatalf("after page3 = %v next=%d err=%v", cursors(t, after), next, err)
	}
	after, next, err = f.store.ListAOPEventsAfter(ctx, "s1", 0, 0)
	if err != nil || len(after) != 5 || next != 0 {
		t.Fatalf("after unlimited = %d next=%d err=%v", len(after), next, err)
	}
}

func TestStoredEventsRetainUnknownProtobufDataAsBytea(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	createSession(t, ctx, f.store, "session")
	event := &aop.Event{Id: "unknown-extension", SessionId: "session", Seq: 1,
		Payload: &aop.Event_Extension{Extension: &anypb.Any{TypeUrl: "type.example/future.Event", Value: []byte{8, 42}}}}
	event.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
	if _, _, err := f.store.AppendAOPEvent(ctx, "session", event); err != nil {
		t.Fatal(err)
	}
	page, _, err := f.store.ListAOPEventsAfter(ctx, "session", 0, 10)
	if err != nil || len(page) != 1 || !proto.Equal(page[0].Event, event) {
		t.Fatalf("unknown payload was changed: %v, %v", page, err)
	}
	var kind string
	if err := f.raw.QueryRowContext(ctx, `SELECT pg_typeof(event_proto)::text FROM chat_aop_events`).Scan(&kind); err != nil || kind != "bytea" {
		t.Fatalf("event storage = %q, %v", kind, err)
	}
}

func TestDeleteSessionCascadesEvents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	createSession(t, ctx, f.store, "s1")
	createSession(t, ctx, f.store, "s2")
	for _, id := range []string{"a", "b"} {
		if err := f.store.AddAOPEvent(ctx, "s1", messageEvent("s1-"+id, "s1", "x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.store.AddAOPEvent(ctx, "s2", messageEvent("s2-a", "s2", "x")); err != nil {
		t.Fatal(err)
	}
	if err := f.store.DeleteSession(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, f.raw, `SELECT count(*) FROM chat_aop_events WHERE session_id = 's1'`); n != 0 {
		t.Fatalf("s1 retained %d events", n)
	}
	if n := countRows(t, f.raw, `SELECT count(*) FROM chat_aop_events WHERE session_id = 's2'`); n != 1 {
		t.Fatalf("s2 events = %d", n)
	}
	// 删除后同一会话的追加失败（events.go 据 GetSession ErrNoRows 吞掉该错误）。
	if err := f.store.AddAOPEvent(ctx, "s1", messageEvent("late", "s1", "x")); err == nil {
		t.Fatal("deleted session accepted an event")
	}
}

// 不经 Service.eventMu 的并发追加：每会话 advisory lock 使游标连续且唯一。
func TestConcurrentAppendsGetDistinctCursors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	createSession(t, ctx, f.store, "s1")
	const writers, perWriter = 8, 10
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perWriter {
				if _, persisted, err := f.store.AppendAOPEvent(ctx, "s1", messageEvent(fmt.Sprintf("w%d-%d", w, i), "s1", "x")); err != nil || !persisted {
					errs <- fmt.Errorf("w%d-%d: persisted=%v err=%v", w, i, persisted, err)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var count, maxCursor, distinct int
	if err := f.raw.QueryRowContext(ctx, `SELECT count(*), max(cursor), count(DISTINCT cursor) FROM chat_aop_events WHERE session_id = 's1'`).Scan(&count, &maxCursor, &distinct); err != nil {
		t.Fatal(err)
	}
	if count != writers*perWriter || maxCursor != count || distinct != count {
		t.Fatalf("count=%d max=%d distinct=%d", count, maxCursor, distinct)
	}
}
