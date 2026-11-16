package store

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"wscyber/internal/domain"
)

func TestCanonicalInput(t *testing.T) {
	first := domain.ActionInput{Kind: "http", Method: "GET", URL: "http://fixture.invalid/a", Reason: "验证授权入口", Headers: map[string]string{"B": "2", "A": "1"}}
	second := first
	second.Headers = map[string]string{"A": "1", "B": "2"}
	a, hashA, err := canonicalInput(first)
	if err != nil {
		t.Fatal(err)
	}
	b, hashB, err := canonicalInput(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) || hashA != hashB || len(hashA) != 64 {
		t.Fatal("对象键顺序不应改变 canonical JSON 与 hash")
	}
	second.Reason = "另一个目的"
	_, changed, err := canonicalInput(second)
	if err != nil || changed == hashA {
		t.Fatal("请求目的改变也必须改变 hash", err)
	}
	if _, _, err = canonicalInput(domain.ActionInput{Kind: "unknown", Reason: "测试"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("无效动作应返回 ErrInvalid", err)
	}
}

// 仅显式提供本项目专用测试 PostgreSQL URL 时执行；不会启动数据库或访问任何目标。
// 每个用例创建独立 schema，清理只删除本用例生成的 schema，不触碰 public 中的数据。
func postgresStore(t *testing.T) (*Store, context.Context, string) {
	t.Helper()
	dsn := os.Getenv("WSCYBER_STORE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("未配置 WSCYBER_STORE_TEST_DATABASE_URL，跳过 PostgreSQL 集成测试")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("测试配置必须是 PostgreSQL URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = admin.Close(cleanup)
	})
	id, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	schemaName := "store_test_" + strings.ReplaceAll(id, "-", "")
	quoted := pgx.Identifier{schemaName}.Sanitize()
	if _, err = admin.Exec(ctx, `CREATE SCHEMA `+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, `DROP SCHEMA `+quoted+` CASCADE`); err != nil {
			t.Errorf("清理测试 schema: %v", err)
		}
	})
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	testURL := parsed.String()
	s, err := Open(ctx, testURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, ctx, testURL
}

func fixtureThread(t *testing.T, ctx context.Context, s *Store) domain.Thread {
	t.Helper()
	mission, err := s.CreateMission(ctx, domain.Mission{Title: "持久层测试", Goal: "验证状态迁移，不执行外部动作"})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := s.CreateThread(ctx, domain.Thread{MissionID: mission.ID, Title: "研究方向", Question: "状态能否可靠保存？", AutoRun: true, Status: "concluded", StepCount: 99})
	if err != nil {
		t.Fatal(err)
	}
	if thread.AutoRun || thread.Status != "ready" || thread.StepCount != 0 {
		t.Fatal("创建方向不应接受外部状态、自动运行标志或累计步数", thread)
	}
	return thread
}

func fixtureAction() domain.ActionInput {
	return domain.ActionInput{Kind: "http", Method: "GET", URL: "https://fixture.invalid/delete", Reason: "只验证危险请求存储，不执行请求"}
}

