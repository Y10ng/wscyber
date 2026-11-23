package storetest_test

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"

	aop "github.com/chainreactors/cyber/aop"
	toolpb "github.com/chainreactors/cyber/aop/tool"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestRequestLedgerLoadSave(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	hash := []byte{0x00, 0x01, 0xfe, 0xff}
	response := &aop.CancelTurnResponse{Outcome: &aop.CancelTurnResponse_Accepted{Accepted: &aop.TurnReceipt{SessionId: "s1", TurnId: "t1", State: "canceled"}}}

	var loaded aop.CancelTurnResponse
	found, conflict, err := f.store.LoadAOPRequest(ctx, "req-1", "CancelTurn", hash, &loaded)
	if err != nil || found || conflict {
		t.Fatalf("load missing = %v %v %v", found, conflict, err)
	}
	if err := f.store.SaveAOPRequest(ctx, "req-1", "CancelTurn", hash, response); err != nil {
		t.Fatal(err)
	}
	found, conflict, err = f.store.LoadAOPRequest(ctx, "req-1", "CancelTurn", hash, &loaded)
	if err != nil || !found || conflict || !proto.Equal(&loaded, response) {
		t.Fatalf("load = %v %v %v %v", found, conflict, err, &loaded)
	}
	// 同一请求 ID、不同哈希或方法 → 冲突。
	if found, conflict, err := f.store.LoadAOPRequest(ctx, "req-1", "CancelTurn", []byte{0x00}, &loaded); err != nil || found || !conflict {
		t.Fatalf("hash conflict = %v %v %v", found, conflict, err)
	}
	if found, conflict, err := f.store.LoadAOPRequest(ctx, "req-1", "OpenSession", hash, &loaded); err != nil || found || !conflict {
		t.Fatalf("method conflict = %v %v %v", found, conflict, err)
	}
	// 重复保存违反主键（与上游一致，调用方先 Load）。
	if err := f.store.SaveAOPRequest(ctx, "req-1", "CancelTurn", hash, response); err == nil {
		t.Fatal("duplicate SaveAOPRequest accepted")
	}
	// 空请求 ID / nil 响应是空操作。
	if found, conflict, err := f.store.LoadAOPRequest(ctx, " ", "CancelTurn", hash, &loaded); err != nil || found || conflict {
		t.Fatalf("blank id load = %v %v %v", found, conflict, err)
	}
	if err := f.store.SaveAOPRequest(ctx, "", "CancelTurn", hash, response); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SaveAOPRequest(ctx, "req-2", "CancelTurn", hash, nil); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, f.raw, `SELECT count(*) FROM aop_request_ledger`); n != 1 {
		t.Fatalf("ledger rows = %d", n)
	}
	var stored []byte
	if err := f.raw.QueryRowContext(ctx, `SELECT request_hash FROM aop_request_ledger WHERE request_id = 'req-1'`).Scan(&stored); err != nil || string(stored) != string(hash) {
		t.Fatalf("request_hash = %x %v", stored, err)
	}
}

func TestArtifactArchiveRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := artifactEvent(t, "artifact-1")
	second := artifactEvent(t, "artifact-2")
	deliveries, err := f.store.SyncArtifactEvents(ctx, []*aop.Event{first, first, second}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 2 || deliveries[0].GetCursor() != "1" {
		t.Fatalf("deliveries = %+v", deliveries)
	}
	secondCursor, err := strconv.ParseInt(deliveries[1].GetCursor(), 10, 64)
	if err != nil || secondCursor <= 1 {
		t.Fatalf("second cursor = %q %v", deliveries[1].GetCursor(), err)
	}
	if !proto.Equal(deliveries[0].GetEvent(), first) || !proto.Equal(deliveries[1].GetEvent(), second) {
		t.Fatal("archive did not preserve the original events")
	}
	next, err := f.store.SyncArtifactEvents(ctx, nil, 1)
	if err != nil || len(next) != 1 || next[0].GetEvent().GetId() != second.GetId() {
		t.Fatalf("resume = %+v %v", next, err)
	}
	// 重放已归档事件不新增行。
	if _, err := f.store.SyncArtifactEvents(ctx, []*aop.Event{second}, 0); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, f.raw, `SELECT count(*) FROM raw_artifacts`); n != 2 {
		t.Fatalf("raw rows = %d", n)
	}
}

