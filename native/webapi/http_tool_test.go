package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent"
	"github.com/chainreactors/cyber/agent/provider"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/pkg/testutil/hosttest"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type toolEvidenceResult struct {
	EvidenceID       string `json:"evidence_id"`
	TaskID           string `json:"task_id"`
	SessionID        string `json:"session_id"`
	TurnID           string `json:"turn_id"`
	CallID           string `json:"call_id"`
	Status           string `json:"status"`
	Sent             *bool  `json:"sent"`
	HTTPStatus       int64  `json:"http_status"`
	Preview          string `json:"preview"`
	PreviewTruncated bool   `json:"preview_truncated"`
	CaptureTruncated bool   `json:"capture_truncated"`
}

func newHTTPToolFixture(t *testing.T, handler http.Handler) (*Capability, *httpTool, *Task, operation.Invocation, *atomic.Int32, string) {
	t.Helper()
	database := t.TempDir() + "/hub.sqlite"
	capability, err := NewCapability(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = capability.Close(context.Background()) })
	for _, model := range SchemaModule().Models {
		if _, err := capability.store.orm.NewCreateTable().Model(model).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	for _, index := range SchemaModule().Indexes {
		query := capability.store.orm.NewCreateIndex().Model(index.Model).Index(index.Name).Column(index.Columns...)
		if index.Expr != "" {
			query = capability.store.orm.NewCreateIndex().Model(index.Model).Index(index.Name).ColumnExpr(index.Expr)
		}
		if index.Unique {
			query = query.Unique()
		}
		if _, err := query.Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	var count atomic.Int32
	counting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(counting.Close)
	outcome, err := capability.store.Save(t.Context(), "task-http", "本机fixture", "验证真实请求证据", []string{originOf(t, counting.URL)})
	if err != nil {
		t.Fatal(err)
	}
	inv := operation.Invocation{SessionID: outcome.Task.InitialSessionID, TurnID: "turn-1", CallID: "call-1"}
	return capability, &httpTool{store: capability.store}, outcome.Task, inv, &count, database
}

func originOf(t *testing.T, raw string) string {
	t.Helper()
	origin, err := normalizeOrigin(raw)
	if err != nil {
		t.Fatal(err)
	}
	return origin
}

func executeHTTP(t *testing.T, tool *httpTool, inv operation.Invocation, method, rawURL, body string, extra string) (*aop.ToolResult, toolEvidenceResult) {
	t.Helper()
	arguments := fmt.Sprintf(`{"method":%q,"url":%q,"body":%q%s}`, method, rawURL, body, extra)
	result, err := tool.Execute(operation.ContextWithInvocation(t.Context(), inv), arguments)
	if err != nil {
		t.Fatal(err)
	}
	var decoded toolEvidenceResult
	if err := json.Unmarshal([]byte(coretool.ResultText(result)), &decoded); err != nil {
		t.Fatalf("tool result %q: %v", coretool.ResultText(result), err)
	}
	return result, decoded
}

func TestWebAPIHTTPTwoRequestsEvidenceAndIdempotency(t *testing.T) {
	capability, tool, task, inv, count, database := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("X-Fixture", "real")
		_, _ = w.Write(append([]byte(r.URL.Path+":"), body...))
	}))

	firstResult, first := executeHTTP(t, tool, inv, "POST", "http://"+strings.TrimPrefix(task.Origins[0], "http://")+"/first", "alpha", `,"headers":{"X-Test":"one"}`)
	if firstResult.IsError || first.Status != requestCompleted || first.Sent == nil || !*first.Sent || first.HTTPStatus != 200 || first.Preview != "/first:alpha" {
		t.Fatalf("first = %+v / %s", first, coretool.ResultText(firstResult))
	}
	inv.CallID = "call-2"
	_, second := executeHTTP(t, tool, inv, "POST", task.Origins[0]+"/second", "beta", "")
	if second.Status != requestCompleted || second.EvidenceID == first.EvidenceID {
		t.Fatalf("second request not independently completed: %+v", second)
	}
	// 已成功请求的新call允许正常再观察；同call重投零新增发送。
	inv.CallID = "call-3"
	_, third := executeHTTP(t, tool, inv, "GET", task.Origins[0]+"/first", "", "")
	if third.Status != requestCompleted || count.Load() != 3 {
		t.Fatalf("new call not allowed: %+v count=%d", third, count.Load())
	}
	duplicateResult, duplicate := executeHTTP(t, tool, inv, "GET", task.Origins[0]+"/first", "", "")
	if duplicateResult.IsError || duplicateResult.Terminate {
		t.Fatal("completed duplicate became error/terminate")
	}
	if duplicate.EvidenceID != third.EvidenceID || count.Load() != 3 {
		t.Fatalf("duplicate sent or changed evidence: %+v", duplicate)
	}
	model, err := capability.store.evidenceByID(t.Context(), task.ID, first.EvidenceID)
	if err != nil {
		t.Fatal(err)
	}
	var flow traffic.Flow
	if err := proto.Unmarshal(model.FlowPB, &flow); err != nil || string(flow.GetRequest().GetBody()) != "alpha" || string(flow.GetResponse().GetBody()) != "/first:alpha" || !flow.GetComplete() {
		t.Fatalf("flow mismatch: %+v err=%v", &flow, err)
	}
	items, err := capability.store.evidenceList(t.Context(), task.ID, 100, "", "")
	if err != nil || len(items) != 3 {
		t.Fatalf("evidence metadata list len=%d err=%v", len(items), err)
	}
	// 同一新库只读连接关闭后，重开Capability仍可读原生Flow。
	if err := capability.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewCapability(database)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.store.evidenceByID(t.Context(), task.ID, first.EvidenceID)
	if err != nil || persisted == nil || !bytes.Equal(persisted.FlowPB, model.FlowPB) {
		t.Fatalf("flow not durable: %+v err=%v", persisted, err)
	}
	_ = reopened.Close(context.Background())
}

