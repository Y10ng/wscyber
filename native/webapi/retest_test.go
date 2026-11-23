package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/uptrace/bun"
	"google.golang.org/protobuf/proto"
)

func retestMux(capability *Capability) *http.ServeMux {
	mux := http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	return mux
}

func doRetest(mux *http.ServeMux, method, path, body string) (int, map[string]any) {
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	var decoded map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &decoded)
	return recorder.Code, decoded
}

func saveTestRetest(t *testing.T, capability *Capability, taskID, findingID, revisionID, id string) RetestSummary {
	t.Helper()
	status, body := doRetest(retestMux(capability), http.MethodPut, "/api/webapi/tasks/"+taskID+"/findings/"+findingID+"/retests/"+id, fmt.Sprintf(`{"source_revision_id":%q}`, revisionID))
	if status != 201 || body["created"] != true {
		t.Fatalf("PUT retest = %d %+v", status, body)
	}
	raw, _ := json.Marshal(body["retest"])
	var summary RetestSummary
	if err := json.Unmarshal(raw, &summary); err != nil {
		t.Fatal(err)
	}
	return summary
}

func executeRetest(t *testing.T, store *taskStore, inv operation.Invocation, arguments string) (map[string]any, string) {
	t.Helper()
	result, err := (&retestTool{store: store}).Execute(operation.ContextWithInvocation(t.Context(), inv), arguments)
	if err != nil {
		t.Fatal(err)
	}
	text := coretool.ResultText(result)
	if result.IsError {
		return nil, text
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("retest result %q: %v", text, err)
	}
	return decoded, text
}

func retestArguments(conclusion, evidenceID, relation string) string {
	return fmt.Sprintf(`{"conclusion":%q,"method":"重新发送独立HTTP请求并比较真实响应","summary":"本机fixture复测结论","limitations":"仅本次范围","evidence":[{"evidence_id":%q,"relation":%q}]}`, conclusion, evidenceID, relation)
}

