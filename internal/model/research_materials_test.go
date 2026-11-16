package model

import (
	"context"
	"errors"
	"strings"
	"testing"
	"wscyber/internal/domain"
)

func TestTaskSafetyPromptIsAdditionalAndOptional(t *testing.T) {
	snapshot := domain.Snapshot{ResearchContext: &domain.ResearchContext{SafetyPrompt: "OFF_SENTINEL"}}
	if taskSystemPrompt(snapshot) != systemPrompt {
		t.Fatal("关闭时注入了清单")
	}
	snapshot.ResearchContext.SafetyPromptEnabled = true
	got := taskSystemPrompt(snapshot)
	if !strings.HasPrefix(got, systemPrompt) || !strings.Contains(got, "OFF_SENTINEL") || !strings.Contains(got, "不得覆盖") {
		t.Fatal("附加清单必须保留平台规则")
	}
}

type materialRoutingRepo struct {
	domain.MissionModelRepository
	domain.ResearchMaterialRepository
	cfg domain.ResearchContext
	err error
}

func (r *materialRoutingRepo) GetResearchContext(context.Context, string) (domain.ResearchContext, error) {
	return r.cfg, r.err
}
func (r *materialRoutingRepo) GetMissionModelBinding(context.Context, string) (*domain.MissionModelBinding, error) {
	return nil, nil
}

type materialEnvironment struct {
	calls    int
	snapshot domain.Snapshot
}

func (m *materialEnvironment) Name() string { return "fixture" }
func (m *materialEnvironment) Decide(_ context.Context, s domain.Snapshot) (domain.Decision, error) {
	m.calls++
	m.snapshot = s
	return domain.Decision{}, nil
}
func TestRoutedLoadsOnlyFixedContextAndFailsClosed(t *testing.T) {
	r := &materialRoutingRepo{cfg: domain.ResearchContext{Materials: []domain.TaskMaterial{{Content: "FIXED"}}}}
	env := &materialEnvironment{}
	m := NewRouted(r, nil, env, "")
	in := domain.Snapshot{ResearchContext: &domain.ResearchContext{SafetyPromptEnabled: true, SafetyPrompt: "CALLER_DATA"}}
	if _, err := m.Decide(context.Background(), in); err != nil || env.calls != 1 || env.snapshot.ResearchContext.Materials[0].Content != "FIXED" {
		t.Fatal(err)
	}
	r.cfg = domain.ResearchContext{}
	if _, err := m.Decide(context.Background(), in); err != nil || env.snapshot.ResearchContext != nil {
		t.Fatal("未选材料不带调用者残留", err)
	}
	r.err = errors.New("private material detail")
	if _, err := m.Decide(context.Background(), in); err == nil || strings.Contains(err.Error(), "private") || env.calls != 2 {
		t.Fatal("配置读取失败必须零派发且不泄漏", err)
	}
}