func TestWebAPIHTTPDenyReviewForgedAuthorizationAndCrossTask(t *testing.T) {
	capability, tool, task, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("should-not-send")) }))
	_, denied := executeHTTP(t, tool, inv, "GET", "http://127.0.0.1:1/outside", "", `,"task":"task-http","session":"forged","origins":["http://127.0.0.1:1"],"approved":true`)
	if denied.Status != "denied" || denied.Sent != nil && *denied.Sent || denied.HTTPStatus != 0 {
		t.Fatalf("denied synthesized target response: %+v", denied)
	}
	inv.CallID = "call-review"
	_, review := executeHTTP(t, tool, inv, "POST", task.Origins[0]+"/delete", `{"operation":"remove"}`, "")
	if review.Status != "review_required" || review.Sent != nil && *review.Sent || review.HTTPStatus != 0 || count.Load() != 0 {
		t.Fatalf("review sent: %+v count=%d", review, count.Load())
	}
	inv.SessionID = "forged-session"
	inv.CallID = "call-forged"
	result, err := tool.Execute(operation.ContextWithInvocation(t.Context(), inv), fmt.Sprintf(`{"method":"GET","url":%q,"approved":true}`, task.Origins[0]))
	if err != nil || !result.Terminate || count.Load() != 0 {
		t.Fatalf("forged session executed: %v", result)
	}
	record, err := capability.store.evidenceByID(t.Context(), "other-task", review.EvidenceID)
	if err != nil || record != nil {
		t.Fatalf("cross-task evidence accessible: %+v err=%v", record, err)
	}
}

