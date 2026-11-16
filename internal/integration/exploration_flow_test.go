package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/execution"
	"wscyber/internal/httpapi"
)

const explorationKey = "00112233-4455-4677-8899-aabbccddeeff"

// 仅测试：从用户材料读目标，发送一次本地 HTTP，再消费已保存证据结束。
type explorationModel struct {
	url   string
	fail  bool
	calls atomic.Int64
}

func (*explorationModel) Name() string { return "integration-only-exploration-script" }
func (m *explorationModel) Decide(_ context.Context, s domain.Snapshot) (domain.Decision, error) {
	call := m.calls.Add(1)
	if m.fail {
		return domain.Decision{}, fmt.Errorf("脚本模拟供应商不可用")
	}
	if s.Mission.Origin != "仅本地 fixture，输入尚待核验" || s.Thread.Question != s.Mission.Goal || len(s.Mission.AllowedOrigins) != 1 {
		return domain.Decision{}, fmt.Errorf("初始任务材料或授权不匹配")
	}
	if call == 1 && len(s.Actions) == 0 && len(s.Observations) == 0 {
		return domain.Decision{Kind: "action", Summary: "读取一次授权本地入口", NextStep: "消费真实响应", Action: &domain.ActionInput{Kind: "http", Method: "GET", URL: m.url, Reason: "本地可靠启动验收"}}, nil
	}
	if call == 2 && len(s.Actions) == 1 {
		a := s.Actions[0]
		result, ok := persistedResult(s, a.ID)
		if ok && a.Status == "succeeded" && bytes.Equal(result.Body, []byte("exploration-local-response")) {
			return domain.Decision{Kind: "conclude", Summary: "已读取真实响应；不是漏洞发现质量验证"}, nil
		}
	}
	return domain.Decision{}, fmt.Errorf("出现重复调用或缺少已保存证据")
}

func TestExplorationStartsAndConsumesEvidence(t *testing.T) {
	requireIntegrationDatabase(t)
	var hits atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("exploration-local-response"))
	}))
	t.Cleanup(target.Close)
	model := &explorationModel{url: target.URL + "/status"}
	h := newHarness(t, model, execution.Config{})
	input := domain.ExplorationInput{Description: "仅本地 fixture，输入尚待核验", Goal: "读取入口并验证证据回流", AllowedOrigins: []string{target.URL}}
	body := h.request(t, "POST", "/api/explorations", h.token, input, 202, explorationKey)
	var started domain.ExplorationStart
	if err := json.Unmarshal(body, &started); err != nil {
		t.Fatal(err)
	}
	if started.StartStatus != "accepted" || started.Thread.Status != "ready" || !started.Thread.AutoRun || started.Thread.StepCount != 0 {
		t.Fatal("启动回执不能伪装正在运行", started)
	}
	completed := h.waitSnapshot(t, started.Thread.ID, "自动探索读取真实证据并结束", func(s domain.Snapshot) bool { return s.Thread.Status == "concluded" || s.Thread.Status == "blocked" })
	if completed.Thread.Status != "concluded" || len(completed.Attempts) != 2 || len(completed.Actions) != 1 || hits.Load() != 1 || model.calls.Load() != 2 {
		t.Fatal(completed, hits.Load(), model.calls.Load())
	}
	var receipt domain.OperationReceipt
	h.call(t, "GET", "/api/operations/"+explorationKey, nil, 200, &receipt)
	if receipt.ResourceID != started.Mission.ID || receipt.ResponseStatus != 202 {
		t.Fatal(receipt)
	}
	// 模型已完成之后重提，得到原成功响应，不能重新开启方向。
	replayed := h.request(t, "POST", "/api/explorations", h.token, input, 202, explorationKey)
	if !bytes.Equal(body, replayed) {
		t.Fatal("回放内容不等于原提交响应", string(body), string(replayed))
	}
	assertStable(t, 150*time.Millisecond, func() {
		s := h.snapshot(t, started.Thread.ID)
		if s.Thread.Status != "concluded" || len(s.Attempts) != 2 || hits.Load() != 1 || model.calls.Load() != 2 {
			t.Fatal("同键重提触发重复执行", s)
		}
	})
}

