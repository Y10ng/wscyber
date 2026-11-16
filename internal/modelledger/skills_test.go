package modelledger

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"wscyber/internal/domain"
	"wscyber/internal/modelprovider"
)

func TestSkillFailureStopsSubsequentDispatch(t *testing.T) {
	var hits atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer provider.Close()
	repo := &callRepo{}
	service := New(repo, nil, nil)
	service.StopDispatch()
	service.StopDispatch()
	select {
	case <-service.Failed():
	default:
		t.Fatal("未发出持久失败通知")
	}
	doc := domain.ModelDocument{ModelConnection: domain.ModelConnection{Name: "fixture", Protocol: "openai_chat", BaseURL: provider.URL, Model: "fake", Auth: "none", TimeoutSeconds: 1, MaxOutputTokens: 32}}
	id := Identity{Source: "environment", MissionID: "mission", ThreadID: "thread", AttemptID: "attempt", SkillLoadIDs: []string{"11111111-1111-1111-1111-111111111111"}}
	result, err := service.Complete(context.Background(), id, doc, modelprovider.Input{User: "fixed skill", MaxOutputTokens: 32}, nil)
	if !errors.Is(err, domain.ErrUnavailable) || result.Attempted || repo.starts != 0 || hits.Load() != 0 {
		t.Fatal("持久失败后仍Begin或外发", result, err, repo.starts, hits.Load())
	}
	var absent *Service
	absent.StopDispatch()
}
