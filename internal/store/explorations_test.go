package store

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"wscyber/internal/domain"
)

const startKey = "00112233-4455-4677-8899-aabbccddeeff"

func startInput() domain.ExplorationInput {
	return domain.ExplorationInput{Description: "仅本地工程验证的已知材料", Goal: "验证可靠启动，不访问外部目标", AllowedOrigins: []string{"https://fixture.invalid"}}
}

func TestPostgresExplorationConcurrentReplay(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	const n = 6
	results := make(chan domain.ExplorationStart, n)
	errs := make(chan error, n)
	begin := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			<-begin
			result, err := s.StartExploration(ctx, startKey, startInput(), true)
			results <- result
			errs <- err
		})
	}
	close(begin)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first domain.ExplorationStart
	for result := range results {
		if first.Mission.ID == "" {
			first = result
		}
		if !reflect.DeepEqual(first, result) {
			t.Fatal("同键回执不同", first, result)
		}
	}
	if first.StartStatus != "accepted" || first.Thread.Status != "ready" || !first.Thread.AutoRun || first.Thread.StepCount != 0 || first.Mission.Origin != startInput().Description || first.Thread.Question != startInput().Goal {
		t.Fatal(first)
	}
	missions, err := s.ListMissions(ctx)
	if err != nil || len(missions) != 1 {
		t.Fatal(missions, err)
	}
	threads, err := s.ListThreads(ctx, first.Mission.ID)
	if err != nil || len(threads) != 1 {
		t.Fatal(threads, err)
	}
	events, err := s.ListEvents(ctx, first.Mission.ID, 0)
	if err != nil || len(events) != 3 || events[2].Kind != "exploration.start_requested" {
		t.Fatal(events, err)
	}
	if snapshot := mustSnapshot(t, ctx, s, first.Thread.ID); len(snapshot.Attempts) != 0 || len(snapshot.Actions) != 0 || len(snapshot.Observations) != 0 {
		t.Fatal("接收不应伪造执行或观察", snapshot)
	}
	changed := startInput()
	changed.Goal = "另一个目标"
	if _, err := s.StartExploration(ctx, startKey, changed, true); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	changed = startInput()
	changed.AllowedOrigins = []string{"https://other.invalid"}
	if _, err := s.StartExploration(ctx, startKey, changed, true); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	for _, sql := range []string{`UPDATE operations SET response_status = 201 WHERE key = $1`, `DELETE FROM operations WHERE key = $1`} {
		if _, err := s.pool.Exec(ctx, sql, startKey); err == nil {
			t.Fatal("回执必须不可变")
		}
	}
	if _, err := s.SetThreadControl(ctx, first.Thread.ID, "paused", false); err != nil {
		t.Fatal(err)
	}
	replay, err := s.StartExploration(ctx, startKey, startInput(), false)
	if err != nil || !reflect.DeepEqual(replay, first) {
		t.Fatal(replay, err)
	}
	if snapshot := mustSnapshot(t, ctx, s, first.Thread.ID); snapshot.Thread.Status != "paused" || snapshot.Thread.AutoRun || len(snapshot.Attempts) != 0 {
		t.Fatal("回放旧成功响应不能撤销暂停", snapshot)
	}
	if attempt, err := s.ClaimThread(ctx, 4); err != nil || attempt != nil {
		t.Fatal(attempt, err)
	}
}

func TestPostgresExplorationUnavailableAndRollback(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	if _, err := s.StartExploration(ctx, startKey, startInput(), false); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal(err)
	}
	// 在最后一步回执写入故障，验证业务、方向和事件也全部回滚。
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION fail_receipt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test rollback' USING ERRCODE = '23514'; END $$;
		CREATE TRIGGER test_fail_receipt BEFORE INSERT ON operations FOR EACH ROW EXECUTE FUNCTION fail_receipt()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartExploration(ctx, startKey, startInput(), true); err == nil {
		t.Fatal("故障应返回失败")
	}
	for _, table := range []string{"missions", "threads", "events", "operations"} {
		var count int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{table}.Sanitize()).Scan(&count); err != nil || count != 0 {
			t.Fatal(table, count, err)
		}
	}
	if _, err := s.GetOperation(ctx, startKey); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER test_fail_receipt ON operations; DROP FUNCTION fail_receipt()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartExploration(ctx, startKey, startInput(), true); err != nil {
		t.Fatal("回滚不占用键", err)
	}
}