func TestWebAPIHTTPUnknownNoReplayAndIndependentRequest(t *testing.T) {
	capability, tool, task, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hijacker := w.(http.Hijacker)
		conn, _, err := hijacker.Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	_, unknown := executeHTTP(t, tool, inv, "POST", task.Origins[0]+"/disconnect", "payload", "")
	if unknown.Status != requestUnknown || unknown.Sent != nil || count.Load() != 1 {
		t.Fatalf("disconnect not unknown: %+v", unknown)
	}
	inv.CallID = "call-new-same"
	_, replay := executeHTTP(t, tool, inv, "POST", task.Origins[0]+"/disconnect", "payload", "")
	if replay.EvidenceID != unknown.EvidenceID || count.Load() != 1 {
		t.Fatalf("unknown replayed: %+v count=%d", replay, count.Load())
	}
	// 独立规范请求不受整任务锁死。
	inv.CallID = "call-independent"
	// 不同body/path的独立调用，证明不是origin+path粗锁。
	_, independent := executeHTTP(t, tool, inv, "POST", task.Origins[0]+"/independent", "other", "")
	if independent.EvidenceID == unknown.EvidenceID || count.Load() != 2 || independent.Status != requestUnknown {
		t.Fatalf("independent request blocked by whole-task lock: %+v", independent)
	}
	model, err := capability.store.evidenceByID(t.Context(), task.ID, unknown.EvidenceID)
	if err != nil || model.StatusCode.Valid || model.Status != requestUnknown {
		t.Fatalf("unknown gained fake response: %+v err=%v", model, err)
	}
}

func TestWebAPIHTTPTruncatedAndBinaryEvidence(t *testing.T) {
	binary := append(bytes.Repeat([]byte{0, 1, 2, 255}, 20*1024), []byte("tail")...)
	capability, tool, task, inv, _, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(binary)
	}))
	result, item := executeHTTP(t, tool, inv, "GET", task.Origins[0]+"/binary", "", "")
	if result.IsError || !item.PreviewTruncated || !item.CaptureTruncated {
		t.Fatalf("capture boundaries absent: %+v", item)
	}
	model, err := capability.store.evidenceByID(t.Context(), task.ID, item.EvidenceID)
	if err != nil {
		t.Fatal(err)
	}
	var flow traffic.Flow
	if err := proto.Unmarshal(model.FlowPB, &flow); err != nil || flow.GetComplete() || len(flow.GetResponse().GetBody()) != maxCaptureBytes || !bytes.Equal(flow.GetResponse().GetBody(), binary[:maxCaptureBytes]) {
		t.Fatalf("binary flow not exact bounded capture: len=%d err=%v", len(flow.GetResponse().GetBody()), err)
	}
}

func TestWebAPIHTTPNativeStandardLoop(t *testing.T) {
	_, tool, task, _, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(append([]byte("native:"), body...))
	}))
	tools := hosttest.Tools(t, tool)
	var modelCalls int
	llm := providerFunc(func(_ context.Context, request *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		modelCalls++
		if modelCalls > 3 {
			t.Fatal("unexpected extra model call")
		}
		if modelCalls < 3 {
			id := fmt.Sprintf("native-call-%d", modelCalls)
			encoded, _ := aop.JSONValue(map[string]any{"method": "POST", "url": fmt.Sprintf("%s/native-%d", task.Origins[0], modelCalls), "body": "evidence"})
			return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: &aop.Message{Role: "assistant", Content: []*aop.Content{{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: id, Name: "webapi_http", Arguments: encoded}}}}}, FinishReason: "tool_calls"}}}, nil
		}
		return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: &aop.Message{Role: "assistant", Content: []*aop.Content{aop.Text("done")}}, FinishReason: "stop"}}}, nil
	})
	run := agent.NewAgent(agent.Config{Loop: agent.StandardLoop{}, Provider: llm, Tools: tools, Model: "fixture", SessionID: task.InitialSessionID, MaxTurns: 3})
	result, err := run.Run(t.Context(), agent.TextInput("two real fixture requests"))
	if err != nil || count.Load() != 2 || result.Stop != "stopped" {
		t.Fatalf("native StandardLoop count=%d result=%+v err=%v", count.Load(), result, err)
	}
	models, err := tool.store.evidenceList(t.Context(), task.ID, 10, "", "")
	if err != nil || len(models) != 2 || models[0].SessionID != task.InitialSessionID || models[0].TurnID == "" || models[0].CallID == "" {
		t.Fatalf("native invocation not correlated: %+v err=%v", models, err)
	}
}

