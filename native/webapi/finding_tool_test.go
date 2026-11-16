package webapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func executeFinding(t *testing.T, tool *findingTool, inv operation.Invocation, arguments string) (*findingToolResult, string) {
	t.Helper()
	result, err := tool.Execute(operation.ContextWithInvocation(t.Context(), inv), arguments)
	if err != nil {
		t.Fatal(err)
	}
	text := coretool.ResultText(result)
	if result.IsError {
		return nil, text
	}
	var decoded findingToolResult
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("finding result %q: %v", text, err)
	}
	return &decoded, text
}

type findingToolResult struct {
	FindingID  string                    `json:"finding_id"`
	Number     string                    `json:"number"`
	Revision   int64                     `json:"revision"`
	RevisionID string                    `json:"revision_id"`
	Status     string                    `json:"status"`
	Evidence   []FindingEvidenceSnapshot `json:"evidence"`
}

func findingArguments(evidenceID, title string) string {
	return fmt.Sprintf(`{"content":{"title":%q,"description":"仅为本机fixture影响主张","steps":[{"text":"观察真实HTTP证据","evidence_indices":[0]}],"remediation":"修复建议","limitations":"尚未独立验证","evidence":[{"evidence_id":%q,"relation":"supporting"}]}}`, title, evidenceID)
}

func TestWebAPIFindingLifecycleSnapshotsAndReadonly(t *testing.T) {
	capability, httpTool, task, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(202)
		_, _ = w.Write([]byte("private fixture response body"))
	}))
	_, evidence := executeHTTP(t, httpTool, inv, "GET", task.Origins[0]+"/real", "", "")
	tool := &findingTool{store: capability.store}
	findingInv := operation.Invocation{SessionID: inv.SessionID, TurnID: "finding-turn", CallID: "finding-call"}
	arguments := findingArguments(evidence.EvidenceID, "候选标题")
	created, _ := executeFinding(t, tool, findingInv, arguments)
	if created == nil || created.Number != "WSC-000001" || created.Revision != 1 || created.Status != "candidate" || len(created.Evidence) != 1 {
		t.Fatalf("created = %+v", created)
	}
	snapshot := created.Evidence[0]
	if snapshot.Status != requestCompleted || snapshot.Sent == nil || !*snapshot.Sent || snapshot.HTTPStatus == nil || *snapshot.HTTPStatus != 202 || snapshot.FlowSHA256 == "" || snapshot.CallID != evidence.CallID {
		t.Fatalf("snapshot not factual: %+v", snapshot)
	}
	duplicate, _ := executeFinding(t, tool, findingInv, arguments)
	if duplicate == nil || duplicate.RevisionID != created.RevisionID {
		t.Fatalf("same call not idempotent: %+v", duplicate)
	}
	if conflict, message := executeFinding(t, tool, findingInv, findingArguments(evidence.EvidenceID, "不同标题")); conflict != nil || !strings.Contains(message, "不同候选载荷") {
		t.Fatalf("different same-call payload accepted: %+v %s", conflict, message)
	}

	findingInv.CallID = "finding-call-2"
	updatedArguments := strings.Replace(arguments, `{"content"`, `{"finding_id":"`+created.FindingID+`","expected_revision":1,"content"`, 1)
	updated, _ := executeFinding(t, tool, findingInv, strings.Replace(updatedArguments, "候选标题", "更新标题", 1))
	if updated == nil || updated.FindingID != created.FindingID || updated.Number != created.Number || updated.Revision != 2 {
		t.Fatalf("updated = %+v", updated)
	}
	staleInv := findingInv
	staleInv.CallID = "finding-call-3"
	if stale, message := executeFinding(t, tool, staleInv, updatedArguments); stale != nil || !strings.Contains(message, "陈旧") {
		t.Fatalf("stale update accepted: %+v %s", stale, message)
	}
	old, err := capability.store.findingRevision(t.Context(), task.ID, created.FindingID, created.RevisionID)
	if err != nil || old == nil || old.Content.Title != "候选标题" || old.Revision != 1 {
		t.Fatalf("old revision changed: %+v %v", old, err)
	}
	encoded, _ := json.Marshal(old)
	if strings.Contains(string(encoded), "private fixture response body") || strings.Contains(string(encoded), "flow_pb") {
		t.Fatalf("finding copied original bytes: %s", encoded)
	}

	secondInv := findingInv
	secondInv.CallID = "finding-call-other"
	second, _ := executeFinding(t, tool, secondInv, findingArguments(evidence.EvidenceID, "另一个候选"))
	if second == nil || second.Number != "WSC-000002" {
		t.Fatalf("second = %+v", second)
	}
	mux := http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	request := func(path string) (int, map[string]any) {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest("GET", path, nil))
		var body map[string]any
		_ = json.Unmarshal(recorder.Body.Bytes(), &body)
		return recorder.Code, body
	}
	base := "/api/webapi/tasks/" + task.ID + "/findings"
	status, page := request(base + "?limit=1")
	if status != 200 || page["has_more"] != true || page["next_cursor"] == nil || len(page["items"].([]any)) != 1 {
		t.Fatalf("list = %d %+v", status, page)
	}
	status, secondPage := request(base + "?limit=1&cursor=" + page["next_cursor"].(string))
	if status != 200 || secondPage["has_more"] != false || secondPage["items"].([]any)[0].(map[string]any)["finding_id"] != created.FindingID {
		t.Fatalf("list cursor = %d %+v", status, secondPage)
	}
	status, _ = request("/api/webapi/tasks/missing/findings")
	if status != 404 {
		t.Fatalf("unknown task list=%d", status)
	}
	status, history := request(base + "/" + created.FindingID + "/history?limit=1")
	if status != 200 || history["has_more"] != true || history["next_cursor"] == nil {
		t.Fatalf("history = %d %+v", status, history)
	}
	cursor := history["next_cursor"].(string)
	status, older := request(base + "/" + created.FindingID + "/history?limit=1&cursor=" + cursor)
	if status != 200 || older["has_more"] != false || older["items"].([]any)[0].(map[string]any)["revision"] != float64(1) {
		t.Fatalf("history cursor = %d %+v", status, older)
	}
	status, _ = request("/api/webapi/tasks/other/findings/" + created.FindingID)
	if status != 404 || count.Load() != 1 {
		t.Fatalf("cross-task read=%d or target sent count=%d", status, count.Load())
	}
	status, exact := request(base + "/" + created.FindingID + "/revisions/" + created.RevisionID)
	if status != 200 || exact["content"].(map[string]any)["title"] != "候选标题" || exact["revision_created_at"] == "" || exact["created_at"] != nil || exact["updated_at"] != nil {
		t.Fatalf("exact revision = %d %+v", status, exact)
	}
}

