package webapi

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/chainreactors/cyber/core/operation"
)

// PostgreSQL 并发语义（native/POSTGRES.md §5.3）：write 助手的全局 advisory lock
// 保证所有 webapi 写事务串行。以下测试用真实并发
// （多个 goroutine、各自连接）验证依赖该串行的业务不变量。

func openPGTestStore(t *testing.T) (*taskStore, testHub) {
	t.Helper()
	hub := newTestHub(t)
	store, err := openTaskStore(hub.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, hub
}

// 全局写锁下两个写事务串行：后到者在先到者提交前不进入事务体，进入后看得到先到者已提交的行；
// 读不取锁，持写锁期间照常返回（看不到未提交行）。
func TestWriteLockSerializesWriters(t *testing.T) {
	store, _ := openPGTestStore(t)
	ctx := t.Context()
	insertTask := func(ctx context.Context, tx bun.Tx, id string) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO webapi_tasks (id, known_info, objective, origins_json, initial_session_id, created_at) VALUES (?, 'k', 'o', '[]', ?, '2026-11-22T00:00:00Z')`, id, "s-"+id)
		return err
	}
	firstIn, releaseFirst := make(chan struct{}), make(chan struct{})
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	var secondEntered time.Time
	var firstCommitted time.Time
	var secondSawFirst bool
	go func() {
		firstDone <- store.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			if err := insertTask(ctx, tx, "serial-1"); err != nil {
				return err
			}
			close(firstIn)
			<-releaseFirst
			firstCommitted = time.Now() // 回调返回后才提交，见下方先后判断留有余量
			return nil
		})
	}()
	<-firstIn
	go func() {
		secondDone <- store.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
			secondEntered = time.Now()
			exists, err := tx.NewSelect().Model((*taskModel)(nil)).Where("id = ?", "serial-1").Exists(ctx)
			secondSawFirst = exists
			if err != nil {
				return err
			}
			return insertTask(ctx, tx, "serial-2")
		})
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("第二个写事务在第一个提交前完成: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	// 持写锁期间读不阻塞，且看不到未提交行。
	readCtx, cancel := context.WithTimeout(ctx, time.Second)
	task, err := store.Get(readCtx, "serial-1")
	cancel()
	if err != nil || task != nil {
		t.Fatalf("持写锁期间读 = %+v, %v（应立即返回且看不到未提交行）", task, err)
	}
	close(releaseFirst)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	if secondEntered.Before(firstCommitted) || !secondSawFirst {
		t.Fatalf("写事务未串行：second entered %v, first committed %v, saw first=%v", secondEntered, firstCommitted, secondSawFirst)
	}
	for _, id := range []string{"serial-1", "serial-2"} {
		if task, err := store.Get(ctx, id); err != nil || task == nil {
			t.Fatalf("%s = %+v %v", id, task, err)
		}
	}
}

// 另一写者（独立连接）持全局写锁时，先读后写的业务写入在事务开头等锁，释放后成功；
// 普通读不受影响；等锁后幂等 / in_flight 语义不变、不额外登记行（原 store_txlock 测试的 PG 版）。
func TestTaskStoreWritesWaitForGlobalWriteLock(t *testing.T) {
	for _, kind := range []string{NotifyKindNew, NotifyKindManual} {
		t.Run(kind, func(t *testing.T) {
			store, hub := openPGTestStore(t)
			release := holdWebAPIWriteLock(t, hub)
			readCtx, cancel := context.WithTimeout(t.Context(), time.Second)
			_, err := store.notification(readCtx, "synthetic-missing")
			cancel()
			if err != nil {
				t.Fatalf("ordinary read under write lock: %v", err)
			}
			model := &notificationModel{
				ID: "synthetic-notification", DedupeKey: "synthetic-key", Kind: kind,
				TaskID: "synthetic-task", FindingID: "synthetic-finding", VerificationID: "synthetic-verification",
				Status: NotifyQueued, CreatedAt: notifyTime(time.Now()),
			}
			done := make(chan error, 1)
			started := time.Now()
			go func() {
				if kind == NotifyKindNew {
					inserted, err := store.insertNotification(t.Context(), model)
					if err == nil && !inserted {
						err = fmt.Errorf("new notification was not inserted")
					}
					done <- err
					return
				}
				id, inFlight, err := store.enqueueManualNotification(t.Context(), model)
				if err == nil && (id != model.ID || inFlight != "") {
					err = fmt.Errorf("manual enqueue: id=%q inFlight=%q", id, inFlight)
				}
				done <- err
			}()
			waitWriteLockWaiter(t, hub)
			select {
			case err := <-done:
				t.Fatalf("write returned before lock release after %v: %v", time.Since(started), err)
			case <-time.After(300 * time.Millisecond):
			}
			release()
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("write after lock release: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("write did not finish after lock release")
			}
			saved, err := store.notification(t.Context(), model.ID)
			if err != nil || saved == nil || saved.Kind != kind || saved.Status != NotifyQueued {
				t.Fatalf("persisted notification = %+v, err=%v", saved, err)
			}
			if kind == NotifyKindNew {
				if inserted, err := store.insertNotification(t.Context(), model); err != nil || inserted {
					t.Fatalf("duplicate new: inserted=%v err=%v", inserted, err)
				}
			} else {
				if id, inFlight, err := store.enqueueManualNotification(t.Context(), model); err != nil || id != "" || inFlight != model.ID {
					t.Fatalf("duplicate manual: id=%q inFlight=%q err=%v", id, inFlight, err)
				}
			}
			var count int
			if err := store.db().QueryRow(`SELECT count(*) FROM webapi_notifications`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("notification count=%d err=%v", count, err)
			}
		})
	}
}

// 同指纹、不同 call 的两个认领真正并发：只放行一个，另一个看到未决行而回滚（0 发送）。
func TestClaimSameFingerprintConcurrentOnlyOne(t *testing.T) {
	store, _ := openPGTestStore(t)
	outcome, err := store.Save(t.Context(), "task-claim", "k", "o", []string{"http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	const callers = 4
	type result struct {
		model *requestModel
		fresh bool
		err   error
	}
	results := make([]result, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "http://127.0.0.1:1/orders/1", nil)
			spec, _ := requestSpecFor(newRequestID(), req, nil)
			inv := operation.Invocation{SessionID: outcome.Task.InitialSessionID, TurnID: "turn", CallID: fmt.Sprintf("call-%d", i)}
			<-start
			model, fresh, err := store.claim(t.Context(), inv, outcome.Task.ID, spec, "claimed", "发送前已认领")
			results[i] = result{model, fresh, err}
		}()
	}
	close(start)
	wg.Wait()
	winners, winnerID := 0, ""
	for _, r := range results {
		if r.fresh && r.err == nil {
			winners++
			winnerID = r.model.ID
		}
	}
	if winners != 1 {
		t.Fatalf("同指纹并发认领放行 %d 个: %+v", winners, results)
	}
	for _, r := range results {
		if r.fresh {
			continue
		}
		if !errors.Is(r.err, errUnknownReplay) || r.model == nil || r.model.ID != winnerID {
			t.Fatalf("落选认领应返回 errUnknownReplay 并指向获胜行 %s: %+v", winnerID, r)
		}
	}
	items, err := store.evidenceList(t.Context(), outcome.Task.ID, 10, "", "")
	if err != nil || len(items) != 1 || items[0].ID != winnerID {
		t.Fatalf("证据行 = %+v %v（落选者须回滚）", items, err)
	}
}

// 多个会话并发新建候选：全局编号 MAX(number)+1 在全局写锁下连续、不重复。
func TestSaveFindingConcurrentNumbersConsecutive(t *testing.T) {
	store, _ := openPGTestStore(t)
	tasks := []string{"task-a", "task-b"}
	sessions := map[string]string{}
	for _, id := range tasks {
		outcome, err := store.Save(t.Context(), id, "k", "o", []string{"http://127.0.0.1:1"})
		if err != nil {
			t.Fatal(err)
		}
		sessions[id] = outcome.Task.InitialSessionID
	}
	const callers = 12
	numbers := make([]string, callers)
	errs := make([]error, callers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			taskID := tasks[i%len(tasks)]
			args := findingToolArguments{Content: findingContent{Title: fmt.Sprintf("候选 %d", i), Description: "合成", Steps: []findingStepInput{}, Evidence: []findingEvidenceInput{}}}
			inv := operation.Invocation{SessionID: sessions[taskID], TurnID: "turn", CallID: fmt.Sprintf("finding-%d", i)}
			<-start
			detail, err := store.saveFinding(t.Context(), inv, taskID, args, []byte(fmt.Sprintf(`{"i":%d}`, i)))
			if err == nil {
				numbers[i] = detail.Number
			}
			errs[i] = err
		}()
	}
	close(start)
	wg.Wait()
	want := make([]string, callers)
	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		want[i] = findingNumber(int64(i + 1))
	}
	slices.Sort(numbers)
	slices.Sort(want)
	if !slices.Equal(numbers, want) {
		t.Fatalf("并发编号 = %v, want %v", numbers, want)
	}
}
