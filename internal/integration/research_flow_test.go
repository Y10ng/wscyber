package integration_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/execution"
)

// 此 Model 仅存在于 _test.go：两轮固定脚本，不配置供应商、凭据或真实模型调用。
// 脚本每轮都核对传入的持久 Snapshot，而不是靠自身对话记忆推进。
type scriptedModel struct {
	mu      sync.Mutex
	url     string
	note    string
	body    []byte
	calls   int
	sawNote bool
	sawBody bool
	problem string
}

func (*scriptedModel) Name() string { return "integration-only-two-step-script" }

func (m *scriptedModel) Decide(ctx context.Context, snapshot domain.Snapshot) (domain.Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	fail := func(problem string) (domain.Decision, error) {
		m.problem = problem
		return domain.Decision{}, fmt.Errorf("测试脚本输入不满足契约：%s", problem)
	}
	if err := ctx.Err(); err != nil {
		return fail("模型上下文已取消")
	}
	foundNote := false
	for _, observation := range snapshot.Observations {
		if observation.Kind == "operator_note" && observation.Content == m.note {
			foundNote = true
		}
	}
	if !foundNote {
		return fail("本轮 Snapshot 没有先前已保存的人工观察")
	}
	switch m.calls {
	case 1:
		if len(snapshot.Actions) != 0 {
			return fail("第一轮不应已有动作")
		}
		m.sawNote = true
		return domain.Decision{
			Kind: "action", Summary: "已消费先前观察，仅申请一个授权本地 HTTP 动作",
			NextStep: "人工审批并执行后，直接消费已保存的响应正文",
			Action: &domain.ActionInput{
				Kind: "http", Method: "GET", URL: m.url, Reason: "有限脚本验收：验证持久观察驱动的下一步",
			},
		}, nil
	case 2:
		if len(snapshot.Actions) != 1 {
			return fail("第二轮应只有原先提出的一个动作")
		}
		action := snapshot.Actions[0]
		if action.Status != "succeeded" || action.Result == nil || !bytes.Equal(action.Result.Body, m.body) {
			return fail("第二轮没有读取到真实已完成动作的原始正文")
		}
		evidence, ok := persistedResult(snapshot, action.ID)
		if !ok || !bytes.Equal(evidence.Body, m.body) {
			return fail("第二轮没有读取到与动作同事务保存的结果观察")
		}
		m.sawBody = true
		return domain.Decision{
			Kind: "conclude", Summary: "有限脚本已读取持久化正文并结束；这不是漏洞发现质量结论",
		}, nil
	default:
		return fail("两轮确定性决策之外发生了额外模型调用")
	}
}

func (m *scriptedModel) state() (calls int, sawNote, sawBody bool, problem string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls, m.sawNote, m.sawBody, m.problem
}

func TestResearchConsumesEvidenceWithoutApprovalPolling(t *testing.T) {
	requireIntegrationDatabase(t)
	responseBody := []byte("仅本地 fixture 的唯一正文：persisted-response-42\n")
	var hits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/delete-research-evidence" {
			http.Error(w, "本地脚本请求不匹配", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write(responseBody)
	}))
	t.Cleanup(target.Close)
	model := &scriptedModel{
		url: target.URL + "/delete-research-evidence", body: responseBody,
		note: "先前已保存材料：只验证明确授权的本地入口；正文是观察，不是漏洞确认。",
	}
	h := newHarness(t, model, execution.Config{})
	_, thread := h.createThread(t, target.URL)
	var note domain.Observation
	h.call(t, "POST", "/api/threads/"+thread.ID+"/observations", map[string]string{"content": model.note}, http.StatusCreated, &note)
	if note.Kind != "operator_note" || note.Content != model.note {
		t.Fatal("人工观察应先持久化为 operator_note", note)
	}
	assertStable(t, 100*time.Millisecond, func() {
		calls, _, _, _ := model.state()
		if calls != 0 || hits.Load() != 0 {
			t.Fatalf("创建方向和保存观察不能自动启动模型或发送动作：calls=%d hits=%d", calls, hits.Load())
		}
	})
	h.call(t, "POST", "/api/threads/"+thread.ID+"/control", map[string]string{"command": "run"}, http.StatusOK, nil)
	waiting := h.waitSnapshot(t, thread.ID, "模型保存动作并释放审批等待", func(s domain.Snapshot) bool {
		return s.Thread.Status == "waiting_approval" || s.Thread.Status == "blocked" || s.Thread.Status == "concluded"
	})
	calls, sawNote, _, problem := model.state()
	if waiting.Thread.Status != "waiting_approval" || len(waiting.Actions) != 1 || waiting.Actions[0].Status != "pending" || calls != 1 || !sawNote || problem != "" {
		t.Fatalf("第一轮未正确消费观察并等待审批：calls=%d sawNote=%t problem=%s snapshot=%+v", calls, sawNote, problem, waiting)
	}
	action := waiting.Actions[0]
	if len(waiting.Attempts) != 1 || waiting.Attempts[0].Status != "waiting_approval" || waiting.Attempts[0].EndedAt == nil || action.AttemptID != waiting.Attempts[0].ID {
		t.Fatal("审批等待应完成本轮 Attempt 并保留动作来源", waiting)
	}
	assertStable(t, 250*time.Millisecond, func() {
		calls, _, _, problem := model.state()
		if calls != 1 || hits.Load() != 0 || problem != "" {
			t.Fatalf("审批等待不应反复调用模型或发送请求：calls=%d hits=%d problem=%s", calls, hits.Load(), problem)
		}
		snapshot := h.snapshot(t, thread.ID)
		if snapshot.Thread.Status != "waiting_approval" || len(snapshot.Attempts) != 1 || findAction(t, snapshot, action.ID).Status != "pending" {
			t.Fatal("界面读取快照不能推进审批等待中的研究", snapshot)
		}
	})
	h.call(t, "POST", "/api/actions/"+action.ID+"/decision", map[string]string{
		"decision": "approved", "reason": "批准脚本提出的唯一请求",
	}, http.StatusOK, nil)
	completed := h.waitSnapshot(t, thread.ID, "下一轮消费动作正文并 conclude", func(s domain.Snapshot) bool {
		return s.Thread.Status == "concluded" || s.Thread.Status == "blocked"
	})
	calls, sawNote, sawBody, problem := model.state()
	if completed.Thread.Status != "concluded" || calls != 2 || !sawNote || !sawBody || problem != "" || hits.Load() != 1 {
		t.Fatalf("两轮有限研究链未完成：calls=%d sawNote=%t sawBody=%t hits=%d problem=%s snapshot=%+v", calls, sawNote, sawBody, hits.Load(), problem, completed)
	}
	if completed.Thread.StepCount != 2 || completed.Thread.AutoRun || len(completed.Attempts) != 2 || completed.Attempts[1].Status != "succeeded" || completed.Attempts[1].Summary == "" || len(completed.Observations) != 2 {
		t.Fatal("conclude 必须保留两轮执行、总结与先后证据，不伪造额外工作", completed)
	}
	assertStable(t, 100*time.Millisecond, func() {
		calls, _, _, problem := model.state()
		if calls != 2 || hits.Load() != 1 || problem != "" {
			t.Fatalf("已结束方向不应再研究或重放动作：calls=%d hits=%d problem=%s", calls, hits.Load(), problem)
		}
	})
}
