package integration_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"wscyber/internal/domain"
	"wscyber/internal/execution"
	"wscyber/internal/policy"
)

func TestHTTPDefaultReleaseAndAutomaticResearch(t *testing.T) {
	requireIntegrationDatabase(t)
	for _, withModel := range []bool{false, true} {
		name := "manual-request"
		if withModel {
			name = "scripted-research"
		}
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int64
			body := []byte("default-release-local-evidence")
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				_, _ = w.Write(body)
			}))
			t.Cleanup(target.Close)
			var model domain.Model
			script := &scriptedModel{url: target.URL + "/status", body: body, note: "消费已保存观察"}
			if withModel {
				model = script
			}
			h := newHarness(t, model, execution.Config{})
			_, thread := h.createThread(t, target.URL)
			if withModel {
				h.call(t, "POST", "/api/threads/"+thread.ID+"/observations", map[string]string{"content": script.note}, http.StatusCreated, nil)
				h.call(t, "POST", "/api/threads/"+thread.ID+"/control", map[string]string{"command": "run"}, http.StatusOK, nil)
			} else {
				var action domain.Action
				h.call(t, "POST", "/api/threads/"+thread.ID+"/actions", domain.ActionInput{Kind: "http", Method: "GET", URL: target.URL + "/status", Reason: "普通 HTTP 应自动执行"}, http.StatusCreated, &action)
				if action.Status != "approved" || !strings.Contains(action.DecisionReason, policy.Version) {
					t.Fatal("新普通 HTTP 必须带策略依据放行", action)
				}
			}
			snapshot := h.waitSnapshot(t, thread.ID, "无人工审批的执行与接续", func(s domain.Snapshot) bool {
				if withModel {
					return s.Thread.Status == "concluded" || s.Thread.Status == "blocked"
				}
				return len(s.Actions) == 1 && finishedAction(s, s.Actions[0].ID)
			})
			if len(snapshot.Actions) != 1 || snapshot.Actions[0].Status != "succeeded" || snapshot.Actions[0].Result == nil || !bytes.Equal(snapshot.Actions[0].Result.Body, body) || hits.Load() != 1 {
				t.Fatal("未自动执行并保留唯一真实回执", snapshot)
			}
			if withModel {
				calls, sawNote, sawBody, problem := script.state()
				if calls != 2 || !sawNote || !sawBody || problem != "" || snapshot.Thread.Status != "concluded" || snapshot.Attempts[0].Status != "succeeded" {
					t.Fatal("自动放行后研究未正确消费证据", snapshot, problem)
				}
			}
		})
	}
}
