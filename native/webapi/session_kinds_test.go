package webapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/chainreactors/cyber/core/operation"
)

// TestWebAPISessionKinds 用临时库与中性合成数据核对批量归属：
// 一个任务 + 2 候选 + 验证 + 复测 + 报告（有草稿）+ 不认识的会话。
func TestWebAPISessionKinds(t *testing.T) {
	f := newReportFixture(t)
	retest := saveTestRetest(t, f.capability, f.task.ID, f.findingB.FindingID, f.findingB.RevisionID, "55555555-5555-4555-8555-555555555555")
	report := f.saveReport(t, "66666666-6666-4666-8666-666666666666")
	reportInv := operation.Invocation{SessionID: report.SessionID, TurnID: "report-turn", CallID: "draft-call"}
	if result, message := executeReportDraft(t, f.capability.store, reportInv, f.draftArguments([]string{f.evidenceA}, []string{f.evidenceB}, "批量归属报告标题")); result == nil {
		t.Fatalf("draft rejected: %s", message)
	}

	unknown := "session-unknown"
	body := fmt.Sprintf(`{"session_ids":[%q,%q,%q,%q,%q,%q,%q]}`,
		f.task.InitialSessionID, f.verifyA.SessionID, retest.SessionID, report.SessionID, unknown, f.verifyA.SessionID, unknown)
	status, decoded, raw := f.do(http.MethodPost, "/api/webapi/session-kinds", body)
	if status != 200 {
		t.Fatalf("session-kinds = %d %s", status, raw)
	}
	encoded, _ := json.Marshal(decoded["items"])
	var items map[string]SessionKindItem
	if err := json.Unmarshal(encoded, &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 4 {
		t.Fatalf("items = %+v", items)
	}
	if _, ok := items[unknown]; ok {
		t.Fatalf("unknown session present: %+v", items[unknown])
	}

	taskItem := items[f.task.InitialSessionID]
	if taskItem.Kind != "task" || taskItem.TaskID != f.task.ID || taskItem.TaskSessionID != f.task.InitialSessionID ||
		taskItem.TaskObjective != f.task.Objective || taskItem.Title != f.task.Objective || taskItem.Number != "" || taskItem.CreatedAt != f.task.CreatedAt {
		t.Fatalf("task item = %+v", taskItem)
	}

	verificationItem := items[f.verifyA.SessionID]
	if verificationItem.Kind != "verification" || verificationItem.TaskID != f.task.ID || verificationItem.TaskSessionID != f.task.InitialSessionID ||
		verificationItem.TaskObjective != f.task.Objective || verificationItem.Number != "WSC-000001" || verificationItem.Title != "候选 A" ||
		verificationItem.CreatedAt != f.verifyA.CreatedAt {
		t.Fatalf("verification item = %+v", verificationItem)
	}

	retestItem := items[retest.SessionID]
	if retestItem.Kind != "retest" || retestItem.TaskID != f.task.ID || retestItem.TaskSessionID != f.task.InitialSessionID ||
		retestItem.Number != "WSC-000002" || retestItem.Title != "候选 B，响应 500" || retestItem.CreatedAt != retest.CreatedAt {
		t.Fatalf("retest item = %+v", retestItem)
	}

	reportItem := items[report.SessionID]
	if reportItem.Kind != "report" || reportItem.TaskID != f.task.ID || reportItem.TaskSessionID != f.task.InitialSessionID ||
		reportItem.TaskObjective != f.task.Objective || reportItem.Number != "" || reportItem.Title != "批量归属报告标题" || reportItem.CreatedAt != report.CreatedAt {
		t.Fatalf("report item = %+v", reportItem)
	}

	for name, bad := range map[string]string{
		"empty":     `{"session_ids":[]}`,
		"missing":   `{}`,
		"unknown":   `{"session_ids":["a"],"extra":1}`,
		"malformed": `{"session_ids":`,
		"trailing":  `{"session_ids":["a"]}{}`,
		"empty_id":  `{"session_ids":[""]}`,
	} {
		status, decoded, _ := f.do(http.MethodPost, "/api/webapi/session-kinds", bad)
		if status != 400 || decoded["error"] == "" || decoded["error"] == nil {
			t.Fatalf("%s accepted = %d %+v", name, status, decoded)
		}
	}

	ids := make([]string, 501)
	for i := range ids {
		ids[i] = fmt.Sprintf("s-%d", i)
	}
	quoted, _ := json.Marshal(ids)
	status, decoded, _ = f.do(http.MethodPost, "/api/webapi/session-kinds", fmt.Sprintf(`{"session_ids":%s}`, quoted))
	if status != 400 || decoded["error"] == nil {
		t.Fatalf("501 ids accepted = %d %+v", status, decoded)
	}

	// 500 个不认识的会话合法：200 空 items，不是 400。
	ids = ids[:500]
	quoted, _ = json.Marshal(ids)
	status, decoded, raw = f.do(http.MethodPost, "/api/webapi/session-kinds", fmt.Sprintf(`{"session_ids":%s}`, quoted))
	if status != 200 {
		t.Fatalf("500 unknown ids = %d %s", status, raw)
	}
	if len(decoded["items"].(map[string]any)) != 0 {
		t.Fatalf("500 unknown ids items = %+v", decoded)
	}
}

// TestWebAPISessionKindsNoDraft 无草稿报告 title 为空；重复会话去重不影响结果。
func TestWebAPISessionKindsNoDraft(t *testing.T) {
	f := newReportFixture(t)
	report := f.saveReport(t, "77777777-7777-4777-8777-777777777777")
	status, decoded, raw := f.do(http.MethodPost, "/api/webapi/session-kinds", fmt.Sprintf(`{"session_ids":[%q,%q]}`, report.SessionID, report.SessionID))
	if status != 200 {
		t.Fatalf("session-kinds = %d %s", status, raw)
	}
	itemsMap, _ := decoded["items"].(map[string]any)
	item, _ := itemsMap[report.SessionID].(map[string]any)
	if item == nil {
		t.Fatalf("report session missing in items: %s", raw)
	}
	if item["kind"] != "report" || item["title"] != "" || item["number"] != "" {
		t.Fatalf("no-draft report item = %+v", item)
	}
}

// TestWebAPISessionKindsMatchesBySession 与既有 by-session 单查接口的编号/标题一致。
func TestWebAPISessionKindsMatchesBySession(t *testing.T) {
	f := newReportFixture(t)
	status, single, raw := f.do(http.MethodGet, "/api/webapi/verifications?session_id="+f.verifyA.SessionID, "")
	if status != 200 {
		t.Fatalf("by-session = %d %s", status, raw)
	}
	source := single["source"].(map[string]any)
	content := source["content"].(map[string]any)
	status, batch, raw := f.do(http.MethodPost, "/api/webapi/session-kinds", fmt.Sprintf(`{"session_ids":[%q]}`, f.verifyA.SessionID))
	if status != 200 {
		t.Fatalf("session-kinds = %d %s", status, raw)
	}
	item := batch["items"].(map[string]any)[f.verifyA.SessionID].(map[string]any)
	if item["number"] != source["number"] || item["title"] != content["title"] {
		t.Fatalf("batch %+v != by-session source %+v", item, source)
	}
}