func TestWebAPIFindingRejectsFakeCrossTaskAndAuthority(t *testing.T) {
	capability, httpTool, task, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	_, evidence := executeHTTP(t, httpTool, inv, "GET", task.Origins[0]+"/real", "", "")
	tool := &findingTool{store: capability.store}
	findingInv := operation.Invocation{SessionID: inv.SessionID, TurnID: "finding-turn", CallID: "fake"}
	if result, message := executeFinding(t, tool, findingInv, findingArguments("we_fake", "假引用")); result != nil || !strings.Contains(message, "真实HTTP") {
		t.Fatalf("fake evidence accepted: %+v %s", result, message)
	}
	other, err := capability.store.Save(t.Context(), "other-task", "other", "other", task.Origins)
	if err != nil {
		t.Fatal(err)
	}
	crossInv := findingInv
	crossInv.SessionID = other.Task.InitialSessionID
	if result, message := executeFinding(t, tool, crossInv, findingArguments(evidence.EvidenceID, "跨任务")); result != nil || !strings.Contains(message, "真实HTTP") {
		t.Fatalf("cross evidence accepted: %+v %s", result, message)
	}
	findingInv.CallID = "authority"
	bad := strings.TrimSuffix(findingArguments(evidence.EvidenceID, "未知权威字段"), "}}") + `,"status":"confirmed"}}`
	if result, message := executeFinding(t, tool, findingInv, bad); result != nil || !strings.Contains(message, "未知字段") {
		t.Fatalf("unknown authority accepted: %+v %s", result, message)
	}
	findingInv.CallID = "bad-index"
	badIndex := strings.Replace(findingArguments(evidence.EvidenceID, "错误索引"), "[0]", "[1]", 1)
	if result, message := executeFinding(t, tool, findingInv, badIndex); result != nil || !strings.Contains(message, "索引") {
		t.Fatalf("bad index accepted: %+v %s", result, message)
	}
	findingInv.CallID = "bad-id"
	badID := `{"finding_id":"missing","expected_revision":0,` + strings.TrimPrefix(findingArguments(evidence.EvidenceID, "不能创建"), "{")
	if result, message := executeFinding(t, tool, findingInv, badID); result != nil || !strings.Contains(message, "更新必须") {
		t.Fatalf("bad ID fell back to create: %+v %s", result, message)
	}
	if count.Load() != 1 {
		t.Fatalf("finding operations sent HTTP: %d", count.Load())
	}
}

func TestWebAPIFindingConcurrentNumbersUnique(t *testing.T) {
	capability, httpTool, task, inv, _, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	_, evidence := executeHTTP(t, httpTool, inv, "GET", task.Origins[0]+"/real", "", "")
	tool := &findingTool{store: capability.store}
	const total = 8
	var wait sync.WaitGroup
	results := make(chan *findingToolResult, total)
	errors := make(chan string, total)
	for index := 0; index < total; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			findingInv := operation.Invocation{SessionID: inv.SessionID, TurnID: "finding-turn", CallID: fmt.Sprintf("finding-%d", index)}
			result, message := executeFinding(t, tool, findingInv, findingArguments(evidence.EvidenceID, fmt.Sprintf("并发%d", index)))
			if result == nil {
				errors <- message
			} else {
				results <- result
			}
		}(index)
	}
	wait.Wait()
	close(results)
	close(errors)
	for message := range errors {
		t.Error(message)
	}
	numbers := map[string]bool{}
	for result := range results {
		if numbers[result.Number] {
			t.Errorf("duplicate number %s", result.Number)
		}
		numbers[result.Number] = true
	}
	if len(numbers) != total {
		t.Fatalf("got %d unique numbers, want %d", len(numbers), total)
	}
}

func TestWebAPIFindingUnsentSnapshotRemainsUnsent(t *testing.T) {
	capability, httpTool, _, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unauthorized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("未授权目的地被访问")
	}))
	defer unauthorized.Close()
	_, evidence := executeHTTP(t, httpTool, inv, "GET", unauthorized.URL+"/outside", "", "")
	tool := &findingTool{store: capability.store}
	findingInv := operation.Invocation{SessionID: inv.SessionID, TurnID: "finding-turn", CallID: "unsent"}
	created, _ := executeFinding(t, tool, findingInv, findingArguments(evidence.EvidenceID, "拒绝也是真实证据"))
	if created == nil || created.Evidence[0].Status != "denied" || created.Evidence[0].Sent == nil || *created.Evidence[0].Sent || created.Evidence[0].HTTPStatus != nil {
		t.Fatalf("unsent changed into response: %+v", created)
	}
	if count.Load() != 0 {
		t.Fatalf("fixture target unexpectedly called: %d", count.Load())
	}
}