func TestWebAPIHTTPNativeMixedBatchUnresolvedStopsNextModel(t *testing.T) {
	capability, tool, task, _, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/unknown" {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	registry := corehooks.New()
	set, err := extension.New(extension.Provided[*corehooks.Registry](registry), coretool.NewToolRegistry(), capability.ToolExtension())
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close(context.Background()) })
	tools := hosttest.ToolsWithHooks(t, registry, tool)
	var modelCalls int
	llm := providerFunc(func(_ context.Context, _ *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		modelCalls++
		if modelCalls > 1 {
			t.Fatal("mixed unresolved batch reached next model")
		}
		message := &aop.Message{Role: "assistant"}
		for _, path := range []string{"/success", "/unknown"} {
			encoded, _ := aop.JSONValue(map[string]any{"method": "GET", "url": task.Origins[0] + path})
			message.Content = append(message.Content, &aop.Content{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: "mixed-" + path, Name: "webapi_http", Arguments: encoded}}})
		}
		return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: message, FinishReason: "tool_calls"}}}, nil
	})
	result, err := agent.NewAgent(agent.Config{Loop: agent.StandardLoop{}, Provider: llm, Tools: tools, Hooks: registry, Model: "fixture", SessionID: task.InitialSessionID, MaxTurns: 3}).Run(t.Context(), agent.TextInput("mixed batch"))
	if err == nil || result.Stop != "error" || modelCalls != 1 || count.Load() != 2 {
		t.Fatalf("mixed batch stop=%s model=%d target=%d err=%v", result.Stop, modelCalls, count.Load(), err)
	}
}

func TestWebAPIHTTPNativeStandardLoopUnknownTerminates(t *testing.T) {
	_, tool, task, _, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	tools := hosttest.Tools(t, tool)
	modelCalls := 0
	llm := providerFunc(func(_ context.Context, _ *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
		modelCalls++
		encoded, _ := aop.JSONValue(map[string]any{"method": "GET", "url": task.Origins[0] + "/unknown"})
		return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: &aop.Message{Role: "assistant", Content: []*aop.Content{{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: "unknown-call", Name: "webapi_http", Arguments: encoded}}}}}, FinishReason: "tool_calls"}}}, nil
	})
	result, err := agent.NewAgent(agent.Config{Loop: agent.StandardLoop{}, Provider: llm, Tools: tools, Model: "fixture", SessionID: task.InitialSessionID, MaxTurns: 3}).Run(t.Context(), agent.TextInput("unknown stops"))
	if err != nil || result.Stop != "terminated" || modelCalls != 1 || count.Load() != 1 {
		t.Fatalf("unknown loop did not terminate: stop=%s model=%d target=%d err=%v", result.Stop, modelCalls, count.Load(), err)
	}
}

type providerFunc func(context.Context, *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error)

func (f providerFunc) Name() string { return "fixture" }
func (f providerFunc) ChatCompletion(ctx context.Context, request *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	return f(ctx, request)
}