func TestWebAPIRetestSourceSessionAndProtection(t *testing.T) {
	capability, httpTool, task, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/disconnect" {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				_ = conn.Close()
			}
			return
		}
		_, _ = w.Write([]byte("neutral fixture"))
	}))
	store := capability.store
	_, evidence := executeHTTP(t, httpTool, inv, "GET", task.Origins[0]+"/source", "", "")
	findingTool := &findingTool{store: store}
	findingInv := operation.Invocation{SessionID: inv.SessionID, TurnID: "finding-turn", CallID: "finding-call"}
	created, message := executeFinding(t, findingTool, findingInv, findingArguments(evidence.EvidenceID, "固定候选"))
	if created == nil {
		t.Fatalf("candidate not created: %s", message)
	}
	// 已有验证及其判读不被复测覆盖。
	verification := saveTestVerification(t, capability, task.ID, created.FindingID, created.RevisionID)
	verifyInv := operation.Invocation{SessionID: verification.SessionID, TurnID: "verify-turn", CallID: "verify-http"}
	_, verifyEvidence := executeHTTP(t, httpTool, verifyInv, "GET", task.Origins[0]+"/verify", "", "")
	verifyInv.CallID = "verify-result"
	if result, message := executeVerification(t, &verificationTool{store: store}, verifyInv, verificationArguments("supported", verifyEvidence.EvidenceID, "supporting")); result == nil {
		t.Fatalf("verification result: %s", message)
	}

	mux := retestMux(capability)
	base := "/api/webapi/tasks/" + task.ID + "/findings/" + created.FindingID + "/retests/"
	firstID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	retest := saveTestRetest(t, capability, task.ID, created.FindingID, created.RevisionID, firstID)
	if retest.SessionID == "" || retest.SessionID == task.InitialSessionID || retest.SessionID == verification.SessionID || retest.RetestID != firstID {
		t.Fatalf("retest session = %+v", retest)
	}
	body := func(revisionID string) string { return fmt.Sprintf(`{"source_revision_id":%q}`, revisionID) }
	if status, replay := doRetest(mux, http.MethodPut, base+firstID, body(created.RevisionID)); status != 200 || replay["idempotent"] != true || replay["retest"].(map[string]any)["session_id"] != retest.SessionID {
		t.Fatalf("same retest replay = %d %+v", status, replay)
	}
	if status, _ := doRetest(mux, http.MethodPut, base+firstID, body("wfr_different")); status != 409 {
		t.Fatalf("different source accepted = %d", status)
	}
	if status, _ := doRetest(mux, http.MethodPut, "/api/webapi/tasks/"+task.ID+"/findings/wf_missing/retests/"+firstID, body(created.RevisionID)); status != 404 {
		t.Fatalf("wrong finding ownership = %d", status)
	}
	if status, _ := doRetest(mux, http.MethodPut, "/api/webapi/tasks/"+task.ID+"/findings/wf_missing/retests/bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", body(created.RevisionID)); status != 404 {
		t.Fatalf("missing finding = %d", status)
	}
	if status, _ := doRetest(mux, http.MethodPut, base+"cccccccc-cccc-4ccc-8ccc-cccccccccccc", `{"source_revision_id":"x","extra":1}`); status != 400 {
		t.Fatalf("unknown body field accepted = %d", status)
	}

	// 同一created_at的第二个复测，用于分页；之后候选更新使旧revision过期。
	second, _, _, err := store.saveRetest(t.Context(), "dddddddd-dddd-4ddd-8ddd-dddddddddddd", task.ID, created.FindingID, created.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model((*retestModel)(nil)).Set("created_at = ?", retest.CreatedAt).Where("id IN (?, ?)", firstID, second.RetestID).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	findingInv.CallID = "finding-call-update"
	updatedArguments := strings.Replace(findingArguments(evidence.EvidenceID, "更新候选"), `{"content"`, `{"finding_id":"`+created.FindingID+`","expected_revision":1,"content"`, 1)
	updated, _ := executeFinding(t, findingTool, findingInv, updatedArguments)
	if updated == nil || updated.Revision != 2 {
		t.Fatalf("candidate update = %+v", updated)
	}
	if status, _ := doRetest(mux, http.MethodPut, base+firstID, body(created.RevisionID)); status != 200 {
		t.Fatalf("old retest replay blocked by newer candidate = %d", status)
	}
	if status, _ := doRetest(mux, http.MethodPut, base+"eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", body(created.RevisionID)); status != 409 {
		t.Fatalf("stale new retest accepted = %d", status)
	}

	// 复测session的HTTP归属父任务并受父任务origins限制。
	retestInv := operation.Invocation{SessionID: retest.SessionID, TurnID: "retest-turn", CallID: "retest-http"}
	_, newEvidence := executeHTTP(t, httpTool, retestInv, "GET", task.Origins[0]+"/retest", "", "")
	if newEvidence.SessionID != retest.SessionID || newEvidence.TaskID != task.ID || newEvidence.Status != requestCompleted {
		t.Fatalf("retest HTTP not owned correctly: %+v", newEvidence)
	}
	retestInv.CallID = "retest-denied"
	_, denied := executeHTTP(t, httpTool, retestInv, "GET", "http://127.0.0.1:1/outside", "", "")
	if denied.Status != "denied" || denied.TaskID != task.ID {
		t.Fatalf("retest lost parent origins: %+v", denied)
	}
	sendsBefore := count.Load()
	inv.CallID = "parent-disconnect"
	_, unknown := executeHTTP(t, httpTool, inv, "POST", task.Origins[0]+"/disconnect", "payload", "")
	if unknown.Status != requestUnknown || count.Load() != sendsBefore+1 {
		t.Fatalf("parent request not unknown: %+v", unknown)
	}
	retestInv.CallID = "retest-same-unknown"
	_, replay := executeHTTP(t, httpTool, retestInv, "POST", task.Origins[0]+"/disconnect", "payload", "")
	if replay.EvidenceID != unknown.EvidenceID || count.Load() != sendsBefore+1 {
		t.Fatalf("retest session bypassed parent unknown protection: %+v count=%d", replay, count.Load())
	}

	// 写权限隔离：复测session不能写候选/验证/报告草稿；验证与普通session不能写复测结论。
	retestInv.CallID = "retest-finding"
	if result, message := executeFinding(t, findingTool, retestInv, findingArguments(newEvidence.EvidenceID, "复测session越权候选")); result != nil || !strings.Contains(message, "复测session不能创建或更新") {
		t.Fatalf("retest session modified candidate: %+v %s", result, message)
	}
	retestInv.CallID = "retest-verification"
	if result, message := executeVerification(t, &verificationTool{store: store}, retestInv, verificationArguments("supported", newEvidence.EvidenceID, "supporting")); result != nil || !strings.Contains(message, "不是已保存的独立验证") {
		t.Fatalf("retest session wrote verification result: %+v %s", result, message)
	}
	retestInv.CallID = "retest-draft"
	if result, message := executeReportDraft(t, store, retestInv, `{"title":"t","overview":"o","items":[]}`); result != nil || !strings.Contains(message, "不是已保存的报告会话") {
		t.Fatalf("retest session wrote report draft: %+v %s", result, message)
	}
	if result, message := executeRetest(t, store, operation.Invocation{SessionID: verification.SessionID, TurnID: "v", CallID: "v"}, retestArguments("reproducible", verifyEvidence.EvidenceID, "supporting")); result != nil || !strings.Contains(message, "不是已保存的复测") {
		t.Fatalf("verification session wrote retest result: %+v %s", result, message)
	}
	if result, message := executeRetest(t, store, operation.Invocation{SessionID: task.InitialSessionID, TurnID: "o", CallID: "o"}, retestArguments("reproducible", newEvidence.EvidenceID, "supporting")); result != nil || !strings.Contains(message, "不是已保存的复测") {
		t.Fatalf("ordinary session wrote retest result: %+v %s", result, message)
	}
	if result, message := executeRetest(t, store, operation.Invocation{SessionID: retest.SessionID}, retestArguments("reproducible", newEvidence.EvidenceID, "supporting")); result != nil || !strings.Contains(message, "缺少可信") {
		t.Fatalf("missing invocation accepted: %+v %s", result, message)
	}

	// 详情、分页与session反查。
	status, detail := doRetest(mux, http.MethodGet, base+firstID, "")
	if status != 200 || detail["result"] != nil || detail["source"].(map[string]any)["revision_id"] != created.RevisionID || detail["source_revision_id"] != created.RevisionID {
		t.Fatalf("retest detail = %d %+v", status, detail)
	}
	if status, _ := doRetest(mux, http.MethodGet, base+"ffffffff-ffff-4fff-8fff-ffffffffffff", ""); status != 404 {
		t.Fatalf("missing retest detail = %d", status)
	}
	list := "/api/webapi/tasks/" + task.ID + "/findings/" + created.FindingID + "/retests"
	status, page := doRetest(mux, http.MethodGet, list+"?limit=1", "")
	if status != 200 || page["has_more"] != true || len(page["items"].([]any)) != 1 {
		t.Fatalf("retest list = %d %+v", status, page)
	}
	first := page["items"].([]any)[0].(map[string]any)
	if _, has := first["conclusion"]; has {
		t.Fatalf("empty conclusion should be omitted: %+v", first)
	}
	status, secondPage := doRetest(mux, http.MethodGet, list+"?limit=2&cursor="+page["next_cursor"].(string), "")
	if status != 200 || secondPage["has_more"] != false || len(secondPage["items"].([]any)) != 1 {
		t.Fatalf("same-time retest second page = %d %+v", status, secondPage)
	}
	if a, b := first["retest_id"], secondPage["items"].([]any)[0].(map[string]any)["retest_id"]; a == b || (a != firstID && a != second.RetestID) || (b != firstID && b != second.RetestID) {
		t.Fatalf("same-time pagination lost/duplicated IDs: %v %v", a, b)
	}
	if status, _ := doRetest(mux, http.MethodGet, list+"?limit=0", ""); status != 400 {
		t.Fatalf("invalid limit = %d", status)
	}
	if status, _ := doRetest(mux, http.MethodGet, "/api/webapi/tasks/"+task.ID+"/findings/wf_missing/retests", ""); status != 404 {
		t.Fatalf("list missing finding = %d", status)
	}
	if status, bySession := doRetest(mux, http.MethodGet, "/api/webapi/retests?session_id="+retest.SessionID, ""); status != 200 || bySession["retest_id"] != firstID {
		t.Fatalf("retest by session = %d %+v", status, bySession)
	}
	if status, _ := doRetest(mux, http.MethodGet, "/api/webapi/retests?session_id="+verification.SessionID, ""); status != 404 {
		t.Fatalf("verification session resolved as retest = %d", status)
	}
	if found, err := store.taskForSession(t.Context(), retest.SessionID); err != nil || found == nil || found.ID != task.ID {
		t.Fatalf("retest session task = %+v %v", found, err)
	}

	// 原候选、验证判读保持原样。
	current, err := store.findingCurrent(t.Context(), task.ID, created.FindingID)
	if err != nil || current.RevisionID != updated.RevisionID || current.Status != "candidate" {
		t.Fatalf("candidate changed: %+v %v", current, err)
	}
	verifyDetail, err := store.verificationResult(t.Context(), verification.VerificationID)
	if err != nil || verifyDetail == nil || verifyDetail.Payload.Verdict != "supported" {
		t.Fatalf("verification result changed: %+v %v", verifyDetail, err)
	}
}

