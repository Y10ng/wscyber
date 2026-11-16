package integration_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/execution"
)

func TestHTTPApprovalEvidenceAndScope(t *testing.T) {
	requireIntegrationDatabase(t)
	var hits, rejectedHits, outsideHits atomic.Int64
	type receivedRequest struct {
		method, uri, header string
		body                []byte
	}
	received := make(chan receivedRequest, 8)
	responseBody := []byte{'f', 'i', 'x', 't', 'u', 'r', 'e', 0, 255}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/rejected" {
			rejectedHits.Add(1)
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			http.Error(w, "本地 fixture 无法读取请求", http.StatusBadRequest)
			return
		}
		select {
		case received <- receivedRequest{r.Method, r.URL.RequestURI(), r.Header.Get("X-Wscyber-Fixture"), body}:
		default:
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Fixture", "local-evidence")
		_, _ = w.Write(responseBody)
	}))
	t.Cleanup(target.Close)
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		outsideHits.Add(1)
		_, _ = w.Write([]byte("未授权本地 fixture，不应收到请求"))
	}))
	t.Cleanup(outside.Close)
	h := newHarness(t, nil, execution.Config{})

	h.request(t, "GET", "/healthz", "", nil, http.StatusOK)
	h.request(t, "GET", "/api/missions", "", nil, http.StatusUnauthorized)
	h.request(t, "GET", "/api/missions", "wrong-test-token", nil, http.StatusUnauthorized)
	h.request(t, "POST", "/api/missions", "", map[string]any{"title": "未鉴权写入"}, http.StatusUnauthorized)
	var missions []domain.Mission
	h.call(t, "GET", "/api/missions", nil, http.StatusOK, &missions)
	if missions == nil || len(missions) != 0 {
		t.Fatal("未鉴权请求不能创建记录；初始列表应为 []", missions)
	}
	mission, thread := h.createThread(t, target.URL)
	h.call(t, "POST", "/api/threads/"+thread.ID+"/control", map[string]string{"command": "run"}, http.StatusServiceUnavailable, nil)
	empty := h.snapshot(t, thread.ID)
	if empty.Actions == nil || empty.Observations == nil || empty.Attempts == nil {
		t.Fatal("快照的空列表必须是 []", empty)
	}

	// 另一个本地端口也是另一个 origin；拒绝必须发生在任何目标发送之前。
	h.call(t, "POST", "/api/threads/"+thread.ID+"/actions", domain.ActionInput{
		Kind: "http", Method: "GET", URL: outside.URL + "/not-authorized", Reason: "验证越界零发送",
	}, http.StatusBadRequest, nil)
	if snapshot := h.snapshot(t, thread.ID); len(snapshot.Actions) != 0 {
		t.Fatal("越界请求不应生成可审批动作", snapshot.Actions)
	}

	var action domain.Action
	h.call(t, "POST", "/api/threads/"+thread.ID+"/actions", domain.ActionInput{
		Kind: "http", Method: "POST", URL: target.URL + "/delete?marker=local",
		Headers: map[string]string{"X-Wscyber-Fixture": "explicit-approval"}, Body: "approval-gated-request",
		Reason: "只访问明确授权的本地 fixture，验证真实请求与原字节保存",
	}, http.StatusCreated, &action)
	if action.Status != "pending" || len(action.RequestHash) != 64 || action.Result != nil {
		t.Fatal("提出动作必须先持久化不可变请求且不执行", action)
	}
	assertStable(t, 200*time.Millisecond, func() {
		if hits.Load() != 0 || outsideHits.Load() != 0 {
			t.Fatalf("批准前或越界动作产生发送：授权目标=%d，越界目标=%d", hits.Load(), outsideHits.Load())
		}
		if got := findAction(t, h.snapshot(t, thread.ID), action.ID); got.Status != "pending" {
			t.Fatal("未经审批的动作不能被认领", got)
		}
	})
	h.call(t, "POST", "/api/actions/"+action.ID+"/decision", map[string]string{
		"decision": "approved", "reason": "人工明确批准本次不可变请求",
	}, http.StatusOK, nil)
	snapshot := h.waitSnapshot(t, thread.ID, "HTTP 真实执行结果持久化", func(s domain.Snapshot) bool {
		return finishedAction(s, action.ID)
	})
	finished := findAction(t, snapshot, action.ID)
	if finished.Status != "succeeded" || finished.Result == nil {
		t.Fatalf("HTTP 执行失败：%+v", finished)
	}
	if hits.Load() != 1 || outsideHits.Load() != 0 || !bytes.Equal(finished.Result.Body, responseBody) || finished.Result.StatusCode != http.StatusOK || finished.Result.Truncated || finished.Result.EffectUnknown {
		t.Fatalf("真实响应或发送次数不符：hits=%d outside=%d result=%+v", hits.Load(), outsideHits.Load(), finished.Result)
	}
	if finished.RequestHash != action.RequestHash || finished.Result.ResolvedIP != "127.0.0.1" {
		t.Fatal("完成后必须保留原请求 hash 与真实连接 IP", finished)
	}
	if values := finished.Result.Headers["X-Fixture"]; len(values) != 1 || values[0] != "local-evidence" {
		t.Fatal("响应头未保存", finished.Result.Headers)
	}
	select {
	case got := <-received:
		if got.method != "POST" || got.uri != "/delete?marker=local" || got.header != "explicit-approval" || string(got.body) != "approval-gated-request" {
			t.Fatal("真实发送参数与获批请求不一致", got)
		}
	default:
		t.Fatal("目标未记录已完成动作的真实请求")
	}
	evidence, ok := persistedResult(snapshot, action.ID)
	if !ok || !bytes.Equal(evidence.Body, responseBody) || len(snapshot.Observations) != 1 {
		t.Fatal("真实结果与关联观察必须一起出现在 API Snapshot", snapshot)
	}
	if snapshot.Mission.ID != mission.ID || snapshot.Thread.ID != thread.ID || snapshot.Thread.Status != "ready" || snapshot.Thread.AutoRun || len(snapshot.Attempts) != 0 {
		t.Fatal("人工动作完成不能伪造模型执行或自动启用研究", snapshot)
	}

	var rejected domain.Action
	h.call(t, "POST", "/api/threads/"+thread.ID+"/actions", domain.ActionInput{
		Kind: "http", Method: "DELETE", URL: target.URL + "/rejected", Reason: "验证人工拒绝零发送",
	}, http.StatusCreated, &rejected)
	h.call(t, "POST", "/api/actions/"+rejected.ID+"/decision", map[string]string{
		"decision": "rejected", "reason": "明确不执行",
	}, http.StatusOK, nil)
	h.call(t, "POST", "/api/actions/"+rejected.ID+"/decision", map[string]string{
		"decision": "approved", "reason": "迟到相反决定不得生效",
	}, http.StatusConflict, nil)
	assertStable(t, 200*time.Millisecond, func() {
		if hits.Load() != 1 || rejectedHits.Load() != 0 || outsideHits.Load() != 0 {
			t.Fatalf("拒绝、越界或重放导致发送：总数=%d 拒绝=%d 越界=%d", hits.Load(), rejectedHits.Load(), outsideHits.Load())
		}
		snapshot := h.snapshot(t, thread.ID)
		got := findAction(t, snapshot, rejected.ID)
		if got.Status != "rejected" || got.Result != nil || len(snapshot.Observations) != 1 {
			t.Fatal("拒绝动作不应伪造执行结果或新观察", snapshot)
		}
	})
	var actions []domain.Action
	h.call(t, "GET", "/api/actions?mission_id="+mission.ID, nil, http.StatusOK, &actions)
	if len(actions) != 2 || actions[0].ID != action.ID || actions[1].ID != rejected.ID {
		t.Fatal("项目动作列表应保留成功与拒绝历史并稳定排序", actions)
	}
	var events []domain.Event
	h.call(t, "GET", "/api/events?mission_id="+mission.ID+"&after=0", nil, http.StatusOK, &events)
	var lastID int64
	started, ended := false, false
	for _, event := range events {
		if event.ID <= lastID || event.MissionID != mission.ID {
			t.Fatal("事件归属或游标排序无效", events)
		}
		lastID = event.ID
		started = started || event.Kind == "action.started"
		ended = ended || event.Kind == "action.finished"
	}
	if !started || !ended {
		t.Fatal("真实执行的认领与完成事件未持久化", events)
	}
}