func TestWebAPIHTTPCancelSlowResponseAndPersistUnknown(t *testing.T) {
	arrived := make(chan struct{})
	_, tool, task, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(operation.ContextWithInvocation(t.Context(), inv))
	done := make(chan *aop.ToolResult, 1)
	go func() {
		result, _ := tool.Execute(ctx, fmt.Sprintf(`{"method":"GET","url":%q}`, task.Origins[0]+"/slow"))
		done <- result
	}()
	select {
	case <-arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("slow fixture not reached")
	}
	cancel()
	select {
	case result := <-done:
		if !result.IsError || !result.Terminate {
			t.Fatal("cancel did not terminate tool")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("native cancellation did not interrupt HTTP")
	}
	models, err := tool.store.evidenceList(context.Background(), task.ID, 10, "", "")
	if err != nil || len(models) != 1 || models[0].Status != requestUnknown || models[0].Sent.Valid || models[0].CompletedAt.String == "" {
		t.Fatalf("cancel evidence not persisted: %+v err=%v", models, err)
	}
	if count.Load() != 1 {
		t.Fatalf("unexpected sends=%d", count.Load())
	}
}

func TestWebAPIHTTPConcurrentSameFingerprintSingleSend(t *testing.T) {
	arrived := make(chan struct{})
	release := make(chan struct{})
	_, tool, task, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(arrived)
		<-release
		_, _ = w.Write([]byte("one"))
	}))
	const callers = 2
	results := make(chan *aop.ToolResult, callers)
	for i := 0; i < callers; i++ {
		current := inv
		current.CallID = fmt.Sprintf("concurrent-%d", i)
		go func() {
			result, _ := tool.Execute(operation.ContextWithInvocation(t.Context(), current), fmt.Sprintf(`{"method":"GET","url":%q}`, task.Origins[0]+"/concurrent"))
			results <- result
		}()
	}
	select {
	case <-arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("no first request")
	}
	close(release)
	for i := 0; i < callers; i++ {
		<-results
	}
	if count.Load() != 1 {
		t.Fatalf("concurrent same fingerprint sent %d", count.Load())
	}
	models, err := tool.store.evidenceList(t.Context(), task.ID, 10, "", "")
	if err != nil || len(models) != 1 {
		t.Fatalf("candidate not rolled back: %+v err=%v", models, err)
	}
}

func TestWebAPIHTTPEvidenceAPI(t *testing.T) {
	srv, _, closeFn := startTestHub(t)
	defer closeFn()
	status, raw := putTask(t, srv.Client(), srv.URL, "task-api", validBody("known", "objective", `"http://127.0.0.1:12345"`), testAccessKey)
	if status != 201 {
		t.Fatalf("save=%d %s", status, raw)
	}
	request, _ := http.NewRequest("GET", srv.URL+"/api/webapi/tasks/task-api/evidence?limit=1", nil)
	request.Header.Set("Authorization", "Bearer "+testAccessKey)
	response, err := srv.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var list struct {
		Items      []Evidence `json:"items"`
		Limit      int        `json:"limit"`
		HasMore    bool       `json:"has_more"`
		NextCursor string     `json:"next_cursor"`
	}
	if err := json.NewDecoder(response.Body).Decode(&list); err != nil || response.StatusCode != 200 || list.Limit != 1 || len(list.Items) != 0 {
		t.Fatalf("list status=%d value=%+v err=%v", response.StatusCode, list, err)
	}
	request, _ = http.NewRequest("GET", srv.URL+"/api/webapi/tasks/other/evidence/not-owned", nil)
	request.Header.Set("Authorization", "Bearer "+testAccessKey)
	response, err = srv.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatalf("cross task status=%d", response.StatusCode)
	}
}