func TestWebAPIRetestResultEvidenceAndImmutability(t *testing.T) {
	capability, httpTool, task, inv, _, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/disconnect" {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				_ = conn.Close()
			}
			return
		}
		w.WriteHeader(500)
		_, _ = w.Write([]byte("actual diagnostic response"))
	}))
	store := capability.store
	_, originalEvidence := executeHTTP(t, httpTool, inv, "GET", task.Origins[0]+"/original", "", "")
	created, _ := executeFinding(t, &findingTool{store: store}, operation.Invocation{SessionID: inv.SessionID, TurnID: "finding-turn", CallID: "finding-call"}, findingArguments(originalEvidence.EvidenceID, "复测候选"))
	if created == nil {
		t.Fatal("candidate not created")
	}
	description := (&retestTool{}).Description()
	if !strings.Contains(description, "Login failure") || !strings.Contains(description, "inconclusive") || !strings.Contains(description, "NOT mean fixed") {
		t.Fatalf("tool description lacks not-fixed guidance: %s", description)
	}

	one := saveTestRetest(t, capability, task.ID, created.FindingID, created.RevisionID, "11111111-1111-4111-8111-111111111111")
	oneHTTP := operation.Invocation{SessionID: one.SessionID, TurnID: "one-http", CallID: "one-http"}
	_, evidence := executeHTTP(t, httpTool, oneHTTP, "GET", task.Origins[0]+"/retest-500", "", "")
	if evidence.HTTPStatus != 500 {
		t.Fatalf("fixture status = %+v", evidence)
	}
	oneInv := operation.Invocation{SessionID: one.SessionID, TurnID: "one-result", CallID: "one-call"}
	if result, message := executeRetest(t, store, oneInv, retestArguments("bogus", evidence.EvidenceID, "supporting")); result != nil || !strings.Contains(message, "conclusion仅支持") {
		t.Fatalf("bogus conclusion accepted: %+v %s", result, message)
	}
	if result, message := executeRetest(t, store, oneInv, `{"conclusion":"inconclusive","method":"m","summary":"s","verdict":"supported"}`); result != nil || !strings.Contains(message, "strict JSON") {
		t.Fatalf("unknown field accepted: %+v %s", result, message)
	}
	if result, message := executeRetest(t, store, oneInv, retestArguments("fixed_in_scope", evidence.EvidenceID, "supporting")); result != nil || !strings.Contains(message, "缺少对应relation") {
		t.Fatalf("fixed_in_scope without counterevidence accepted: %+v %s", result, message)
	}
	if result, message := executeRetest(t, store, oneInv, retestArguments("reproducible", originalEvidence.EvidenceID, "supporting")); result != nil || !strings.Contains(message, "本复测session") {
		t.Fatalf("original candidate evidence accepted: %+v %s", result, message)
	}
	arguments := retestArguments("reproducible", evidence.EvidenceID, "supporting")
	result, message := executeRetest(t, store, oneInv, arguments)
	if result == nil || result["conclusion"] != "reproducible" || result["retest_id"] != one.RetestID {
		t.Fatalf("HTTP500 valid reproducible evidence rejected: %+v %s", result, message)
	}
	if replayed, message := executeRetest(t, store, oneInv, arguments); replayed == nil {
		t.Fatalf("same invocation replay rejected: %s", message)
	}
	if changed, message := executeRetest(t, store, oneInv, retestArguments("inconclusive", evidence.EvidenceID, "limitation")); changed != nil || !strings.Contains(message, "不可变") {
		t.Fatalf("same invocation changed result: %+v %s", changed, message)
	}
	otherInv := oneInv
	otherInv.CallID = "one-call-other"
	if changed, message := executeRetest(t, store, otherInv, arguments); changed != nil || !strings.Contains(message, "不可变") {
		t.Fatalf("other invocation overwrote result: %+v %s", changed, message)
	}

	// fixed_in_scope：完整反证可保存；他复测的证据被拒。
	two := saveTestRetest(t, capability, task.ID, created.FindingID, created.RevisionID, "22222222-2222-4222-8222-222222222222")
	twoInv := operation.Invocation{SessionID: two.SessionID, TurnID: "two-result", CallID: "two-call"}
	if rejected, message := executeRetest(t, store, twoInv, retestArguments("fixed_in_scope", evidence.EvidenceID, "counterevidence")); rejected != nil || !strings.Contains(message, "本复测session") {
		t.Fatalf("cross retest evidence accepted: %+v %s", rejected, message)
	}
	_, twoEvidence := executeHTTP(t, httpTool, operation.Invocation{SessionID: two.SessionID, TurnID: "two-http", CallID: "two-http"}, "GET", task.Origins[0]+"/fixed", "", "")
	if accepted, message := executeRetest(t, store, twoInv, retestArguments("fixed_in_scope", twoEvidence.EvidenceID, "counterevidence")); accepted == nil || accepted["conclusion"] != "fixed_in_scope" {
		t.Fatalf("fixed_in_scope with complete counterevidence rejected: %+v %s", accepted, message)
	}

	// 无响应(unknown)与截断不能支撑已修复/可复现，只能作为限制给inconclusive。
	three := saveTestRetest(t, capability, task.ID, created.FindingID, created.RevisionID, "33333333-3333-4333-8333-333333333333")
	threeHTTP := operation.Invocation{SessionID: three.SessionID, TurnID: "three-http", CallID: "three-unknown"}
	_, unreachable := executeHTTP(t, httpTool, threeHTTP, "GET", task.Origins[0]+"/disconnect", "", "")
	if unreachable.Status != requestUnknown {
		t.Fatalf("fixture not unknown: %+v", unreachable)
	}
	threeHTTP.TurnID, threeHTTP.CallID = "three-http-2", "three-truncated"
	_, truncated := executeHTTP(t, httpTool, threeHTTP, "GET", task.Origins[0]+"/truncated", "", "")
	model, err := store.evidenceByID(t.Context(), task.ID, truncated.EvidenceID)
	if err != nil {
		t.Fatal(err)
	}
	model.CaptureTruncated = true
	var flow traffic.Flow
	if err := proto.Unmarshal(model.FlowPB, &flow); err != nil {
		t.Fatal(err)
	}
	flow.Complete = false
	model.FlowPB, _ = proto.Marshal(&flow)
	if err := store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model(model).Column("capture_truncated", "flow_pb").WherePK().Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	threeInv := operation.Invocation{SessionID: three.SessionID, TurnID: "three-result", CallID: "three-call"}
	if rejected, message := executeRetest(t, store, threeInv, retestArguments("fixed_in_scope", unreachable.EvidenceID, "counterevidence")); rejected != nil || !strings.Contains(message, "完整捕获") {
		t.Fatalf("unreachable accepted as fixed: %+v %s", rejected, message)
	}
	if rejected, message := executeRetest(t, store, threeInv, retestArguments("fixed_in_scope", unreachable.EvidenceID, "limitation")); rejected != nil || !strings.Contains(message, "不等于已修复") {
		t.Fatalf("fixed_in_scope with only limitation accepted: %+v %s", rejected, message)
	}
	if rejected, message := executeRetest(t, store, threeInv, retestArguments("reproducible", truncated.EvidenceID, "supporting")); rejected != nil || !strings.Contains(message, "完整捕获") {
		t.Fatalf("truncated evidence accepted: %+v %s", rejected, message)
	}
	limited := fmt.Sprintf(`{"conclusion":"inconclusive","method":"重新访问","summary":"目标不可达且捕获截断","limitations":"不可达不等于已修复","evidence":[{"evidence_id":%q,"relation":"limitation"},{"evidence_id":%q,"relation":"limitation"}]}`, unreachable.EvidenceID, truncated.EvidenceID)
	if accepted, message := executeRetest(t, store, threeInv, limited); accepted == nil || accepted["conclusion"] != "inconclusive" {
		t.Fatalf("inconclusive with limitations rejected: %+v %s", accepted, message)
	}

	// 结论可读：详情含结论与新证据快照；列表显示conclusion；原候选不变。
	mux := retestMux(capability)
	status, detail := doRetest(mux, http.MethodGet, "/api/webapi/tasks/"+task.ID+"/findings/"+created.FindingID+"/retests/"+one.RetestID, "")
	resultView, _ := detail["result"].(map[string]any)
	if status != 200 || resultView == nil || resultView["payload"].(map[string]any)["conclusion"] != "reproducible" || len(resultView["evidence"].([]any)) != 1 || resultView["call_id"] != "one-call" {
		t.Fatalf("retest detail result = %d %+v", status, detail)
	}
	status, page := doRetest(mux, http.MethodGet, "/api/webapi/tasks/"+task.ID+"/findings/"+created.FindingID+"/retests", "")
	conclusions := map[string]any{}
	for _, item := range page["items"].([]any) {
		conclusions[item.(map[string]any)["retest_id"].(string)] = item.(map[string]any)["conclusion"]
	}
	if status != 200 || conclusions[one.RetestID] != "reproducible" || conclusions[two.RetestID] != "fixed_in_scope" || conclusions[three.RetestID] != "inconclusive" {
		t.Fatalf("retest list conclusions = %d %+v", status, conclusions)
	}
	current, err := store.findingCurrent(t.Context(), task.ID, created.FindingID)
	if err != nil || current.Status != "candidate" || current.RevisionID != created.RevisionID {
		t.Fatalf("retest changed candidate: %+v %v", current, err)
	}
}