func TestExplorationModelFailureAndMissingConfig(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(fmt.Sprintf("configured=%t", configured), func(t *testing.T) {
			var m domain.Model
			script := &explorationModel{fail: true}
			if configured {
				m = script
			}
			h := newHarness(t, m, execution.Config{})
			input := domain.ExplorationInput{Description: "本地故障验证", Goal: "不运行真实模型", AllowedOrigins: []string{"https://fixture.invalid"}}
			if !configured {
				h.request(t, "POST", "/api/explorations", h.token, input, 503, explorationKey)
				h.call(t, "GET", "/api/operations/"+explorationKey, nil, 404, nil)
				var missions []domain.Mission
				h.call(t, "GET", "/api/missions", nil, 200, &missions)
				if len(missions) != 0 {
					t.Fatal("未配置模型时不能留半个已启动任务", missions)
				}
				return
			}
			body := h.request(t, "POST", "/api/explorations", h.token, input, 202, explorationKey)
			var start domain.ExplorationStart
			if err := json.Unmarshal(body, &start); err != nil {
				t.Fatal(err)
			}
			s := h.waitSnapshot(t, start.Thread.ID, "模型故障如实阻塞", func(s domain.Snapshot) bool { return s.Thread.Status == "blocked" })
			if len(s.Attempts) != 1 || s.Attempts[0].Status != "failed" || s.Attempts[0].Error == "" || len(s.Actions) != 0 {
				t.Fatal(s)
			}
			h.request(t, "POST", "/api/explorations", h.token, input, 202, explorationKey)
			assertStable(t, 100*time.Millisecond, func() {
				if script.calls.Load() != 1 {
					t.Fatal("启动回放不能重新调用失败模型")
				}
			})
		})
	}
}

func TestExplorationLostHTTPResponseCanBeReconciled(t *testing.T) {
	repo, ctx := isolatedStore(t)
	token := "lost-response-local-test-token"
	handler := httpapi.New(repo, nil, httpapi.Config{Token: token, System: httpapi.SystemInfo{ModelConfigured: true}})
	var dropped atomic.Bool
	// 事务已成功但不发送任何响应字节，模拟浏览器失联；不运行调度器或目标。
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/explorations" && dropped.CompareAndSwap(false, true) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, r)
			if recorder.Code != 202 {
				t.Errorf("故障注入前提交未成功：%d", recorder.Code)
			}
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(api.Close)
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	h := &harness{ctx: ctx, api: api, token: token, client: &http.Client{Transport: transport, Timeout: 5 * time.Second}}
	input := domain.ExplorationInput{Description: "回执丢失本地测试", Goal: "保持唯一启动", AllowedOrigins: []string{"https://fixture.invalid"}}
	body, _ := json.Marshal(input)
	req, err := http.NewRequestWithContext(ctx, "POST", api.URL+"/api/explorations", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", explorationKey)
	res, err := h.client.Do(req)
	if err == nil {
		res.Body.Close()
		t.Fatal("应观察到响应丢失，而不是成功响应")
	}
	var receipt domain.OperationReceipt
	h.call(t, "GET", "/api/operations/"+explorationKey, nil, 200, &receipt)
	body = h.request(t, "POST", "/api/explorations", token, input, 202, explorationKey)
	var start domain.ExplorationStart
	if err := json.Unmarshal(body, &start); err != nil {
		t.Fatal(err)
	}
	if start.Mission.ID != receipt.ResourceID {
		t.Fatal(start, receipt)
	}
	missions, err := repo.ListMissions(ctx)
	if err != nil || len(missions) != 1 {
		t.Fatal(missions, err)
	}
	threads, err := repo.ListThreads(ctx, receipt.ResourceID)
	if err != nil || len(threads) != 1 {
		t.Fatal(threads, err)
	}
}
