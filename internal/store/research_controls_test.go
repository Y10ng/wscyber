package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
	"wscyber/internal/domain"
)

func controlKey(n int) string { return fmt.Sprintf("c2b-key-%024d", n) }
func TestC2BControlReceipts(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	validate := func(context.Context) error { return nil }
	run, err := s.ControlResearch(ctx, controlKey(1), th.MissionID, th.ID, "run", validate)
	if err != nil || !run.Thread.AutoRun {
		t.Fatal(run, err)
	}
	attempt, err := s.ClaimThread(ctx, 8)
	if err != nil || attempt == nil {
		t.Fatal(err)
	}
	pause, err := s.ControlResearch(ctx, controlKey(2), th.MissionID, th.ID, "pause", nil)
	if err != nil || !pause.Thread.PauseRequested || pause.Thread.Status != "running" || pause.Thread.AutoRun {
		t.Fatal(pause, err)
	}
	if a, e := s.ClaimThread(ctx, 8); e != nil || a != nil {
		t.Fatal(a, e)
	}
	if _, err = s.ControlResearch(ctx, controlKey(3), th.MissionID, th.ID, "resume", nil); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if err = s.FinishAttempt(ctx, attempt.ID, "succeeded", "ready", "fixture", "next"); err != nil {
		t.Fatal(err)
	}
	replay, err := s.ControlResearch(ctx, controlKey(1), th.MissionID, th.ID, "run", func(context.Context) error { t.Fatal("replay revalidated"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(run)
	b, _ := json.Marshal(replay)
	if string(a) != string(b) {
		t.Fatal("fixed response changed")
	}
	now, _ := s.GetThread(ctx, th.ID)
	if !now.PauseRequested || now.Status != "paused" || now.StepCount != 1 {
		t.Fatal(now)
	}
	res, err := s.ControlResearch(ctx, controlKey(4), th.MissionID, th.ID, "resume", nil)
	if err != nil || res.Thread.PauseRequested || res.Thread.AutoRun || res.Thread.StepCount != 1 {
		t.Fatal(res, err)
	}
	if _, err = s.ControlResearch(ctx, controlKey(4), th.MissionID, th.ID, "pause", nil); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	other := fixtureThread(t, ctx, s)
	if _, err = s.ControlResearch(ctx, controlKey(5), other.MissionID, th.ID, "pause", nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = s.GetOperation(ctx, controlKey(5)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("rejected operation persisted", err)
	}
	if _, err = s.ControlResearch(ctx, controlKey(6), th.MissionID, th.ID, "run", validate); err != nil {
		t.Fatal(err)
	}
	if a, err := s.ClaimThread(ctx, 1); err != nil || a != nil {
		t.Fatal("budget reset", a, err)
	}
	now, _ = s.GetThread(ctx, th.ID)
	if now.StepCount != 1 || now.Status != "blocked" {
		t.Fatal(now)
	}
}
func TestC2BSupplementAtomicityAndAssociation(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	in := domain.SupplementInput{Source: "人工本机记录", Content: "C2B_PRIVATE\r\n<script>文本</script>", ShareWithModel: true}
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	ids := make(chan string, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := s.AddSupplement(ctx, controlKey(20), th.MissionID, th.ID, in)
			errs <- e
			if v.Supplement != nil {
				ids <- v.Supplement.ID
			}
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	id := ""
	for v := range ids {
		if id != "" && id != v {
			t.Fatal("duplicate")
		}
		id = v
	}
	page, err := s.ListSupplements(ctx, th.MissionID, th.ID)
	if err != nil || len(page.Items) != 1 || page.Items[0].Content != "" {
		t.Fatal(page, err)
	}
	orig, err := s.GetSupplement(ctx, th.MissionID, th.ID, id)
	if err != nil || orig.Content != in.Content || orig.ByteSize != len(in.Content) {
		t.Fatal(orig, err)
	}
	snap, err := s.Snapshot(ctx, th.ID)
	raw, _ := json.Marshal(snap)
	if err != nil || strings.Contains(string(raw), "C2B_PRIVATE") {
		t.Fatal("snapshot leaked", err)
	}
	other := fixtureThread(t, ctx, s)
	if _, err = s.GetSupplement(ctx, other.MissionID, th.ID, id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	changed := in
	changed.Content = "changed"
	if _, err = s.AddSupplement(ctx, controlKey(20), th.MissionID, th.ID, changed); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	_, err = s.ControlResearch(ctx, controlKey(21), th.MissionID, th.ID, "run", func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := s.ClaimThread(ctx, 8)
	if err != nil || attempt == nil {
		t.Fatal(err)
	}
	call := domain.BeginModelCall{Call: domain.ModelCall{ID: strings.Repeat("a", 32), ModelPriceKey: domain.ModelPriceKey{Source: "environment", Protocol: "openai_chat", Model: "fixture"}, Purpose: "research", MissionID: th.MissionID, ThreadID: th.ID, AttemptID: attempt.ID}, SupplementIDs: []string{id}}
	foreign, err := s.AddSupplement(ctx, controlKey(22), other.MissionID, other.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	// 先测跨归属，避免被同Attempt最多一次调用的唯一约束提前拦截。
	foreignCall := call
	foreignCall.Call.ID = strings.Repeat("b", 32)
	foreignCall.SupplementIDs = []string{foreign.Supplement.ID}
	if _, err = s.BeginModelCall(ctx, foreignCall); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("foreign supplement must fail ownership lookup", err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM model_calls WHERE id=$1`, foreignCall.Call.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("orphan call", count, err)
	}
	if _, err = s.BeginModelCall(ctx, call); err != nil {
		t.Fatal(err)
	}
	calls, err := s.SupplementModelCalls(ctx, th.MissionID, th.ID, id, "", 25)
	if err != nil || len(calls.Items) != 1 || calls.Items[0].Attempted != nil {
		t.Fatal(calls, err)
	}
	if err = s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	calls, err = s.SupplementModelCalls(ctx, th.MissionID, th.ID, id, "", 25)
	if err != nil || calls.Items[0].Outcome != "interrupted" {
		t.Fatal(calls, err)
	}
	for i := 1; i < 16; i++ {
		if _, err = s.AddSupplement(ctx, controlKey(100+i), th.MissionID, th.ID, in); err != nil {
			t.Fatal(i, err)
		}
	}
	if _, err = s.AddSupplement(ctx, controlKey(200), th.MissionID, th.ID, in); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if _, err = s.GetOperation(ctx, controlKey(200)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}
func TestC2BSupplementCapacity(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	for i := 0; i < 4; i++ {
		_, err := s.AddSupplement(ctx, controlKey(300+i), th.MissionID, th.ID, domain.SupplementInput{Source: "fixture", Content: strings.Repeat("x", 16384), ShareWithModel: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddSupplement(ctx, controlKey(305), th.MissionID, th.ID, domain.SupplementInput{Source: "fixture", Content: "x", ShareWithModel: true}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	th = fixtureThread(t, ctx, s)
	for i := 0; i < 2; i++ {
		_, err := s.AddSupplement(ctx, controlKey(310+i), th.MissionID, th.ID, domain.SupplementInput{Source: "fixture", Content: strings.Repeat("<", 16384), ShareWithModel: true})
		if !errors.Is(err, domain.ErrConflict) {
			t.Fatal("serialized cap", err)
		}
	}
	page, err := s.ListSupplements(ctx, th.MissionID, th.ID)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("capacity rollback", page, err)
	}
	// 无副作用的取消请求不得写入回执。
	cancelCtx, cancel := context.WithTimeout(ctx, time.Nanosecond)
	defer cancel()
	_, err = s.ControlResearch(cancelCtx, controlKey(320), th.MissionID, th.ID, "pause", nil)
	if err == nil {
		t.Fatal("cancel accepted")
	}
}