func TestArtifactArchiveUsesFixedPages(t *testing.T) {
	f := newFixture(t)
	const batch = 100 // 上游 artifactSyncBatch
	events := make([]*aop.Event, batch+1)
	for i := range events {
		events[i] = artifactEvent(t, fmt.Sprintf("artifact-%03d", i))
	}
	first, err := f.store.SyncArtifactEvents(context.Background(), events, 0)
	if err != nil || len(first) != batch {
		t.Fatalf("first page = %d %v", len(first), err)
	}
	second, err := f.store.SyncArtifactEvents(context.Background(), nil, batch)
	if err != nil || len(second) != 1 {
		t.Fatalf("second page = %d %v", len(second), err)
	}
}

func TestArtifactArchiveRetainsLootInEitherOrder(t *testing.T) {
	for _, lootFirst := range []bool{false, true} {
		f := newFixture(t)
		artifact := artifactEvent(t, "artifact")
		payload, err := anypb.New(&toolpb.Loot{ResultId: "result-1", Tool: "gogo", VerificationStatus: "confirmed"})
		if err != nil {
			t.Fatal(err)
		}
		loot := &aop.Event{Id: "loot", EmittedAt: timestamppb.Now(), Payload: &aop.Event_Extension{Extension: payload}}
		events := []*aop.Event{artifact, loot}
		if lootFirst {
			events = []*aop.Event{loot, artifact}
		}
		deliveries, err := f.store.SyncArtifactEvents(t.Context(), append(events, events...), 0)
		if err != nil || len(deliveries) != 2 {
			t.Fatalf("archive: %v %v", deliveries, err)
		}
		for i, event := range events {
			if !proto.Equal(event, deliveries[i].Event) {
				t.Fatal("canonical event changed")
			}
		}
	}
}

func TestArtifactArchiveRejectsInvalidEvents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	noID := artifactEvent(t, "")
	noTime := artifactEvent(t, "no-time")
	noTime.EmittedAt = nil
	message := messageEvent("msg", "s1", "x")
	for name, batch := range map[string][]*aop.Event{
		"nil": {nil}, "no id": {noID}, "no emitted_at": {noTime}, "not artifact": {message},
		"mixed": {artifactEvent(t, "ok"), message},
	} {
		if _, err := f.store.SyncArtifactEvents(ctx, batch, 0); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if n := countRows(t, f.raw, `SELECT count(*) FROM raw_artifacts`); n != 0 {
		t.Fatalf("invalid batch left %d rows", n)
	}
}

// 并发归档：所有事件落库且读者按游标翻页不漏（写入持同一把 advisory lock，按游标序提交）。
func TestConcurrentArtifactSyncKeepsCursorOrder(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	const writers, perWriter = 6, 15
	var wg sync.WaitGroup
	errs := make(chan error, writers*perWriter)
	batches := make([][]*aop.Event, writers)
	for w := range writers {
		for i := range perWriter {
			batches[w] = append(batches[w], artifactEvent(t, fmt.Sprintf("w%d-%d", w, i)))
		}
	}
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perWriter {
				if _, err := f.store.SyncArtifactEvents(ctx, batches[w][i:i+1], 0); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	seen := map[string]bool{}
	var after int64
	for {
		page, err := f.store.SyncArtifactEvents(ctx, nil, after)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, item := range page {
			seen[item.GetEvent().GetId()] = true
			after, _ = strconv.ParseInt(item.GetCursor(), 10, 64)
		}
	}
	if len(seen) != writers*perWriter {
		t.Fatalf("paged %d artifacts, want %d", len(seen), writers*perWriter)
	}
}