func TestWebAPIHTTPEvidenceAPIRealFlowAndCursor(t *testing.T) {
	capability, tool, task, inv, _, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write(append([]byte(r.URL.Path+":"), body...))
	}))
	_, first := executeHTTP(t, tool, inv, "POST", task.Origins[0]+"/first", "alpha", "")
	inv.CallID = "call-2"
	executeHTTP(t, tool, inv, "POST", task.Origins[0]+"/second", "beta", "")
	mux := http.NewServeMux()
	mux.Handle("GET /api/webapi/tasks/{id}/evidence", http.HandlerFunc(capability.handleEvidenceList))
	mux.Handle("GET /api/webapi/tasks/{id}/evidence/{evidence_id}", http.HandlerFunc(capability.handleEvidenceGet))
	mux.Handle("GET /api/webapi/tasks", http.HandlerFunc(capability.handleGetTaskBySession))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	response, err := srv.Client().Get(srv.URL + "/api/webapi/tasks/" + task.ID + "/evidence/" + first.EvidenceID)
	if err != nil {
		t.Fatal(err)
	}
	var detail struct {
		Flow json.RawMessage `json:"flow"`
	}
	if err := json.NewDecoder(response.Body).Decode(&detail); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	var flow traffic.Flow
	if err := protojson.Unmarshal(detail.Flow, &flow); err != nil || string(flow.GetResponse().GetBody()) != "/first:alpha" {
		t.Fatalf("protojson flow invalid: %v %s", err, detail.Flow)
	}
	response, err = srv.Client().Get(srv.URL + "/api/webapi/tasks/" + task.ID + "/evidence?limit=1")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items      []Evidence `json:"items"`
		HasMore    bool       `json:"has_more"`
		NextCursor string     `json:"next_cursor"`
	}
	if err := json.NewDecoder(response.Body).Decode(&page); err != nil || !page.HasMore || page.NextCursor == "" || len(page.Items) != 1 {
		t.Fatalf("first page=%+v err=%v", page, err)
	}
	response.Body.Close()
	response, err = srv.Client().Get(srv.URL + "/api/webapi/tasks/" + task.ID + "/evidence?limit=1&cursor=" + page.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	var secondPage struct {
		Items   []Evidence `json:"items"`
		HasMore bool       `json:"has_more"`
	}
	if err := json.NewDecoder(response.Body).Decode(&secondPage); err != nil || secondPage.HasMore || len(secondPage.Items) != 1 || secondPage.Items[0].ID == page.Items[0].ID {
		t.Fatalf("second page=%+v err=%v", secondPage, err)
	}
	response.Body.Close()
	response, err = srv.Client().Get(srv.URL + "/api/webapi/tasks?session_id=" + task.InitialSessionID)
	if err != nil {
		t.Fatal(err)
	}
	var found Task
	if err := json.NewDecoder(response.Body).Decode(&found); err != nil || found.ID != task.ID {
		t.Fatalf("by-session=%+v err=%v", found, err)
	}
	response.Body.Close()
}

func TestWebAPIHTTPBinaryPreviewBounds(t *testing.T) {
	small, truncated := responsePreview([]byte{0, 255}, "application/octet-stream")
	if truncated || string(small) != "base64:AP8=" || len(small) > maxPreviewBytes {
		t.Fatalf("small binary=%q truncated=%v", small, truncated)
	}
	large, truncated := responsePreview(bytes.Repeat([]byte{255}, maxCaptureBytes), "application/octet-stream")
	if !truncated || len(large) > maxPreviewBytes || !strings.HasPrefix(string(large), "base64:") {
		t.Fatalf("large binary len=%d truncated=%v", len(large), truncated)
	}
}

func TestWebAPIHTTPRequestURIAndBinaryPreview(t *testing.T) {
	uris := make(chan string, 1)
	_, tool, task, inv, _, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uris <- r.RequestURI
		_, _ = w.Write([]byte("ok"))
	}))
	executeHTTP(t, tool, inv, "GET", task.Origins[0]+"/a%2Fb/路径", "", "")
	if uri := <-uris; uri != "/a%2Fb/%E8%B7%AF%E5%BE%84" {
		t.Fatalf("wire RequestURI=%q", uri)
	}
}

func TestWebAPIHTTPRequestHostAndHeaders(t *testing.T) {
	if _, err := buildHTTPRequest("GET", "http://127.0.0.1:8080/a", map[string]string{"Host": "other"}, nil); err == nil {
		t.Fatal("Host override accepted")
	}
	req, err := buildHTTPRequest("post", "HTTP://LOCALHOST:80", nil, []byte("x"))
	if err != nil || req.URL.String() != "http://localhost/" || req.Host != "localhost" || req.Header.Get("Accept-Encoding") != "identity" {
		t.Fatalf("canonical request=%+v err=%v", req, err)
	}
	slash, _ := buildHTTPRequest("GET", "http://LOCALHOST/", nil, nil)
	empty, _ := buildHTTPRequest("GET", "http://localhost:80", nil, nil)
	if fingerprintRequest(slash, nil) != fingerprintRequest(empty, nil) {
		t.Fatal("equivalent empty/slash URL fingerprint mismatch")
	}
	_, err = buildHTTPRequest("GET", "http://localhost/", map[string]string{"X-Test": "a\nX-Extra: injected"}, []byte("body"))
	if err == nil {
		t.Fatal("newline header unexpectedly accepted")
	}
}
