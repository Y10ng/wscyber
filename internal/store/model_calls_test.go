package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
	"wscyber/internal/modelledger"
	"wscyber/internal/modelprovider"
)

// 仅用于postgresStore生成的隔离schema，旧升级用例先退去空006结构。
func dropLedgerFixture(t *testing.T, s *Store, ctx context.Context) {
	t.Helper()
	dropResearchFixture(t, s, ctx)
	if _, err := s.pool.Exec(ctx, `DROP TABLE model_call_contents; DROP FUNCTION guard_model_call_content();
 DROP TABLE model_calls; DROP FUNCTION guard_model_call_terminal(); DROP TABLE model_prices;
 DROP TABLE model_ledger_settings; ALTER TABLE missions DROP COLUMN record_model_content;
 DELETE FROM wscyber_schema_migrations WHERE version>=6`); err != nil {
		t.Fatal(err)
	}
}

func ledgerID(t *testing.T) string {
	t.Helper()
	id, e := newID()
	if e != nil {
		t.Fatal(e)
	}
	return strings.ReplaceAll(id, "-", "")
}
func ledgerAttempt(t *testing.T, s *Store, ctx context.Context) (domain.Thread, *domain.Attempt) {
	t.Helper()
	thread := fixtureThread(t, ctx, s)
	return thread, claimAttempt(t, ctx, s, thread.ID, 4)
}
func beginLedger(t *testing.T, s *Store, ctx context.Context) domain.ModelCall {
	t.Helper()
	thread, a := ledgerAttempt(t, s, ctx)
	c, e := s.BeginModelCall(ctx, domain.BeginModelCall{Call: domain.ModelCall{ID: ledgerID(t), ModelPriceKey: domain.ModelPriceKey{Source: "environment", Protocol: "openai_chat", Model: "fixture"}, Purpose: "research", MissionID: thread.MissionID, ThreadID: thread.ID, AttemptID: a.ID}})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func endLedger(t *testing.T, s *Store, ctx context.Context, c domain.ModelCall, usage domain.ModelUsage) domain.FinishModelCall {
	t.Helper()
	f := domain.FinishModelCall{ID: c.ID, Outcome: "valid_response", Attempted: true, FinishedAt: time.Now(), Usage: usage, Cost: modelledger.Estimate(c.Protocol, usage, false, c.Cost)}
	if e := s.FinishModelCall(ctx, f); e != nil {
		t.Fatal(e)
	}
	return f
}
func TestPostgresModelLedgerRecoveryTerminalAndOwnership(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	c := beginLedger(t, s, ctx)
	if _, e := s.GetModelCall(ctx, c.ID, "wrong"); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e := s.BeginModelCall(ctx, domain.BeginModelCall{Call: c}); e == nil {
		t.Fatal("同attempt不能重派发")
	}
	s.Close()
	s, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	if e = s.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	got, e := s.GetModelCall(ctx, c.ID, "")
	if e != nil || got.State != "interrupted" || got.Attempted != nil || got.Usage.Status != "missing" {
		t.Fatal(got, e)
	}
	if e = s.FinishModelCall(ctx, domain.FinishModelCall{ID: c.ID, Outcome: "valid_response", Attempted: true, FinishedAt: time.Now(), Usage: modelprovider.MissingUsage(), Cost: c.Cost}); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("迟到完成覆盖终态", e)
	}
	if a, e := s.ClaimThread(ctx, 4); e != nil || a != nil {
		t.Fatal("恢复重放", a, e)
	}
	c = beginLedger(t, s, ctx)
	f := endLedger(t, s, ctx, c, modelprovider.MissingUsage())
	if e = s.FinishModelCall(ctx, f); e != nil {
		t.Fatal("同回执应幂等", e)
	}
	f.Outcome = "invalid_response"
	if e = s.FinishModelCall(ctx, f); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("冲突回执", e)
	}
}
func TestPostgresModelUsageGapsPaginationAndDST(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	raws := []string{`{"usage":{"prompt_tokens":120,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":40}}}`, `{"usage":{"prompt_tokens":7}}`, `{}`, `{"usage":{"prompt_tokens":-1}}`}
	for _, raw := range raws {
		c := beginLedger(t, s, ctx)
		if _, e := s.pool.Exec(ctx, `UPDATE model_calls SET started_at='2026-03-08 07:00:00+00' WHERE id=$1`, c.ID); e != nil {
			t.Fatal(e)
		}
		u, _ := modelprovider.ParseUsage("openai_chat", []byte(raw))
		endLedger(t, s, ctx, c, u)
	}
	f, e := modelledger.ParseFilter(url.Values{"from": {"2026-03-08"}, "to": {"2026-03-09"}, "time_zone": {"America/New_York"}}, "", true, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	report, e := s.ModelUsage(ctx, f)
	if e != nil || report.Summary.Calls != "4" || report.Summary.MissingUsageCalls != "3" || report.Summary.InvalidUsageCalls != "1" || report.Summary.InputTokens != "127" || report.Summary.OutputTokens != "20" || report.Summary.TotalTokens != "140" || len(report.Daily) != 1 || report.Daily[0].Calls != "4" {
		t.Fatal(report, e)
	}
	// 边界在纽约春季23小时日：04:59Z前一天、04:00Z翌日，均不得混入。
	for _, stamp := range []string{"2026-03-08T04:59:59Z", "2026-03-09T04:00:00Z"} {
		c := beginLedger(t, s, ctx)
		if _, e := s.pool.Exec(ctx, `UPDATE model_calls SET started_at=$2 WHERE id=$1`, c.ID, mustLedgerTime(t, stamp)); e != nil {
			t.Fatal(e)
		}
		endLedger(t, s, ctx, c, modelprovider.MissingUsage())
	}
	report, e = s.ModelUsage(ctx, f)
	if e != nil || report.Summary.Calls != "4" {
		t.Fatal(report, e)
	}
	f.Limit = 2
	page, e := s.ListModelCalls(ctx, f)
	if e != nil || len(page.Items) != 2 || page.NextCursor == nil {
		t.Fatal(page, e)
	}
	f.Cursor = *page.NextCursor
	page2, e := s.ListModelCalls(ctx, f)
	if e != nil || len(page2.Items) != 2 || page2.NextCursor != nil {
		t.Fatal(page2, e)
	}
	seen := map[string]bool{}
	for _, c := range append(page.Items, page2.Items...) {
		if seen[c.ID] {
			t.Fatal("同时间分页重复")
		}
		seen[c.ID] = true
	}
	f.Purpose = "research"
	if _, e = s.ListModelCalls(ctx, f); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal("签名未绑定筛选", e)
	}
	f.Purpose = ""
	f.Cursor += "bad"
	if _, e = s.ListModelCalls(ctx, f); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal(e)
	}
	for _, stamp := range []string{"2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z"} {
		c := beginLedger(t, s, ctx)
		if _, e := s.pool.Exec(ctx, `UPDATE model_calls SET started_at=$2 WHERE id=$1`, c.ID, mustLedgerTime(t, stamp)); e != nil {
			t.Fatal(e)
		}
		endLedger(t, s, ctx, c, modelprovider.MissingUsage())
	}
	f, e = modelledger.ParseFilter(url.Values{"from": {"2026-11-01"}, "to": {"2026-11-02"}, "time_zone": {"America/New_York"}}, "", true, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	report, e = s.ModelUsage(ctx, f)
	if e != nil || report.Summary.Calls != "2" || report.Daily[0].Calls != "2" || f.To.Sub(f.From) != 25*time.Hour {
		t.Fatal(report, e)
	}
}
func mustLedgerTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestPostgresModelRecordingAtomicReplayAndContent(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	opts := domain.ExplorationModelOptions{EnvironmentConfigured: true, EnvironmentFingerprint: "fixture", ContentRecordingAvailable: true}
	input := startInput()
	input.RecordModelContent = true
	if _, e := s.StartExplorationWithModel(ctx, startKey, input, domain.ExplorationModelOptions{EnvironmentConfigured: true}); !errors.Is(e, domain.ErrUnavailable) {
		t.Fatal(e)
	}
	var n int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM missions`).Scan(&n); e != nil || n != 0 {
		t.Fatal("缺钥留下任务", n, e)
	}
	started, e := s.StartExplorationWithModel(ctx, startKey, input, opts)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := s.StartExplorationWithModel(ctx, startKey, input, domain.ExplorationModelOptions{})
	if e != nil || replay.Mission.ID != started.Mission.ID {
		t.Fatal(replay, e)
	}
	input.RecordModelContent = false
	if _, e = s.StartExplorationWithModel(ctx, startKey, input, opts); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("true须入幂等身份", e)
	}
	a, e := s.ClaimThread(ctx, 4)
	if e != nil || a == nil {
		t.Fatal(a, e)
	}
	v, e := modelconfig.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	if e != nil {
		t.Fatal(e)
	}
	id := ledgerID(t)
	cipher, e := v.SealContent(id, "input", []byte("BUSINESS_SENTINEL"))
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.BeginModelCall(ctx, domain.BeginModelCall{Call: domain.ModelCall{ID: id, ModelPriceKey: domain.ModelPriceKey{Source: "environment", Protocol: "openai_chat", Model: "fixture"}, Purpose: "research", MissionID: started.Mission.ID, ThreadID: a.ThreadID, AttemptID: a.ID, RecordModelContent: true}, Input: &domain.SealedCallContent{Kind: "input", Ciphertext: cipher, MediaType: "application/json", Complete: true}})
	if e != nil {
		t.Fatal(e)
	}
	var raw []byte
	if e = s.pool.QueryRow(ctx, `SELECT ciphertext FROM model_call_contents WHERE call_id=$1`, id).Scan(&raw); e != nil || bytes.Contains(raw, []byte("BUSINESS_SENTINEL")) {
		t.Fatal("数据库原文未加密", e)
	}
	encoded, _ := json.Marshal(c)
	if bytes.Contains(encoded, cipher) || bytes.Contains(encoded, []byte("Ciphertext")) {
		t.Fatal("列表含密文")
	}
	service := modelledger.New(s, v, nil)
	content, e := service.Content(ctx, id, started.Mission.ID)
	if e != nil || content.Input.State != "available" || content.Response.State != "pending" {
		t.Fatal(content, e)
	}
	if _, e = modelledger.New(s, nil, nil).Content(ctx, id, ""); !errors.Is(e, domain.ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE model_call_contents SET redacted=true WHERE call_id=$1`, id); e == nil {
		t.Fatal("原文可改写")
	}
	endLedger(t, s, ctx, c, modelprovider.MissingUsage())
	off := beginLedger(t, s, ctx)
	if _, e = s.pool.Exec(ctx, `INSERT INTO model_call_contents VALUES($1,'response',$2,'application/json',true,false)`, off.ID, cipher); e == nil {
		t.Fatal("关闭记录却保存原文")
	}
}
func TestPostgresModelPriceCASObservedIdentityAndSnapshot(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	service := modelledger.New(s, nil, nil)
	in := domain.ModelPriceWrite{ModelPriceKey: domain.ModelPriceKey{Source: "environment", Protocol: "openai_chat", Model: "fixture"}, PriceTerms: domain.PriceTerms{Currency: "USD", InputPerMillion: "2", OutputPerMillion: "8"}, Enabled: true}
	priceID := ledgerID(t)
	if _, _, e := service.SavePrice(ctx, priceID, in); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal("未观察组合不能定价", e)
	}
	c := beginLedger(t, s, ctx)
	endLedger(t, s, ctx, c, modelprovider.MissingUsage())
	price, created, e := service.SavePrice(ctx, priceID, in)
	if e != nil || !created || price.Revision != 1 {
		t.Fatal(price, e)
	}
	old := beginLedger(t, s, ctx)
	if old.Cost.PriceSnapshot == nil || old.Cost.PriceSnapshot.InputPerMillion != "2" {
		t.Fatal(old)
	}
	in.ExpectedRevision = 1
	in.InputPerMillion = "20"
	var successes, conflicts atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, e := service.SavePrice(ctx, priceID, in)
			if e == nil {
				successes.Add(1)
			} else if errors.Is(e, domain.ErrConflict) {
				conflicts.Add(1)
			} else {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != 7 {
		t.Fatal(successes.Load(), conflicts.Load())
	}
	got, e := s.GetModelCall(ctx, old.ID, "")
	if e != nil || got.Cost.PriceSnapshot.InputPerMillion != "2" {
		t.Fatal(got, e)
	}
	next := beginLedger(t, s, ctx)
	if next.Cost.PriceSnapshot.InputPerMillion != "20" {
		t.Fatal(next)
	}
	in.ExpectedRevision = 0
	if _, _, e = service.SavePrice(ctx, ledgerID(t), in); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("价格key重复", e)
	}
}
func TestPostgresModelLedgerPersistFailureZeroSendAndNoDecision(t *testing.T) {
	for _, stage := range []string{"begin", "finish", "lock"} {
		t.Run(stage, func(t *testing.T) {
			s, ctx, _ := postgresStore(t)
			thread, a := ledgerAttempt(t, s, ctx)
			var hits atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"OK"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)
			}))
			defer provider.Close()
			if stage != "lock" {
				event := "INSERT"
				if stage == "finish" {
					event = "UPDATE"
				}
				if _, e := s.pool.Exec(ctx, `CREATE FUNCTION ledger_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'SQL_SENTINEL'; END $$; CREATE TRIGGER ledger_fault BEFORE `+event+` ON model_calls FOR EACH ROW EXECUTE FUNCTION ledger_fault()`); e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e := s.lock.Exec(ctx, `SELECT pg_advisory_unlock_all()`); e != nil {
					t.Fatal(e)
				}
			}
			service := modelledger.New(s, nil, nil)
			doc := domain.ModelDocument{ModelConnection: domain.ModelConnection{Name: "fixture", Protocol: "openai_chat", BaseURL: provider.URL, Model: "fixture", Auth: "none", TimeoutSeconds: 2, MaxOutputTokens: 32}}
			ident := modelledger.Identity{Source: "environment", MissionID: thread.MissionID, ThreadID: thread.ID, AttemptID: a.ID}
			result, e := service.Complete(ctx, ident, doc, modelprovider.Input{User: "fixed", MaxOutputTokens: 32}, nil)
			if !errors.Is(e, domain.ErrUnavailable) || result.Text != "" || strings.Contains(e.Error(), "SQL_SENTINEL") {
				t.Fatal(result, e)
			}
			expected := int32(0)
			if stage == "finish" {
				expected = 1
			}
			if hits.Load() != expected {
				t.Fatal("外发不符", hits.Load())
			}
			if _, e = service.Complete(ctx, ident, doc, modelprovider.Input{User: "fixed", MaxOutputTokens: 32}, nil); !errors.Is(e, domain.ErrUnavailable) || hits.Load() != expected {
				t.Fatal("停止后重放", e)
			}
			var n int
			if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM model_calls`).Scan(&n); e != nil || n != int(expected) {
				t.Fatal(n, e)
			}
		})
	}
}