func TestPostgresExplorationRestartAndUnknown(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	first, err := s.StartExploration(ctx, startKey, startInput(), true)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	receipt, err := s.GetOperation(ctx, startKey)
	if err != nil || receipt.ResourceID != first.Mission.ID || receipt.ResponseStatus != 202 || receipt.CommittedAt.IsZero() {
		t.Fatal(receipt, err)
	}
	attempt, err := s.ClaimThread(ctx, 1)
	if err != nil || attempt == nil || attempt.ThreadID != first.Thread.ID {
		t.Fatal(attempt, err)
	}
	input := fixtureAction()
	input.URL = "https://fixture.invalid/read"
	action, err := s.ProposeAction(ctx, first.Thread.ID, attempt.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishAttempt(ctx, attempt.ID, "succeeded", "ready", "提出一个受控动作", ""); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimAction(ctx)
	if err != nil || claimed == nil || claimed.ID != action.ID {
		t.Fatal(claimed, err)
	}
	// 未调用外部执行器，模拟进程在认领后退出；恢复必须保守标 unknown。
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	replayed, err := s.StartExploration(ctx, startKey, startInput(), true)
	if err != nil || !reflect.DeepEqual(first, replayed) {
		t.Fatal(replayed, err)
	}
	snapshot := mustSnapshot(t, ctx, s, first.Thread.ID)
	if snapshot.Thread.Status != "blocked" || len(snapshot.Actions) != 1 || snapshot.Actions[0].Status != "outcome_unknown" || len(snapshot.Attempts) != 1 {
		t.Fatal(snapshot)
	}
	if a, err := s.ClaimAction(ctx); err != nil || a != nil {
		t.Fatal("不得重放未知动作", a, err)
	}
	if a, err := s.ClaimThread(ctx, 4); err != nil || a != nil {
		t.Fatal("未知动作应阻止继续研究", a, err)
	}
}

func TestPostgresExplorationWaitBoundAndInFlightReceipt(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	hash := sha256.Sum256([]byte(startKey))
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1::bigint)`, int64(binary.BigEndian.Uint64(hash[:8]))); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = s.StartExploration(ctx, startKey, startInput(), true)
	if !errors.Is(err, domain.ErrConflict) || time.Since(start) > 5*time.Second {
		t.Fatal("锁等待必须有界", err, time.Since(start))
	}
	if _, err := s.GetOperation(ctx, startKey); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// 阻塞最后事件锁：写入仍在途时，查询回执是 404，但随后可以成功提交。
	tx, err = s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1,$2)`, lockNamespace, eventLock); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.StartExploration(ctx, startKey, startInput(), true); done <- err }()
	// 等到该提交确实持有键锁，避免依靠固定 sleep 猜测调度。
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
wait:
	for {
		var held bool
		if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND objsubid=1 AND granted)`).Scan(&held); err != nil {
			t.Fatal(err)
		}
		if held {
			break wait
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("提交未进入在途状态")
		}
	}
	if _, err := s.GetOperation(ctx, startKey); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("在途提交不能提前生成成功回执", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := s.GetOperation(ctx, startKey); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresExplorationCanonicalScopeAndBudget(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	input := startInput()
	input.AllowedOrigins = []string{"https://z.invalid", "https://a.invalid", "https://z.invalid"}
	first, err := s.StartExploration(ctx, startKey, input, true)
	if err != nil {
		t.Fatal(err)
	}
	input.AllowedOrigins = []string{"https://a.invalid", "https://z.invalid"}
	second, err := s.StartExploration(ctx, startKey, input, true)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal(second, err)
	}
	attempt, err := s.ClaimThread(ctx, 1)
	if err != nil || attempt == nil {
		t.Fatal(attempt, err)
	}
	if err := s.FinishAttempt(ctx, attempt.ID, "succeeded", "ready", "已完成一轮", "继续待研究问题"); err != nil {
		t.Fatal(err)
	}
	if attempt, err := s.ClaimThread(ctx, 1); err != nil || attempt != nil {
		t.Fatal(attempt, err)
	}
	if _, err := s.StartExploration(ctx, startKey, input, true); err != nil {
		t.Fatal(err)
	}
	snapshot := mustSnapshot(t, ctx, s, first.Thread.ID)
	if snapshot.Thread.Status != "blocked" || snapshot.Thread.StepCount != 1 || len(snapshot.Attempts) != 1 {
		t.Fatal("回执重放不得刷新预算", snapshot)
	}
}

func TestPostgresMigrationUpgradeAndFailure(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	a := propose(t, ctx, s, thread.ID, "")
	if _, err := s.SetThreadControl(ctx, thread.ID, "paused", false); err != nil {
		t.Fatal(err)
	}
	before := mustSnapshot(t, ctx, s, thread.ID)
	beforeJSON, _ := json.Marshal(before)
	// 保留旧原始字节、Attempt 与累计预算；这不是实际执行外部动作。
	evidenceThread := fixtureThread(t, ctx, s)
	attempt := claimAttempt(t, ctx, s, evidenceThread.ID, 4)
	evidenceAction := propose(t, ctx, s, evidenceThread.ID, attempt.ID)
	approve(t, ctx, s, evidenceAction.ID)
	if err := s.FinishAttempt(ctx, attempt.ID, "succeeded", "ready", "本地测试结果", "读取已有材料"); err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.ClaimAction(ctx); err != nil || claimed == nil || claimed.ID != evidenceAction.ID {
		t.Fatal(claimed, err)
	}
	if err := s.FinishAction(ctx, evidenceAction.ID, "succeeded", domain.ActionResult{Body: []byte{0, 255, 10, 13, 128}, StatusCode: 200}, ""); err != nil {
		t.Fatal(err)
	}
	evidenceBefore, _ := json.Marshal(mustSnapshot(t, ctx, s, evidenceThread.ID))
	// 仅在本例隔离 schema 内退去新增空结构，重建无版本表的旧安装状态。
	dropResearchFixture(t, s, ctx)
	if _, err := s.pool.Exec(ctx, `DROP TABLE model_call_contents; DROP FUNCTION guard_model_call_content();
        DROP TABLE model_calls; DROP FUNCTION guard_model_call_terminal(); DROP TABLE model_prices;
        DROP TABLE model_ledger_settings; ALTER TABLE missions DROP COLUMN record_model_content;
        DROP TABLE mission_models; DROP FUNCTION reject_mission_model_mutation();
		DROP TABLE model_tests; DROP FUNCTION protect_model_test();
		DROP TABLE model_settings; ALTER TABLE model_configs DROP CONSTRAINT model_configs_current_version;
		DROP TABLE model_config_versions; DROP TABLE model_configs; DROP FUNCTION reject_model_version_mutation();
		DROP TABLE operations; DROP FUNCTION wscyber_guard_operation(); DROP TABLE wscyber_schema_migrations`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	after := mustSnapshot(t, ctx, s, thread.ID)
	afterJSON, _ := json.Marshal(after)
	if string(beforeJSON) != string(afterJSON) || after.Actions[0].ID != a.ID || after.Actions[0].Status != "pending" {
		t.Fatal("升级改变了旧记录", after)
	}
	evidenceAfter, _ := json.Marshal(mustSnapshot(t, ctx, s, evidenceThread.ID))
	if string(evidenceBefore) != string(evidenceAfter) {
		t.Fatal("升级改变了旧证据字节、尝试或预算")
	}
	var paused bool
	if err := s.pool.QueryRow(ctx, `SELECT pause_requested FROM threads WHERE id=$1`, thread.ID).Scan(&paused); err != nil || !paused {
		t.Fatal(paused, err)
	}
	bad := append(append([]migration{}, migrations...), migration{migrations[len(migrations)-1].version + 1, `CREATE TABLE should_rollback(id integer); SELECT missing_migration_function()`})
	if err := s.mutate(ctx, func(tx pgx.Tx) error { return applyMigrations(ctx, tx, bad) }); err == nil {
		t.Fatal("坏迁移不能成功")
	}
	var count int
	var absent bool
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM wscyber_schema_migrations`).Scan(&count); err != nil || count != len(migrations) {
		t.Fatal(count, err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT to_regclass('should_rollback') IS NULL`).Scan(&absent); err != nil || !absent {
		t.Fatal("失败迁移遗留结构", absent, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE wscyber_schema_migrations SET checksum='changed' WHERE version=1`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if reopened, err := Open(ctx, dsn); err == nil {
		reopened.Close()
		t.Fatal("已应用迁移被修改应拒绝启动")
	}
}
