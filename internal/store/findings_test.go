package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"wscyber/internal/domain"
	"wscyber/internal/modelprovider"
)

func findingProposal(ref domain.FindingReference) domain.FindingProposal {
	return domain.FindingProposal{Content: domain.FindingContent{Title: "候选：实际材料差异", Description: "FINDING_BODY_未独立验证", Steps: []domain.FindingStep{{Text: "读取已保存材料，不重放", EvidenceIndices: []int{0}}}, Remediation: "核对访问控制", Limitations: "关联真实性不证明主张成立", Evidence: []domain.FindingReference{ref}}}
}
func findingCall(t *testing.T, s *Store, ctx context.Context, th domain.Thread) (*domain.Attempt, domain.Snapshot, domain.ModelCall) {
	t.Helper()
	a := claimAttempt(t, ctx, s, th.ID, 100)
	snap, e := s.AssembleResearchSnapshot(ctx, th.MissionID, th.ID, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	in := contextCall(t, th, a, snap)
	c, e := s.BeginModelCall(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	endLedger(t, s, ctx, c, modelprovider.MissingUsage())
	return a, snap, c
}
func finishFindingAttempt(t *testing.T, s *Store, ctx context.Context, a *domain.Attempt) {
	t.Helper()
	if e := s.FinishAttempt(ctx, a.ID, "succeeded", "blocked", "有限研究成果已保存", "继续核对材料"); e != nil {
		t.Fatal(e)
	}
}
func TestD1PersistenceHistoryEvidenceAndRetry(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	action, e := s.ProposeAction(ctx, th.ID, "", domain.ActionInput{Kind: "http", Method: "GET", URL: "http://127.0.0.1:9/fixture", Reason: "只测持久化，不发送"})
	if e != nil {
		t.Fatal(e)
	}
	claimed, e := s.ClaimAction(ctx)
	if e != nil || claimed == nil {
		t.Fatal(e)
	}
	if e = s.FinishAction(ctx, action.ID, "succeeded", domain.ActionResult{StatusCode: 200, Body: []byte("ORIGINAL_HTTP_BYTES"), Truncated: true}, ""); e != nil {
		t.Fatal(e)
	}
	a, snap, c := findingCall(t, s, ctx, th)
	if snap.Assembly.Policy != domain.FindingsContextPolicy || len(snap.Assembly.Windows) != 4 {
		t.Fatal("policy")
	}
	p := findingProposal(domain.FindingReference{Kind: "action", ID: action.ID, Relation: "supporting"})
	r, e := s.ApplyFinding(ctx, a.ID, p)
	if e != nil {
		t.Fatal(e)
	}
	if r.Status != "candidate" || r.Number != "WSC-000001" || r.CallID != c.ID || r.EventID == "" || !r.Evidence[0].Truncated {
		t.Fatal("saved facts", r)
	}
	before, _ := json.Marshal(r)
	// 多次同Attempt只回放原修订，不产生第二编号或事件。
	for range 3 {
		got, e := s.ApplyFinding(ctx, a.ID, p)
		if e != nil || got.ID != r.ID {
			t.Fatal("retry", e)
		}
	}
	changed := p
	changed.Content.Title = "异载荷"
	if _, e = s.ApplyFinding(ctx, a.ID, changed); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("payload conflict", e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE finding_revisions SET content=content WHERE id=$1`, r.ID); e == nil {
		t.Fatal("mutable history")
	}
	if _, e = s.pool.Exec(ctx, `DELETE FROM finding_revisions WHERE id=$1`, r.ID); e == nil {
		t.Fatal("deleted history")
	}
	if _, e = s.pool.Exec(ctx, `UPDATE findings SET number=number+1 WHERE id=$1`, r.FindingID); e == nil {
		t.Fatal("changed identity")
	}
	source, e := s.FindingEvidenceContent(ctx, th.MissionID, r.FindingID, r.ID, 0)
	if e != nil || source.Action == nil || !source.MatchesSnapshot || string(source.Action.Result.Body) != "ORIGINAL_HTTP_BYTES" {
		t.Fatal("original", e)
	}
	other := fixtureThread(t, ctx, s)
	if _, e = s.GetFinding(ctx, other.MissionID, r.FindingID, ""); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("foreign finding", e)
	}
	if _, e = s.FindingEvidenceContent(ctx, other.MissionID, r.FindingID, r.ID, 0); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal("foreign original", e)
	}
	finishFindingAttempt(t, s, ctx, a)
	// 原文关闭的研究成果与原件关联保留，但没有provider/prompt归档行。
	var contents int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM model_call_contents WHERE call_id=$1`, c.ID).Scan(&contents); e != nil || contents != 0 {
		t.Fatal("raw bypass", contents, e)
	}
	a2, snap2, _ := findingCall(t, s, ctx, th)
	if len(snap2.Findings) != 1 || snap2.Findings[0].ID != r.ID {
		t.Fatal("candidate absent from real assembly")
	}
	p.FindingID = r.FindingID
	p.ExpectedRevision = 1
	p.Content.Description = "FINDING_UPDATE_保留同编号"
	p.Content.Evidence[0].Relation = "counterevidence"
	r2, e := s.ApplyFinding(ctx, a2.ID, p)
	if e != nil || r2.FindingID != r.FindingID || r2.Number != r.Number || r2.Revision != 2 || r2.ID == r.ID {
		t.Fatal("update", e, r2)
	}
	old, e := s.GetFinding(ctx, th.MissionID, r.FindingID, r.ID)
	after, _ := json.Marshal(old)
	if e != nil || string(before) != string(after) {
		t.Fatal("old revision changed", e)
	}
	history, e := s.FindingHistory(ctx, th.MissionID, r.FindingID, domain.FindingQuery{Limit: 1})
	if e != nil || len(history.Items) != 1 || history.Items[0].ID != r2.ID || history.NextCursor == "" {
		t.Fatal("history first", e)
	}
	second, e := s.FindingHistory(ctx, th.MissionID, r.FindingID, domain.FindingQuery{Limit: 1, Cursor: history.NextCursor})
	if e != nil || len(second.Items) != 1 || second.Items[0].ID != r.ID || second.NextCursor != "" {
		t.Fatal("history second", e)
	}
	if _, e = s.FindingHistory(ctx, other.MissionID, r.FindingID, domain.FindingQuery{Cursor: history.NextCursor}); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal("cursor binding", e)
	}
	events, e := s.ListEvents(ctx, th.MissionID, 0)
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, event := range events {
		if strings.HasPrefix(event.Kind, "finding.") {
			n++
			if event.FindingID != r.FindingID || event.FindingRevisionID == "" || strings.Contains(event.Message, "FINDING_") {
				t.Fatal("event facts")
			}
		}
	}
	if n != 2 {
		t.Fatal("events", n)
	}
	finishFindingAttempt(t, s, ctx, a2)
}
func TestD1RejectUnseenForeignChangedAndWrongAttempt(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	other := fixtureThread(t, ctx, s)
	own, e := s.AddObservation(ctx, domain.Observation{ThreadID: th.ID, Kind: "operator_note", Content: "原件"})
	if e != nil {
		t.Fatal(e)
	}
	foreign, e := s.AddObservation(ctx, domain.Observation{ThreadID: other.ID, Kind: "operator_note", Content: "他项目"})
	if e != nil {
		t.Fatal(e)
	}
	a, _, _ := findingCall(t, s, ctx, th)
	later, e := s.AddObservation(ctx, domain.Observation{ThreadID: th.ID, Kind: "operator_note", Content: "装配后出现"})
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{foreign.ID, later.ID, mustID(t)} {
		if _, e = s.ApplyFinding(ctx, a.ID, findingProposal(domain.FindingReference{Kind: "observation", ID: id, Relation: "supporting"})); e == nil {
			t.Fatal("accepted fabricated/unseen/foreign")
		}
	}
	p := findingProposal(domain.FindingReference{Kind: "observation", ID: own.ID, Relation: "supporting"})
	p.FindingID = mustID(t)
	p.ExpectedRevision = 1
	if _, e = s.ApplyFinding(ctx, a.ID, p); e == nil {
		t.Fatal("invented update")
	}
	p.FindingID = ""
	p.ExpectedRevision = 0
	// 模拟原材料被非API写入改变：hash冲突不把新内容冒充模型曾读内容。
	if _, e = s.pool.Exec(ctx, `UPDATE observations SET content='changed' WHERE id=$1`, own.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ApplyFinding(ctx, a.ID, p); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("changed input accepted", e)
	}
	finishFindingAttempt(t, s, ctx, a)
	a2 := claimAttempt(t, ctx, s, other.ID, 20)
	if _, e = s.ApplyFinding(ctx, a2.ID, p); e == nil {
		t.Fatal("no valid model call")
	}
	page, e := s.ListFindings(ctx, th.MissionID, domain.FindingQuery{})
	if e != nil || len(page.Items) != 0 {
		t.Fatal("rejected decisions persisted", e)
	}
}
func TestD1ConcurrentNumberAndSameAttemptIdempotency(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	type work struct {
		a *domain.Attempt
		p domain.FindingProposal
	}
	jobs := []work{}
	for i := 0; i < 8; i++ {
		th := fixtureThread(t, ctx, s)
		o, e := s.AddObservation(ctx, domain.Observation{ThreadID: th.ID, Kind: "operator_note", Content: fmt.Sprint("材料", i)})
		if e != nil {
			t.Fatal(e)
		}
		a, _, _ := findingCall(t, s, ctx, th)
		jobs = append(jobs, work{a, findingProposal(domain.FindingReference{Kind: "observation", ID: o.ID, Relation: "supporting"})})
	}
	var wg sync.WaitGroup
	results := make(chan domain.FindingRevision, 16)
	errs := make(chan error, 16)
	for _, j := range jobs {
		for range 2 {
			wg.Add(1)
			go func(j work) { defer wg.Done(); v, e := s.ApplyFinding(ctx, j.a.ID, j.p); results <- v; errs <- e }(j)
		}
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	numbers := map[string]string{}
	ids := map[string]bool{}
	for v := range results {
		if old, ok := numbers[v.Number]; ok && old != v.FindingID {
			t.Fatal("reused number")
		}
		numbers[v.Number] = v.FindingID
		ids[v.ID] = true
	}
	if len(numbers) != 8 || len(ids) != 8 {
		t.Fatal("duplicates", len(numbers), len(ids))
	}
}
func TestD1SavedCandidateSurvivesUnfinishedAttemptAndRecovery(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	o, e := s.AddObservation(ctx, domain.Observation{ThreadID: th.ID, Kind: "operator_note", Content: "保留"})
	if e != nil {
		t.Fatal(e)
	}
	a, _, _ := findingCall(t, s, ctx, th)
	p := findingProposal(domain.FindingReference{Kind: "observation", ID: o.ID, Relation: "limitation"})
	saved, e := s.ApplyFinding(ctx, a.ID, p)
	if e != nil {
		t.Fatal(e)
	}
	// 不执行FinishAttempt，模拟成果提交后进程退出；不触发新模型/目标请求。
	s.Close()
	reopened, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if e = reopened.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	snap, e := reopened.Snapshot(ctx, th.ID)
	if e != nil || snap.Attempts[0].Status != "interrupted" {
		t.Fatal("recovery", e)
	}
	again, e := reopened.ApplyFinding(ctx, a.ID, p)
	if e != nil || again.ID != saved.ID || again.Number != saved.Number {
		t.Fatal("retry after recovery", e)
	}
	if _, e = reopened.SetThreadControl(ctx, th.ID, "paused", false); e != nil {
		t.Fatal(e)
	}
	got, e := reopened.GetFinding(ctx, th.MissionID, saved.FindingID, "")
	if e != nil || got.ID != saved.ID {
		t.Fatal("pause hid candidate", e)
	}
}
func TestD1BoundedCandidatesAndInheritedEvidence(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	o, e := s.AddObservation(ctx, domain.Observation{ThreadID: th.ID, Kind: "operator_note", Content: "引用原件"})
	if e != nil {
		t.Fatal(e)
	}
	var latest domain.FindingRevision
	for i := 0; i < 12; i++ {
		a, _, _ := findingCall(t, s, ctx, th)
		p := findingProposal(domain.FindingReference{Kind: "observation", ID: o.ID, Relation: "supporting"})
		p.Content.Title = fmt.Sprintf("不同候选%d", i)
		latest, e = s.ApplyFinding(ctx, a.ID, p)
		if e != nil {
			t.Fatal(e)
		}
		finishFindingAttempt(t, s, ctx, a)
	}
	// 原引用不在32观察窗口，但通过已装配候选的固定引用仍可保留。
	for i := 0; i < 40; i++ {
		if _, e = s.AddObservation(ctx, domain.Observation{ThreadID: th.ID, Kind: "operator_note", Content: fmt.Sprint("新观察", i)}); e != nil {
			t.Fatal(e)
		}
	}
	a, snap, c := findingCall(t, s, ctx, th)
	if len(snap.Findings) != 8 || !snap.Assembly.Windows[3].OlderExists {
		t.Fatal("unbounded candidates", len(snap.Findings))
	}
	for _, x := range snap.Observations {
		if x.ID == o.ID {
			t.Fatal("old source unexpectedly direct")
		}
	}
	p := findingProposal(domain.FindingReference{Kind: "observation", ID: o.ID, Relation: "counterevidence"})
	p.FindingID = latest.FindingID
	p.ExpectedRevision = 1
	next, e := s.ApplyFinding(ctx, a.ID, p)
	if e != nil || next.Revision != 2 || next.Evidence[0].ContentSHA256 != latest.Evidence[0].ContentSHA256 {
		t.Fatal("inherit evidence", e)
	}
	audit, e := s.GetModelCallContext(ctx, c.ID, th.MissionID)
	if e != nil || len(audit.Assembly.Windows) != 4 {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(audit)
	if strings.Contains(string(raw), "FINDING_BODY_") {
		t.Fatal("body in audit")
	}
	page, e := s.ListFindings(ctx, th.MissionID, domain.FindingQuery{Limit: 5})
	if e != nil || len(page.Items) != 5 || page.NextCursor == "" {
		t.Fatal("page", e)
	}
	nextPage, e := s.ListFindings(ctx, th.MissionID, domain.FindingQuery{Limit: 5, Cursor: page.NextCursor})
	if e != nil || len(nextPage.Items) != 5 || nextPage.Items[0].ID == page.Items[0].ID {
		t.Fatal("page2", e)
	}
	if _, e = s.ListFindings(ctx, th.MissionID, domain.FindingQuery{Limit: 51}); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal("limit", e)
	}
	if _, e = s.ListFindings(ctx, th.MissionID, domain.FindingQuery{Cursor: page.NextCursor + "="}); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal("cursor", e)
	}
}
