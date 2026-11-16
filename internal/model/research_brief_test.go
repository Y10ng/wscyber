package model

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"wscyber/internal/domain"
)

func TestResearchBriefCombinedBudgetAndReadFailureNoHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	env, err := New(Config{BaseURL: server.URL, APIKey: "local-fixture", Model: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
	share := true
	brief := &domain.ResearchBrief{BusinessRules: strings.Repeat("b", 8192), ShareWithModel: &share}
	repo := &materialRoutingRepo{cfg: domain.ResearchContext{ResearchBrief: brief}}
	routed := NewRouted(repo, nil, env, "")
	snapshot := domain.Snapshot{Observations: []domain.Observation{{Content: strings.Repeat("x", maxSnapshotBytes-4096)}}}
	if _, err := snapshotPrompt(snapshot); err != nil {
		t.Fatal("原快照单独应低于上限", err)
	}
	if _, err := routed.Decide(context.Background(), snapshot); !errors.Is(err, ErrContextTooLarge) || calls.Load() != 0 {
		t.Fatal("说明与快照组合超限必须发送前拒绝", err, calls.Load())
	}
	if len(snapshot.Observations[0].Content) != maxSnapshotBytes-4096 || len(brief.BusinessRules) != 8192 {
		t.Fatal("不得截断原件")
	}
	repo.err = errors.New("PRIVATE_BRIEF_READ_FAILURE")
	if _, err := routed.Decide(context.Background(), domain.Snapshot{}); err == nil || strings.Contains(err.Error(), "PRIVATE") || calls.Load() != 0 {
		t.Fatal("固定配置读取失败必须零HTTP，不回显正文", err, calls.Load())
	}
	repo.err = nil
	brief.ShareWithModel = nil
	if _, err := routed.Decide(context.Background(), domain.Snapshot{}); err == nil || calls.Load() != 0 {
		t.Fatal("配置完整性失败必须零HTTP", err, calls.Load())
	}
}

func TestRoutedResearchBriefOnlyAndNoShare(t *testing.T) {
	share := true
	brief := &domain.ResearchBrief{BusinessRules: "BRIEF_SENTINEL", ShareWithModel: &share}
	repo := &materialRoutingRepo{cfg: domain.ResearchContext{ResearchBrief: brief}}
	env := &materialEnvironment{}
	routed := NewRouted(repo, nil, env, "")
	caller := domain.Snapshot{ResearchContext: &domain.ResearchContext{ResearchBrief: &domain.ResearchBrief{BusinessRules: "CALLER_SENTINEL", ShareWithModel: &share}}}
	for _, sharing := range []bool{true, false} {
		*brief.ShareWithModel = sharing
		if _, err := routed.Decide(context.Background(), caller); err != nil {
			t.Fatal(err)
		}
		user, err := snapshotPrompt(env.snapshot)
		if err != nil || strings.Contains(user, "BRIEF_SENTINEL") != sharing || strings.Contains(user, "CALLER_SENTINEL") || strings.Contains(taskSystemPrompt(env.snapshot), "BRIEF_SENTINEL") {
			t.Fatal("only-brief/分享隔离/低信任边界", err)
		}
		if !sharing && env.snapshot.ResearchContext != nil {
			t.Fatal("false不应挂载空配置")
		}
	}
	repo.err = errors.New("private config detail")
	if _, err := routed.Decide(context.Background(), caller); err == nil || strings.Contains(err.Error(), "private") || env.calls != 2 {
		t.Fatal("读取失败发送前拒绝", err)
	}
	repo.err = nil
	brief.ShareWithModel = nil
	if _, err := routed.Decide(context.Background(), caller); err == nil || env.calls != 2 {
		t.Fatal("完整性失败发送前拒绝", err)
	}
}
