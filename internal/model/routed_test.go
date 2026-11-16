package model

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
)

type routeRepo struct {
	domain.MissionModelRepository
	domain.ModelConfigRepository
	binding *domain.MissionModelBinding
	records map[int64]domain.SealedModelConfig
	current int64
	enabled bool
}

func (r *routeRepo) GetMissionModelBinding(context.Context, string) (*domain.MissionModelBinding, error) {
	return r.binding, nil
}
func (r *routeRepo) GetModelConfig(_ context.Context, _ string, revision int64) (domain.SealedModelConfig, error) {
	if revision == 0 {
		revision = r.current
	}
	v, ok := r.records[revision]
	if !ok {
		return v, domain.ErrNotFound
	}
	v.Enabled = r.enabled
	return v, nil
}
func (r *routeRepo) SaveModelConfig(_ context.Context, in domain.SealedModelConfig, expected int64) (domain.SealedModelConfig, error) {
	if expected != r.current {
		return in, domain.ErrConflict
	}
	r.records[in.Revision] = in
	r.current = in.Revision
	r.enabled = in.Enabled
	return in, nil
}

type routeEnvironment struct{ calls int }

func (m *routeEnvironment) Name() string { return "explicit-environment" }
func (m *routeEnvironment) Decide(context.Context, domain.Snapshot) (domain.Decision, error) {
	m.calls++
	return domain.Decision{Kind: "conclude", Summary: "环境兼容"}, nil
}
func routedFixture(t *testing.T, base, protocol string) (*Routed, *routeRepo, *modelconfig.Manager, modelconfig.ModelConfigEdit) {
	t.Helper()
	const id = "00112233445566778899aabbccddeeff"
	r := &routeRepo{binding: &domain.MissionModelBinding{MissionID: "fixture", Source: "managed", ConfigID: id, ConfigRevision: 1}, records: map[int64]domain.SealedModelConfig{}}
	v, err := modelconfig.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{21}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	m := modelconfig.New(r, v)
	key := "fixture-secret"
	in := modelconfig.ModelConfigEdit{ModelConnection: domain.ModelConnection{Name: "研究配置", Model: "bound-fixture", Protocol: protocol, BaseURL: base, Auth: "bearer", TimeoutSeconds: 1, MaxOutputTokens: 128}, Enabled: true, APIKey: &key, Headers: []modelconfig.HeaderEntry{}, Parameters: []modelconfig.ParameterEntry{}}
	if _, err := m.Edit(context.Background(), id, in); err != nil {
		t.Fatal(err)
	}
	return NewRouted(r, m, nil, ""), r, m, in
}
func TestRoutedTwoProtocolsRealDecisionAndDisabled(t *testing.T) {
	for _, protocol := range []string{"openai_chat", "anthropic_messages"} {
		t.Run(protocol, func(t *testing.T) {
			var hits atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				body, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(body), "mission-data-marker") || !strings.Contains(string(body), "outcome_unknown") || !strings.Contains(string(body), "bound-fixture") {
					t.Error("未使用原研究prompt/快照/绑定模型")
				}
				if r.Header.Get("Authorization") != "Bearer fixture-secret" {
					t.Error("未使用配置认证")
				}
				w.Header().Set("Content-Type", "application/json")
				text, _ := json.Marshal(`{"kind":"conclude","summary":"本地证据已核对"}`)
				if protocol == "openai_chat" {
					fmt.Fprintf(w, `{"object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":%s}}]}`, text)
				} else {
					fmt.Fprintf(w, `{"type":"message","role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":%s}]}`, text)
				}
			}))
			defer provider.Close()
			routed, r, m, in := routedFixture(t, provider.URL, protocol)
			snapshot := domain.Snapshot{Mission: domain.Mission{ID: "fixture", Origin: "mission-data-marker"}}
			in.ExpectedRevision = 1
			in.Model = "edited-model"
			in.APIKey = nil
			if _, err := m.Edit(context.Background(), r.binding.ConfigID, in); err != nil {
				t.Fatal(err)
			}
			got, err := routed.Decide(context.Background(), snapshot)
			if err != nil || got.Kind != "conclude" || hits.Load() != 1 {
				t.Fatal(got, err, hits.Load())
			}
			in.ExpectedRevision = 2
			in.Enabled = false
			if _, err := m.Edit(context.Background(), r.binding.ConfigID, in); err != nil {
				t.Fatal(err)
			}
			environment := &routeEnvironment{}
			routed.environment = environment
			if _, err := routed.Decide(context.Background(), snapshot); err == nil || !strings.Contains(SafeErrorMessage(err), "禁用") {
				t.Fatal(err)
			}
			if hits.Load() != 1 || environment.calls != 0 {
				t.Fatal("禁用后不得发出请求或回退环境")
			}
		})
	}
}
func TestRoutedFailureBoundaries(t *testing.T) {
	for _, kind := range []string{"malformed_decision", "http_error", "disconnect", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			release := make(chan struct{})
			var hits atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				switch kind {
				case "http_error":
					w.WriteHeader(429)
					io.WriteString(w, `{"error":{"message":"fixture-secret"}}`)
				case "disconnect":
					conn, _, _ := w.(http.Hijacker).Hijack()
					conn.Close()
				case "timeout":
					select {
					case <-r.Context().Done():
					case <-release:
					}
				default:
					w.Header().Set("Content-Type", "application/json")
					text, _ := json.Marshal(`{"kind":"conclude","summary":"fixture-secret","summary":"duplicate"}`)
					fmt.Fprintf(w, `{"object":"chat.completion","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":%s}}]}`, text)
				}
			}))
			defer provider.Close()
			defer close(release)
			routed, _, _, _ := routedFixture(t, provider.URL, "openai_chat")
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err := routed.Decide(ctx, domain.Snapshot{Mission: domain.Mission{ID: "fixture"}})
			if err == nil || strings.Contains(SafeErrorMessage(err), "fixture-secret") || hits.Load() != 1 {
				t.Fatal(err, hits.Load())
			}
		})
	}
}
func TestRoutedLegacyAndEnvironmentIdentity(t *testing.T) {
	r := &routeRepo{}
	env := &routeEnvironment{}
	router := NewRouted(r, nil, env, "current")
	if _, err := router.Decide(context.Background(), domain.Snapshot{}); err != nil || env.calls != 1 {
		t.Fatal(err)
	}
	r.binding = &domain.MissionModelBinding{Source: "environment", EnvironmentFingerprint: "previous"}
	if _, err := router.Decide(context.Background(), domain.Snapshot{}); err == nil || env.calls != 1 {
		t.Fatal("环境变化必须阻塞", err)
	}
	if _, err := router.MissionModel(context.Background(), "fixture"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal(err)
	}
	r.binding = nil
	router.environment = nil
	if _, err := router.MissionModel(context.Background(), "fixture"); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal(err)
	}
}
