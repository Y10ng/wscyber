package webapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

type reportFixture struct {
	capability *Capability
	httpTool   *httpTool
	task       *Task
	mux        *http.ServeMux
	findingA   *findingToolResult
	findingB   *findingToolResult
	evidenceA  string
	evidenceB  string
	unrelated  string
	verifyEvID string
	verifyA    VerificationSummary
	sends      func() int32
}

// newReportFixture 只用临时库和本机fixture：候选A（含有判读验证）与候选B（响应500，无验证）。
func newReportFixture(t *testing.T) *reportFixture {
	t.Helper()
	capability, httpTool, task, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/b") {
			w.WriteHeader(500)
		}
		_, _ = w.Write([]byte("neutral fixture"))
	}))
	_, evidenceA := executeHTTP(t, httpTool, inv, "GET", task.Origins[0]+"/a", "", "")
	inv.CallID = "http-b"
	_, evidenceB := executeHTTP(t, httpTool, inv, "GET", task.Origins[0]+"/b", "", "")
	inv.CallID = "http-unrelated"
	_, unrelated := executeHTTP(t, httpTool, inv, "GET", task.Origins[0]+"/unrelated", "", "")
	findingTool := &findingTool{store: capability.store}
	findingA, message := executeFinding(t, findingTool, operation.Invocation{SessionID: inv.SessionID, TurnID: "finding-turn", CallID: "finding-a"}, findingArguments(evidenceA.EvidenceID, "候选 A"))
	if findingA == nil {
		t.Fatalf("finding A: %s", message)
	}
	findingB, message := executeFinding(t, findingTool, operation.Invocation{SessionID: inv.SessionID, TurnID: "finding-turn", CallID: "finding-b"}, findingArguments(evidenceB.EvidenceID, "候选 B，响应 500"))
	if findingB == nil {
		t.Fatalf("finding B: %s", message)
	}
	verifyA := saveTestVerification(t, capability, task.ID, findingA.FindingID, findingA.RevisionID)
	verifyInv := operation.Invocation{SessionID: verifyA.SessionID, TurnID: "verify-turn", CallID: "verify-http"}
	_, verifyEvidence := executeHTTP(t, httpTool, verifyInv, "GET", task.Origins[0]+"/verify", "", "")
	verifyInv.CallID = "verify-result"
	if result, message := executeVerification(t, &verificationTool{store: capability.store}, verifyInv, verificationArguments("supported", verifyEvidence.EvidenceID, "supporting")); result == nil {
		t.Fatalf("verification result: %s", message)
	}
	mux := http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	return &reportFixture{capability: capability, httpTool: httpTool, task: task, mux: mux, findingA: findingA, findingB: findingB,
		evidenceA: evidenceA.EvidenceID, evidenceB: evidenceB.EvidenceID, unrelated: unrelated.EvidenceID, verifyEvID: verifyEvidence.EvidenceID,
		verifyA: verifyA, sends: count.Load}
}

func (f *reportFixture) do(method, path, body string) (int, map[string]any, string) {
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	var decoded map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
	return recorder.Code, decoded, recorder.Body.String()
}

func (f *reportFixture) items() string {
	return fmt.Sprintf(`{"items":[{"finding_id":%q,"revision_id":%q,"verification_ids":[%q]},{"finding_id":%q,"revision_id":%q}]}`,
		f.findingA.FindingID, f.findingA.RevisionID, f.verifyA.VerificationID, f.findingB.FindingID, f.findingB.RevisionID)
}

func (f *reportFixture) saveReport(t *testing.T, id string) ReportSummary {
	t.Helper()
	status, body, raw := f.do(http.MethodPut, "/api/webapi/tasks/"+f.task.ID+"/reports/"+id, f.items())
	if status != 201 || body["created"] != true {
		t.Fatalf("PUT report = %d %s", status, raw)
	}
	encoded, _ := json.Marshal(body["report"])
	var summary ReportSummary
	if err := json.Unmarshal(encoded, &summary); err != nil {
		t.Fatal(err)
	}
	return summary
}