func propose(t *testing.T, ctx context.Context, s *Store, threadID, attemptID string) domain.Action {
	t.Helper()
	a, err := s.ProposeAction(ctx, threadID, attemptID, fixtureAction())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func approve(t *testing.T, ctx context.Context, s *Store, id string) {
	t.Helper()
	if _, err := s.DecideAction(ctx, id, "approved", "明确授权"); err != nil {
		t.Fatal(err)
	}
}

func claimAttempt(t *testing.T, ctx context.Context, s *Store, threadID string, maxSteps int) *domain.Attempt {
	t.Helper()
	if _, err := s.SetThreadControl(ctx, threadID, "ready", true); err != nil {
		t.Fatal(err)
	}
	a, err := s.ClaimThread(ctx, maxSteps)
	if err != nil || a == nil || a.ThreadID != threadID {
		t.Fatalf("认领执行失败: %v, %v", a, err)
	}
	return a
}

func mustSnapshot(t *testing.T, ctx context.Context, s *Store, id string) domain.Snapshot {
	t.Helper()
	snapshot, err := s.Snapshot(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestPostgresEmptyListsAndOwnership(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	missions, err := s.ListMissions(ctx)
	if err != nil || missions == nil || len(missions) != 0 {
		t.Fatal("空项目列表必须是 []", missions, err)
	}
	thread := fixtureThread(t, ctx, s)
	snapshot := mustSnapshot(t, ctx, s, thread.ID)
	encoded, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(encoded), `"observations":null`) || snapshot.Actions == nil || snapshot.Attempts == nil {
		t.Fatal("Snapshot 中的空列表必须是 []", string(encoded), err)
	}
	if actions, err := s.ListActions(ctx, thread.MissionID); err != nil || actions == nil || len(actions) != 0 {
		t.Fatal("空动作列表必须是 []", actions, err)
	}
	if events, err := s.ListEvents(ctx, thread.MissionID, 1<<60); err != nil || events == nil || len(events) != 0 {
		t.Fatal("空事件列表必须是 []", events, err)
	}
	if _, err := s.ListThreads(ctx, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("缺失项目应返回 ErrNotFound", err)
	}
	other := fixtureThread(t, ctx, s)
	attempt := claimAttempt(t, ctx, s, other.ID, 5)
	if _, err := s.ProposeAction(ctx, thread.ID, attempt.ID, fixtureAction()); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("不能引用其他方向的执行", err)
	}
	if _, err := s.AddObservation(ctx, domain.Observation{ThreadID: thread.ID, AttemptID: attempt.ID, Kind: "model_observation", Content: "跨方向"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("观察不能引用其他方向执行", err)
	}
	action := propose(t, ctx, s, thread.ID, "")
	if _, err := s.AddObservation(ctx, domain.Observation{ThreadID: other.ID, ActionID: action.ID, Kind: "operator_note", Content: "跨方向"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("观察不能引用其他方向动作", err)
	}
	if _, err := s.ProposeAction(ctx, other.ID, "", fixtureAction()); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("人工不能向 running 方向插入动作", err)
	}
}

func TestPostgresActionApprovalAndPause(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	attempt := claimAttempt(t, ctx, s, thread.ID, 3)
	action := propose(t, ctx, s, thread.ID, attempt.ID)
	if _, err := s.ProposeAction(ctx, thread.ID, attempt.ID, fixtureAction()); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("只能有一个未决动作", err)
	}
	approve(t, ctx, s, action.ID)
	if got, err := s.ClaimAction(ctx); err != nil || got != nil {
		t.Fatal("不能抢在模型执行结束前执行动作", got, err)
	}
	if _, err := s.DecideAction(ctx, action.ID, "rejected", "迟到拒绝"); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("不能覆盖已生效的相反决定", err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE actions SET input = input || '{"reason":"篡改"}'::jsonb WHERE id = $1`, action.ID); err == nil {
		t.Fatal("数据库必须阻止请求参数更新")
	}
	if _, err := s.pool.Exec(ctx, `UPDATE actions SET request_hash = repeat('a',64) WHERE id = $1`, action.ID); err == nil {
		t.Fatal("数据库必须阻止 hash 更新")
	}
	if err := s.FinishAttempt(ctx, attempt.ID, "waiting_approval", "waiting_approval", "等待人工审批", "消费动作结果"); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimAction(ctx)
	if err != nil || claimed == nil || claimed.ID != action.ID {
		t.Fatal("模型执行完成后应可认领已批准动作", claimed, err)
	}
	paused, err := s.SetThreadControl(ctx, thread.ID, "paused", false)
	if err != nil || paused.Status != "running" || paused.AutoRun {
		t.Fatal("在途暂停不能伪造执行已停止", paused, err)
	}
	if _, err := s.SetThreadControl(ctx, thread.ID, "ready", true); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("恢复不能覆盖在途状态", err)
	}
	result := domain.ActionResult{Stdout: []byte{0, 1, 255}, Truncated: true}
	if err := s.FinishAction(ctx, action.ID, "succeeded", result, ""); err != nil {
		t.Fatal(err)
	}
	snapshot := mustSnapshot(t, ctx, s, thread.ID)
	if snapshot.Thread.Status != "paused" || len(snapshot.Observations) != 1 || !snapshot.Actions[0].Result.Truncated || string(snapshot.Actions[0].Result.Stdout) != string(result.Stdout) {
		t.Fatal("结果、证据与暂停必须同时生效", snapshot)
	}
	if err := s.FinishAttempt(ctx, attempt.ID, "waiting_approval", "waiting_approval", "迟到", ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("迟到执行回执不得重新制造等待", err)
	}
	if err := s.FinishAction(ctx, action.ID, "failed", domain.ActionResult{}, "迟到失败"); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("迟到动作结果不得覆盖终态", err)
	}
}

func TestPostgresFastRejectionAndManualCompletion(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	attempt := claimAttempt(t, ctx, s, thread.ID, 5)
	action := propose(t, ctx, s, thread.ID, attempt.ID)
	if _, err := s.DecideAction(ctx, action.ID, "rejected", "人工拒绝"); err != nil {
		t.Fatal(err)
	}
	if got := mustSnapshot(t, ctx, s, thread.ID).Thread.Status; got != "running" {
		t.Fatal("拒绝动作不能提前结束运行中的 Attempt", got)
	}
	if err := s.FinishAttempt(ctx, attempt.ID, "waiting_approval", "waiting_approval", "模型已提出动作", "继续研究"); err != nil {
		t.Fatal("快速拒绝后 FinishAttempt 仍应收敛", err)
	}
	if got := mustSnapshot(t, ctx, s, thread.ID).Thread.Status; got != "ready" {
		t.Fatal("动作已拒绝，不应退回审批等待", got)
	}
	if _, err := s.SetThreadControl(ctx, thread.ID, "ready", false); err != nil {
		t.Fatal(err)
	}
	manual := propose(t, ctx, s, thread.ID, "")
	approve(t, ctx, s, manual.ID)
	if a, err := s.ClaimAction(ctx); err != nil || a == nil {
		t.Fatal(a, err)
	}
	if err := s.FinishAction(ctx, manual.ID, "failed", domain.ActionResult{Stderr: []byte("受控失败"), ExitCode: 2}, "工具失败"); err != nil {
		t.Fatal(err)
	}
	snapshot := mustSnapshot(t, ctx, s, thread.ID)
	if snapshot.Thread.Status != "ready" || snapshot.Thread.AutoRun || len(snapshot.Observations) != 1 || snapshot.Actions[1].Error != "工具失败" {
		t.Fatal("人工模式不是暂停；失败结果也必须保存", snapshot)
	}
}

func TestPostgresBudgetAndInFlightPause(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	attempt := claimAttempt(t, ctx, s, thread.ID, 1)
	if _, err := s.SetThreadControl(ctx, thread.ID, "paused", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetThreadControl(ctx, thread.ID, "ready", false); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("暂停在途研究后不能恢复出第二个 Attempt", err)
	}
	if err := s.FinishAttempt(ctx, attempt.ID, "succeeded", "ready", "观察已保存", "下一步"); err != nil {
		t.Fatal(err)
	}
	if got := mustSnapshot(t, ctx, s, thread.ID).Thread; got.Status != "paused" || got.StepCount != 1 {
		t.Fatal("完成时须尊重暂停并保留预算", got)
	}
	if _, err := s.SetThreadControl(ctx, thread.ID, "ready", false); err != nil {
		t.Fatal(err)
	}
	if a, err := s.ClaimThread(ctx, 1); err != nil || a != nil {
		t.Fatal("人工恢复不启用自动研究", a, err)
	}
	if _, err := s.SetThreadControl(ctx, thread.ID, "ready", true); err != nil {
		t.Fatal(err)
	}
	if a, err := s.ClaimThread(ctx, 1); err != nil || a != nil {
		t.Fatal("人工恢复不能绕过累计步数预算", a, err)
	}
	snapshot := mustSnapshot(t, ctx, s, thread.ID)
	if snapshot.Thread.Status != "blocked" || snapshot.Thread.StepCount != 1 || !strings.Contains(snapshot.Thread.NextStep, "预算") || len(snapshot.Attempts) != 1 {
		t.Fatal("预算耗尽应阻塞而非结束问题", snapshot)
	}
	attempt = claimAttempt(t, ctx, s, thread.ID, 2)
	if err := s.FinishAttempt(ctx, attempt.ID, "succeeded", "concluded", "明确结束", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetThreadControl(ctx, thread.ID, "ready", true); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("concluded 不应隐式重开", err)
	}
}

func TestPostgresConcurrentClaims(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	if _, err := s.SetThreadControl(ctx, thread.ID, "ready", true); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	attempts := make(chan *domain.Attempt, 8)
	errorsC := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := s.ClaimThread(ctx, 10)
			attempts <- a
			errorsC <- err
		}()
	}
	wg.Wait()
	close(attempts)
	close(errorsC)
	for err := range errorsC {
		if err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	var claimed *domain.Attempt
	for a := range attempts {
		if a != nil {
			count++
			claimed = a
		}
	}
	if count != 1 {
		t.Fatal("并发只应认领一次研究", count)
	}
	action := propose(t, ctx, s, thread.ID, claimed.ID)
	approve(t, ctx, s, action.ID)
	if err := s.FinishAttempt(ctx, claimed.ID, "waiting_approval", "waiting_approval", "等待审批", ""); err != nil {
		t.Fatal(err)
	}
	actions := make(chan *domain.Action, 8)
	errorsC = make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := s.ClaimAction(ctx)
			actions <- a
			errorsC <- err
		}()
	}
	wg.Wait()
	close(actions)
	close(errorsC)
	for err := range errorsC {
		if err != nil {
			t.Fatal(err)
		}
	}
	count = 0
	for a := range actions {
		if a != nil {
			count++
		}
	}
	if count != 1 {
		t.Fatal("并发只应认领一次动作", count)
	}
}

func TestPostgresRecoveryAndUnknownResolution(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	research := fixtureThread(t, ctx, s)
	attempt := claimAttempt(t, ctx, s, research.ID, 10)
	if _, err := s.AddObservation(ctx, domain.Observation{ThreadID: research.ID, AttemptID: attempt.ID, Kind: "model_observation", Content: "中断前及时保存的材料"}); err != nil {
		t.Fatal(err)
	}
	work := fixtureThread(t, ctx, s)
	unknown := propose(t, ctx, s, work.ID, "")
	approve(t, ctx, s, unknown.ID)
	if a, err := s.ClaimAction(ctx); err != nil || a == nil || a.ID != unknown.ID {
		t.Fatal(a, err)
	}
	queued := fixtureThread(t, ctx, s)
	approved := propose(t, ctx, s, queued.ID, "")
	approve(t, ctx, s, approved.ID)
	pendingThread := fixtureThread(t, ctx, s)
	pending := propose(t, ctx, s, pendingThread.ID, "")
	s.Close()
	var err error
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err = s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot := mustSnapshot(t, ctx, s, research.ID)
	if snapshot.Thread.Status != "blocked" || snapshot.Attempts[0].Status != "interrupted" || snapshot.Attempts[0].Error == "" || len(snapshot.Observations) != 1 {
		t.Fatal("恢复必须保留方向、错误和已有材料", snapshot)
	}
	if err = s.FinishAttempt(ctx, attempt.ID, "succeeded", "ready", "迟到", ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("恢复后的 Attempt 不接受迟到结果", err)
	}
	if got, err := s.GetAction(ctx, pending.ID); err != nil || got.Status != "pending" {
		t.Fatal("恢复不能改变待审批动作", got, err)
	}
	if got, err := s.ClaimAction(ctx); err != nil || got == nil || got.ID != approved.ID {
		t.Fatal("恢复后只应派发原先已批准但未运行的动作", got, err)
	}
	if got, err := s.ClaimAction(ctx); err != nil || got != nil {
		t.Fatal("未知效果动作不能重放", got, err)
	}
	if _, err = s.SetThreadControl(ctx, work.ID, "ready", true); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("普通恢复不能解除未知动作阻塞", err)
	}
	if _, err = s.ProposeAction(ctx, work.ID, "", fixtureAction()); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("未知动作未核对前不能提出新动作", err)
	}
	if err = s.FinishAction(ctx, unknown.ID, "succeeded", domain.ActionResult{}, ""); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("未知历史不能被旧执行回执覆盖", err)
	}
	if err = s.ResolveUnknownAction(ctx, unknown.ID, " "); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("核对说明不能为空", err)
	}
	if err = s.ResolveUnknownAction(ctx, unknown.ID, "人工核对，保留不确定结论"); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAction(ctx, unknown.ID)
	if err != nil || got.Status != "outcome_unknown" || got.ResolvedAt == nil || got.Error == "" || got.RequestHash != unknown.RequestHash {
		t.Fatal("resolve 必须保留原未知状态、请求和错误", got, err)
	}
	if err = s.ResolveUnknownAction(ctx, unknown.ID, "覆盖核对说明"); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("人工核对历史不能被覆盖", err)
	}
	if got := mustSnapshot(t, ctx, s, work.ID).Thread; got.Status != "ready" || got.AutoRun {
		t.Fatal("resolve 不应自动调用模型", got)
	}
	propose(t, ctx, s, work.ID, "")
}

func TestPostgresFinishActionAtomicity(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	action := propose(t, ctx, s, thread.ID, "")
	approve(t, ctx, s, action.ID)
	if a, err := s.ClaimAction(ctx); err != nil || a == nil {
		t.Fatal(a, err)
	}
	before, err := s.ListEvents(ctx, thread.MissionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	// 仅此测试 schema 注入证据保存失败；整个完成事务都必须回滚。
	if _, err = s.pool.Exec(ctx, `ALTER TABLE observations ADD CONSTRAINT test_reject_result CHECK (kind <> 'action_result')`); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishAction(ctx, action.ID, "succeeded", domain.ActionResult{Stdout: []byte("证据")}, ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("注入失败未触发约束", err)
	}
	snapshot := mustSnapshot(t, ctx, s, thread.ID)
	after, err := s.ListEvents(ctx, thread.MissionID, 0)
	if err != nil || snapshot.Actions[0].Status != "running" || snapshot.Actions[0].Result != nil || len(snapshot.Observations) != 0 || len(before) != len(after) {
		t.Fatal("证据保存失败不能部分提交结果或事件", snapshot, after, err)
	}
	if _, err = s.pool.Exec(ctx, `ALTER TABLE observations DROP CONSTRAINT test_reject_result`); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishAction(ctx, action.ID, "succeeded", domain.ActionResult{EffectUnknown: true, Stderr: []byte("未收到回执")}, "效果未知"); err != nil {
		t.Fatal(err)
	}
	snapshot = mustSnapshot(t, ctx, s, thread.ID)
	if snapshot.Thread.Status != "blocked" || snapshot.Actions[0].Status != "outcome_unknown" || len(snapshot.Observations) != 1 {
		t.Fatal("EffectUnknown 必须优先于 succeeded", snapshot)
	}
}

func TestPostgresSingleInstanceAndLockLoss(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	other, err := Open(ctx, dsn)
	if other != nil {
		other.Close()
	}
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatal("同一数据库不应允许第二实例", err)
	}
	thread := fixtureThread(t, ctx, s)
	if _, err = s.SetThreadControl(ctx, thread.ID, "ready", true); err != nil {
		t.Fatal(err)
	}
	s.lockMu.Lock()
	err = s.lock.Close(ctx)
	s.lockMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if a, err := s.ClaimThread(ctx, 5); !errors.Is(err, domain.ErrConflict) || a != nil {
		t.Fatal("失锁后不能继续认领研究", a, err)
	}
	if a, err := s.ClaimAction(ctx); !errors.Is(err, domain.ErrConflict) || a != nil {
		t.Fatal("失锁后不能继续认领动作", a, err)
	}
	if err = s.Recover(ctx); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("失锁后不能恢复业务状态", err)
	}
	s.Close()
	s.Close()
	other, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal("持锁连接关闭后应允许新实例接管", err)
	}
	other.Close()
}