func executeReportDraft(t *testing.T, store *taskStore, inv operation.Invocation, arguments string) (map[string]any, string) {
	t.Helper()
	result, err := (&reportDraftTool{store: store}).Execute(operation.ContextWithInvocation(t.Context(), inv), arguments)
	if err != nil {
		t.Fatal(err)
	}
	text := coretool.ResultText(result)
	if result.IsError {
		return nil, text
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("draft result %q: %v", text, err)
	}
	return decoded, text
}

func (f *reportFixture) draftArguments(evidenceA, evidenceB []string, title string) string {
	quote := func(ids []string) string {
		raw, _ := json.Marshal(ids)
		return string(raw)
	}
	return fmt.Sprintf(`{"title":%q,"overview":"两条候选的中性报告概述","items":[`+
		`{"finding_id":%q,"description":"候选 A 描述","reproduction":"1. 发送 GET /a\n2. 观察响应","remediation":"候选 A 修复建议","evidence_ids":%s},`+
		`{"finding_id":%q,"description":"候选 B 描述，响应 500","reproduction":"发送 GET /b","evidence_ids":%s}],"limitations":"仅本机fixture"}`,
		title, f.findingA.FindingID, quote(evidenceA), f.findingB.FindingID, quote(evidenceB))
}

const (
	reportID      = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	otherReportID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

func TestWebAPIReportSaveReplayAndSessionProtection(t *testing.T) {
	f := newReportFixture(t)
	report := f.saveReport(t, reportID)
	if report.SessionID == "" || report.SessionID == f.task.InitialSessionID || report.SessionID == f.verifyA.SessionID || len(report.Items) != 2 || report.HasDraft || report.Title != "" {
		t.Fatalf("report summary = %+v", report)
	}
	if len(report.Items[1].VerificationIDs) != 0 || report.Items[1].VerificationIDs == nil {
		t.Fatalf("missing verification_ids not normalized: %+v", report.Items[1])
	}
	base := "/api/webapi/tasks/" + f.task.ID + "/reports/"
	status, replay, _ := f.do(http.MethodPut, base+reportID, f.items())
	if status != 200 || replay["idempotent"] != true || replay["created"] != false || replay["report"].(map[string]any)["session_id"] != report.SessionID ||
		replay["report"].(map[string]any)["has_draft"] != false || replay["report"].(map[string]any)["title"] != "" {
		t.Fatalf("same report replay = %d %+v", status, replay)
	}
	only := func(findingID, revisionID, verifications string) string {
		return fmt.Sprintf(`{"items":[{"finding_id":%q,"revision_id":%q,"verification_ids":[%s]}]}`, findingID, revisionID, verifications)
	}
	if status, _, _ := f.do(http.MethodPut, base+reportID, only(f.findingB.FindingID, f.findingB.RevisionID, "")); status != 409 {
		t.Fatalf("different items accepted = %d", status)
	}
	if status, _, _ := f.do(http.MethodPut, "/api/webapi/tasks/missing-task/reports/"+reportID, f.items()); status != 404 {
		t.Fatalf("report under other task = %d", status)
	}
	// 验证必须属于该候选且来源修订一致；跨候选归属404。
	if status, _, _ := f.do(http.MethodPut, base+"cccccccc-cccc-4ccc-8ccc-cccccccccccc", only(f.findingB.FindingID, f.findingB.RevisionID, fmt.Sprintf("%q", f.verifyA.VerificationID))); status != 404 {
		t.Fatalf("verification of other finding accepted = %d", status)
	}
	if status, _, _ := f.do(http.MethodPut, base+"cccccccc-cccc-4ccc-8ccc-cccccccccccc", only("wf_missing", f.findingB.RevisionID, "")); status != 404 {
		t.Fatalf("missing finding accepted = %d", status)
	}
	for name, body := range map[string]string{
		"empty":     `{"items":[]}`,
		"duplicate": fmt.Sprintf(`{"items":[{"finding_id":%q,"revision_id":%q},{"finding_id":%q,"revision_id":%q}]}`, f.findingB.FindingID, f.findingB.RevisionID, f.findingB.FindingID, f.findingB.RevisionID),
		"unknown":   `{"items":[{"finding_id":"a","revision_id":"b","verdict":"x"}]}`,
	} {
		if status, _, _ := f.do(http.MethodPut, base+"dddddddd-dddd-4ddd-8ddd-dddddddddddd", body); status != 400 {
			t.Fatalf("%s items accepted = %d", name, status)
		}
	}

	// 候选更新后：同ID回放仍200，新报告固定旧修订409；同修订验证不匹配新修订404。
	updatedArguments := strings.Replace(findingArguments(f.evidenceA, "候选 A 更新"), `{"content"`, `{"finding_id":"`+f.findingA.FindingID+`","expected_revision":1,"content"`, 1)
	updated, message := executeFinding(t, &findingTool{store: f.capability.store}, operation.Invocation{SessionID: f.task.InitialSessionID, TurnID: "finding-turn", CallID: "finding-a-update"}, updatedArguments)
	if updated == nil || updated.Revision != 2 {
		t.Fatalf("candidate update: %s", message)
	}
	if status, _, _ := f.do(http.MethodPut, base+reportID, f.items()); status != 200 {
		t.Fatalf("old report replay blocked by newer revision = %d", status)
	}
	if status, _, _ := f.do(http.MethodPut, base+otherReportID, f.items()); status != 409 {
		t.Fatalf("stale new report accepted = %d", status)
	}
	if status, _, _ := f.do(http.MethodPut, base+otherReportID, only(f.findingA.FindingID, updated.RevisionID, fmt.Sprintf("%q", f.verifyA.VerificationID))); status != 404 {
		t.Fatalf("verification of older revision accepted = %d", status)
	}

	// 报告session不能写HTTP、候选或验证结果。
	sendsBefore := f.sends()
	reportInv := operation.Invocation{SessionID: report.SessionID, TurnID: "report-turn", CallID: "report-http"}
	if task, err := f.capability.store.taskForSession(t.Context(), report.SessionID); err != nil || task != nil {
		t.Fatalf("report session resolved to task: %+v %v", task, err)
	}
	httpResult, err := f.httpTool.Execute(operation.ContextWithInvocation(t.Context(), reportInv), fmt.Sprintf(`{"method":"GET","url":%q}`, f.task.Origins[0]+"/from-report"))
	if err != nil || !httpResult.IsError {
		t.Fatalf("report session sent HTTP: %+v %v", httpResult, err)
	}
	reportInv.CallID = "report-finding"
	if created, _ := executeFinding(t, &findingTool{store: f.capability.store}, reportInv, findingArguments(f.evidenceA, "报告session越权候选")); created != nil {
		t.Fatalf("report session created finding: %+v", created)
	}
	reportInv.CallID = "report-verification"
	if result, _ := executeVerification(t, &verificationTool{store: f.capability.store}, reportInv, verificationArguments("supported", f.verifyEvID, "supporting")); result != nil {
		t.Fatalf("report session wrote verification result: %+v", result)
	}
	if f.sends() != sendsBefore {
		t.Fatalf("report session caused target sends: %d -> %d", sendsBefore, f.sends())
	}

	// 列表、详情、session反查只读固定来源；无草稿导出409。
	listPath := "/api/webapi/tasks/" + f.task.ID + "/reports"
	if _, _, _, err := f.capability.store.saveReport(t.Context(), "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", f.task.ID, []ReportItem{{FindingID: f.findingB.FindingID, RevisionID: f.findingB.RevisionID, VerificationIDs: []string{}}}); err != nil {
		t.Fatal(err)
	}
	status, page, raw := f.do(http.MethodGet, listPath+"?limit=1", "")
	if status != 200 || page["has_more"] != true || len(page["items"].([]any)) != 1 || page["items"].([]any)[0].(map[string]any)["title"] != "" {
		t.Fatalf("report list = %d %s", status, raw)
	}
	status, second, raw := f.do(http.MethodGet, listPath+"?limit=1&cursor="+page["next_cursor"].(string), "")
	if status != 200 || second["has_more"] != false || len(second["items"].([]any)) != 1 ||
		second["items"].([]any)[0].(map[string]any)["report_id"] == page["items"].([]any)[0].(map[string]any)["report_id"] {
		t.Fatalf("report second page = %d %s", status, raw)
	}
	if status, _, _ := f.do(http.MethodGet, "/api/webapi/tasks/missing-task/reports", ""); status != 404 {
		t.Fatalf("missing task list = %d", status)
	}
	status, detail, raw := f.do(http.MethodGet, base+reportID, "")
	if status != 200 || detail["draft"] != nil || detail["has_draft"] != false {
		t.Fatalf("report detail = %d %s", status, raw)
	}
	items := detail["items"].([]any)
	first := items[0].(map[string]any)
	if first["revision"].(map[string]any)["revision"] != float64(1) || first["revision"].(map[string]any)["content"].(map[string]any)["title"] != "候选 A" {
		t.Fatalf("pinned revision snapshot changed: %s", raw)
	}
	verifications := first["verifications"].([]any)
	if len(verifications) != 1 || verifications[0].(map[string]any)["verdict"] != "supported" || verifications[0].(map[string]any)["result"] == nil {
		t.Fatalf("attached verification result missing: %s", raw)
	}
	status, bySession, _ := f.do(http.MethodGet, "/api/webapi/reports?session_id="+report.SessionID, "")
	if status != 200 || bySession["report_id"] != reportID {
		t.Fatalf("report by session = %d %+v", status, bySession)
	}
	if status, _, _ := f.do(http.MethodGet, "/api/webapi/reports?session_id="+f.task.InitialSessionID, ""); status != 404 {
		t.Fatalf("ordinary session resolved as report = %d", status)
	}
	if status, _, _ := f.do(http.MethodGet, base+reportID+"/export?format=markdown", ""); status != 409 {
		t.Fatalf("export without draft = %d", status)
	}
}

func TestWebAPIReportDraftAndDeterministicExport(t *testing.T) {
	f := newReportFixture(t)
	report := f.saveReport(t, reportID)
	store := f.capability.store
	valid := f.draftArguments([]string{f.evidenceA, f.verifyEvID}, []string{f.evidenceB}, "报告标题")

	if result, message := executeReportDraft(t, store, operation.Invocation{SessionID: f.task.InitialSessionID, TurnID: "t", CallID: "c"}, valid); result != nil || !strings.Contains(message, "不是已保存的报告会话") {
		t.Fatalf("ordinary session wrote draft: %+v %s", result, message)
	}
	if result, message := executeReportDraft(t, store, operation.Invocation{SessionID: f.verifyA.SessionID, TurnID: "t", CallID: "c"}, valid); result != nil || !strings.Contains(message, "不是已保存的报告会话") {
		t.Fatalf("verification session wrote draft: %+v %s", result, message)
	}
	inv := operation.Invocation{SessionID: report.SessionID, TurnID: "report-turn", CallID: "draft-call"}
	missingItem := fmt.Sprintf(`{"title":"t","overview":"o","items":[{"finding_id":%q,"description":"d","reproduction":"r"}]}`, f.findingA.FindingID)
	if result, message := executeReportDraft(t, store, inv, missingItem); result != nil || !strings.Contains(message, "一一对应") {
		t.Fatalf("draft missing item accepted: %+v %s", result, message)
	}
	duplicated := strings.Replace(valid, f.findingB.FindingID, f.findingA.FindingID, 1)
	if result, message := executeReportDraft(t, store, inv, duplicated); result != nil || !strings.Contains(message, "一一对应") {
		t.Fatalf("draft duplicated item accepted: %+v %s", result, message)
	}
	for name, evidence := range map[string][]string{"unrelated": {f.unrelated}, "other-item": {f.evidenceA}} {
		if result, message := executeReportDraft(t, store, inv, f.draftArguments([]string{f.evidenceA}, evidence, "t")); result != nil || !strings.Contains(message, "evidence_ids") {
			t.Fatalf("%s evidence accepted: %+v %s", name, result, message)
		}
	}
	if result, message := executeReportDraft(t, store, inv, strings.Replace(valid, `"title"`, `"report_id":"x","title"`, 1)); result != nil || !strings.Contains(message, "未知字段") {
		t.Fatalf("self-reported ownership accepted: %+v %s", result, message)
	}
	if status, _, _ := f.do(http.MethodGet, "/api/webapi/tasks/"+f.task.ID+"/reports/"+reportID+"/export?format=json", ""); status != 409 {
		t.Fatalf("rejected drafts left a draft: %d", status)
	}

	saved, message := executeReportDraft(t, store, inv, valid)
	if saved == nil || saved["report_id"] != reportID || saved["item_count"] != float64(2) {
		t.Fatalf("valid draft rejected: %+v %s", saved, message)
	}
	// 同Invocation同规范载荷回读（空白不同也一致），改载荷或换Invocation拒绝。
	if replay, message := executeReportDraft(t, store, inv, strings.Replace(valid, `,"items"`, ` , "items"`, 1)); replay == nil {
		t.Fatalf("same invocation replay rejected: %s", message)
	}
	if changed, message := executeReportDraft(t, store, inv, f.draftArguments([]string{f.evidenceA}, nil, "改过的标题")); changed != nil || !strings.Contains(message, "不可变") {
		t.Fatalf("same invocation changed draft: %+v %s", changed, message)
	}
	other := inv
	other.CallID = "draft-call-other"
	if changed, message := executeReportDraft(t, store, other, valid); changed != nil || !strings.Contains(message, "不可变") {
		t.Fatalf("other invocation overwrote draft: %+v %s", changed, message)
	}

	exportPath := "/api/webapi/tasks/" + f.task.ID + "/reports/" + reportID + "/export"
	status, _, markdown := f.do(http.MethodGet, exportPath+"?format=markdown", "")
	if status != 200 {
		t.Fatalf("markdown export = %d %s", status, markdown)
	}
	_, _, again := f.do(http.MethodGet, exportPath+"?format=markdown", "")
	if again != markdown {
		t.Fatal("markdown export is not deterministic")
	}
	order := []string{"# 报告标题", "## 概述", "两条候选的中性报告概述", "## 候选", "### 1. " + f.findingA.Number + " 候选 A", "修订：r1",
		"#### 描述", "候选 A 描述", "#### 复现步骤", "证据引用：", "`" + f.evidenceA + "`", "`" + f.verifyEvID + "`", "#### 修复建议", "候选 A 修复建议",
		"#### 验证判读", "支持（模型判读）", "### 2. " + f.findingB.Number + " 候选 B，响应 500", "HTTP 500", "#### 修复建议\n\n未提供", "#### 验证判读\n\n未提供", "## 局限", "仅本机fixture"}
	position := 0
	for _, fragment := range order {
		index := strings.Index(markdown[position:], fragment)
		if index < 0 {
			t.Fatalf("markdown missing %q after offset %d:\n%s", fragment, position, markdown)
		}
		position += index + len(fragment)
	}
	if strings.Contains(markdown, "候选 A 更新") {
		t.Fatal("markdown used non-pinned content")
	}

	status, _, rawJSON := f.do(http.MethodGet, exportPath+"?format=json", "")
	var exported reportExport
	if status != 200 || json.Unmarshal([]byte(rawJSON), &exported) != nil {
		t.Fatalf("json export = %d %s", status, rawJSON)
	}
	if exported.Format != "wscyber.report.v1" || exported.ReportID != reportID || len(exported.Items) != 2 || exported.Items[0].Title != "候选 A" ||
		len(exported.Items[0].Evidence) != 2 || len(exported.Items[0].Verifications) != 1 || exported.Items[1].Remediation != "" || exported.DraftSource.CallID != "draft-call" {
		t.Fatalf("json export content = %s", rawJSON)
	}
	if _, _, rawAgain := f.do(http.MethodGet, exportPath+"?format=json", ""); rawAgain != rawJSON {
		t.Fatal("json export is not deterministic")
	}
	if status, _, _ := f.do(http.MethodGet, exportPath+"?format=pdf", ""); status != 400 {
		t.Fatalf("unsupported format = %d", status)
	}
	status, page, _ := f.do(http.MethodGet, "/api/webapi/tasks/"+f.task.ID+"/reports", "")
	if status != 200 || page["items"].([]any)[0].(map[string]any)["has_draft"] != true || page["items"].([]any)[0].(map[string]any)["title"] != "报告标题" {
		t.Fatalf("list has_draft/title = %d %+v", status, page)
	}
	status, replay, _ := f.do(http.MethodPut, "/api/webapi/tasks/"+f.task.ID+"/reports/"+reportID, f.items())
	if status != 200 || replay["report"].(map[string]any)["has_draft"] != true || replay["report"].(map[string]any)["title"] != "报告标题" {
		t.Fatalf("replay with draft = %d %+v", status, replay)
	}
	status, detail, _ := f.do(http.MethodGet, "/api/webapi/tasks/"+f.task.ID+"/reports/"+reportID, "")
	if status != 200 || detail["draft"] == nil || detail["has_draft"] != true {
		t.Fatalf("detail draft = %d %+v", status, detail)
	}
}

func TestWebAPIReportLegacyPreparation(t *testing.T) {
	// 含验证六表的旧库：只补报告两表，原行保全。
	database := t.TempDir() + "/hub.sqlite"
	stage, err := webservice.NewSQLiteStore(database, webapiSchemaStage(6, 8))
	if err != nil {
		t.Fatal(err)
	}
	if err := stage.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := openTaskStore(database)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := store.Save(t.Context(), "verification-stage-task", "旧任务", "保留验证阶段行", []string{"http://127.0.0.1:8000"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := PrepareWebAPISchema(database); err != nil {
		t.Fatalf("prepare verification stage: %v", err)
	}
	complete, err := webservice.NewSQLiteStore(database, SchemaModule())
	if err != nil {
		t.Fatalf("full native validation after prepare: %v", err)
	}
	if err := complete.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openTaskStore(database)
	if err != nil {
		t.Fatal(err)
	}
	preserved, err := reopened.Get(t.Context(), outcome.Task.ID)
	if err != nil || preserved == nil || preserved.KnownInfo != "旧任务" {
		t.Fatalf("verification stage task lost: %+v %v", preserved, err)
	}
	if count, err := reopened.orm.NewSelect().Model((*reportModel)(nil)).Count(t.Context()); err != nil || count != 0 {
		t.Fatalf("report table not empty/created: %d %v", count, err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	if err := PrepareWebAPISchema(database); err != nil {
		t.Fatalf("complete schema not accepted: %v", err)
	}

	// 六表＋仅报告表的半成结构拒绝。
	halfPath := t.TempDir() + "/half.sqlite"
	half, err := webservice.NewSQLiteStore(halfPath, webapiSchemaStage(6, 8))
	if err != nil {
		t.Fatal(err)
	}
	if err := half.Close(); err != nil {
		t.Fatal(err)
	}
	halfStore, err := openTaskStore(halfPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := halfStore.orm.NewCreateTable().Model((*reportModel)(nil)).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := halfStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := PrepareWebAPISchema(halfPath); err == nil {
		t.Fatal("partial report schema accepted")
	}
}
