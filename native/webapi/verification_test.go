package webapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
	"google.golang.org/protobuf/proto"
)

func saveTestVerification(t *testing.T, capability *Capability, taskID, findingID, revisionID string) VerificationSummary {
	t.Helper()
	mux := http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	path := "/api/webapi/tasks/" + taskID + "/findings/" + findingID + "/verifications/11111111-1111-4111-8111-111111111111"
	request := httptest.NewRequest(http.MethodPut, path, strings.NewReader(fmt.Sprintf(`{"source_revision_id":%q}`, revisionID)))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != 201 {
		t.Fatalf("PUT verification = %d %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Verification VerificationSummary `json:"verification"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response.Verification
}

func executeVerification(t *testing.T, tool *verificationTool, inv operation.Invocation, arguments string) (map[string]any, string) {
	t.Helper()
	result, err := tool.Execute(operation.ContextWithInvocation(t.Context(), inv), arguments)
	if err != nil {
		t.Fatal(err)
	}
	text := coretool.ResultText(result)
	if result.IsError {
		return nil, text
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(text), &decoded); err != nil {
		t.Fatalf("verification result %q: %v", text, err)
	}
	return decoded, text
}

func verificationArguments(verdict, evidenceID, relation string) string {
	return fmt.Sprintf(`{"verdict":%q,"method":"重新发送独立HTTP请求并比较真实响应","summary":"本机fixture模型判读","limitations":"不代表自动确认","evidence":[{"evidence_id":%q,"relation":%q}]}`, verdict, evidenceID, relation)
}

type unknownSchemaTable struct {
	ID string `bun:"id,pk"`
}

func TestWebAPIVerificationSourceSessionAndFindingProtection(t *testing.T) {
	capability, httpTool, task, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("source fixture"))
	}))
	_, evidence := executeHTTP(t, httpTool, inv, "GET", task.Origins[0]+"/source", "", "")
	findingTool := &findingTool{store: capability.store}
	findingInv := operation.Invocation{SessionID: inv.SessionID, TurnID: "finding-turn", CallID: "finding-call"}
	created, _ := executeFinding(t, findingTool, findingInv, findingArguments(evidence.EvidenceID, "固定候选"))
	if created == nil {
		t.Fatal("candidate not created")
	}
	verification := saveTestVerification(t, capability, task.ID, created.FindingID, created.RevisionID)
	if verification.SessionID == "" || verification.SessionID == task.InitialSessionID {
		t.Fatalf("verification session = %+v", verification)
	}

	mux := http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	put := func(id, revisionID string) (int, map[string]any) {
		request := httptest.NewRequest(http.MethodPut, "/api/webapi/tasks/"+task.ID+"/findings/"+created.FindingID+"/verifications/"+id, strings.NewReader(fmt.Sprintf(`{"source_revision_id":%q}`, revisionID)))
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, request)
		var body map[string]any
		_ = json.Unmarshal(recorder.Body.Bytes(), &body)
		return recorder.Code, body
	}
	status, replay := put(verification.VerificationID, created.RevisionID)
	if status != 200 || replay["idempotent"] != true || replay["verification"].(map[string]any)["session_id"] != verification.SessionID {
		t.Fatalf("same verification replay = %d %+v", status, replay)
	}
	status, _ = put(verification.VerificationID, "wfr_different")
	if status != 409 {
		t.Fatalf("different source accepted = %d", status)
	}

	// 候选后续变化不修改验证的固定来源；同ID回放仍成功，新ID旧revision过期。
	secondSameTime, _, _, err := capability.store.saveVerification(t.Context(), "33333333-3333-4333-8333-333333333333", task.ID, created.FindingID, created.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := capability.store.orm.NewUpdate().Model((*verificationModel)(nil)).Set("created_at = ?", verification.CreatedAt).Where("id IN (?, ?)", verification.VerificationID, secondSameTime.VerificationID).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	findingInv.CallID = "finding-call-update"
	updatedArguments := strings.Replace(findingArguments(evidence.EvidenceID, "更新候选"), `{"content"`, `{"finding_id":"`+created.FindingID+`","expected_revision":1,"content"`, 1)
	updated, _ := executeFinding(t, findingTool, findingInv, updatedArguments)
	if updated == nil || updated.Revision != 2 {
		t.Fatalf("candidate update = %+v", updated)
	}
	status, _ = put(verification.VerificationID, created.RevisionID)
	if status != 200 {
		t.Fatalf("old verification replay blocked by newer candidate = %d", status)
	}
	status, _ = put("22222222-2222-4222-8222-222222222222", created.RevisionID)
	if status != 409 {
		t.Fatalf("stale new verification accepted = %d", status)
	}
	detail, err := capability.store.verificationDetail(t.Context(), &verificationModel{ID: verification.VerificationID, TaskID: task.ID, FindingID: created.FindingID, SourceRevisionID: created.RevisionID, SessionID: verification.SessionID})
	if err != nil || detail.Source.Content.Title != "固定候选" || detail.Source.Revision != 1 {
		t.Fatalf("fixed source changed: %+v %v", detail, err)
	}

	verificationInv := operation.Invocation{SessionID: verification.SessionID, TurnID: "verify-turn", CallID: "verify-http"}
	_, newEvidence := executeHTTP(t, httpTool, verificationInv, "GET", task.Origins[0]+"/verify", "", "")
	if newEvidence.SessionID != verification.SessionID || newEvidence.TaskID != task.ID || newEvidence.Status != requestCompleted {
		t.Fatalf("verification HTTP not owned correctly: %+v", newEvidence)
	}
	verificationInv.CallID = "verify-denied"
	_, denied := executeHTTP(t, httpTool, verificationInv, "GET", "http://127.0.0.1:1/outside", "", "")
	if denied.Status != "denied" || denied.TaskID != task.ID {
		t.Fatalf("verification lost parent origins: %+v", denied)
	}
	if result, message := executeVerification(t, &verificationTool{store: capability.store}, operation.Invocation{SessionID: inv.SessionID, TurnID: "not-verification", CallID: "not-verification"}, verificationArguments("supported", newEvidence.EvidenceID, "supporting")); result != nil || !strings.Contains(message, "不是已保存的独立验证") {
		t.Fatalf("ordinary session forged verification result: %+v %s", result, message)
	}
	verificationInv.CallID = "verify-finding"
	if result, message := executeFinding(t, findingTool, verificationInv, findingArguments(newEvidence.EvidenceID, "验证session越权候选")); result != nil || !strings.Contains(message, "不能创建或更新") {
		t.Fatalf("verification session modified candidate: %+v %s", result, message)
	}
	if count.Load() != 2 {
		t.Fatalf("unexpected target sends = %d", count.Load())
	}

	// 列表与session反查均只读固定记录，不拉Flow原件。
	get := func(path string) (int, map[string]any) {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		var body map[string]any
		_ = json.Unmarshal(recorder.Body.Bytes(), &body)
		return recorder.Code, body
	}
	base := "/api/webapi/tasks/" + task.ID + "/findings/" + created.FindingID + "/verifications"
	status, page := get(base + "?limit=1")
	if status != 200 || page["has_more"] != true || len(page["items"].([]any)) != 1 {
		t.Fatalf("verification list = %d %+v", status, page)
	}
	firstID := page["items"].([]any)[0].(map[string]any)["verification_id"]
	status, secondPage := get(base + "?limit=2&cursor=" + page["next_cursor"].(string))
	if status != 200 || secondPage["has_more"] != false || len(secondPage["items"].([]any)) != 1 {
		t.Fatalf("same-time verification second page = %d %+v", status, secondPage)
	}
	secondID := secondPage["items"].([]any)[0].(map[string]any)["verification_id"]
	if firstID == secondID || (firstID != verification.VerificationID && firstID != secondSameTime.VerificationID) || (secondID != verification.VerificationID && secondID != secondSameTime.VerificationID) {
		t.Fatalf("same-time pagination lost/duplicated IDs: %v %v", firstID, secondID)
	}
	status, bySession := get("/api/webapi/verifications?session_id=" + verification.SessionID)
	if status != 200 || bySession["session_id"] != verification.SessionID || bySession["result"] != nil {
		t.Fatalf("verification by session = %d %+v", status, bySession)
	}
	// 两个验证session即使turn/call及具体请求相同，也不能串用旧唯一键回执。
	if _, _, _, err := capability.store.saveVerification(t.Context(), "44444444-4444-4444-8444-444444444444", task.ID, created.FindingID, updated.RevisionID); err != nil {
		t.Fatal(err)
	}
	otherVerification, err := capability.store.verificationByID(t.Context(), task.ID, created.FindingID, "44444444-4444-4444-8444-444444444444")
	if err != nil {
		t.Fatal(err)
	}
	sameCall := operation.Invocation{SessionID: verification.SessionID, TurnID: "shared-turn", CallID: "shared-call"}
	_, shared := executeHTTP(t, httpTool, sameCall, "GET", task.Origins[0]+"/shared", "", "")
	sameCall.SessionID = otherVerification.SessionID
	result, duplicate := executeHTTP(t, httpTool, sameCall, "GET", task.Origins[0]+"/shared", "", "")
	if !result.IsError || duplicate.EvidenceID != shared.EvidenceID || count.Load() != 3 {
		t.Fatalf("cross-session same call reused receipt/sent: %+v %+v count=%d", result, duplicate, count.Load())
	}
}

func TestWebAPIVerificationResultEvidenceAndImmutability(t *testing.T) {
	capability, httpTool, task, inv, _, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("actual diagnostic response"))
	}))
	_, originalEvidence := executeHTTP(t, httpTool, inv, "GET", task.Origins[0]+"/original", "", "")
	findingInv := operation.Invocation{SessionID: inv.SessionID, TurnID: "finding-turn", CallID: "finding-call"}
	created, _ := executeFinding(t, &findingTool{store: capability.store}, findingInv, findingArguments(originalEvidence.EvidenceID, "HTTP500只作真实响应证据"))
	verification := saveTestVerification(t, capability, task.ID, created.FindingID, created.RevisionID)
	verificationInv := operation.Invocation{SessionID: verification.SessionID, TurnID: "verify-turn", CallID: "verify-http"}
	_, evidence := executeHTTP(t, httpTool, verificationInv, "GET", task.Origins[0]+"/verify-500", "", "")
	if evidence.HTTPStatus != 500 {
		t.Fatalf("fixture status = %+v", evidence)
	}
	tool := &verificationTool{store: capability.store}
	resultInv := operation.Invocation{SessionID: verification.SessionID, TurnID: "result-turn", CallID: "result-call"}
	arguments := verificationArguments("supported", evidence.EvidenceID, "supporting")
	result, _ := executeVerification(t, tool, resultInv, arguments)
	if result == nil || result["verdict"] != "supported" {
		t.Fatalf("HTTP500 valid evidence rejected: %+v", result)
	}
	replayed, _ := executeVerification(t, tool, resultInv, arguments)
	if replayed == nil {
		t.Fatal("same invocation replay rejected")
	}
	if changed, message := executeVerification(t, tool, resultInv, verificationArguments("inconclusive", evidence.EvidenceID, "limitation")); changed != nil || !strings.Contains(message, "不可变") {
		t.Fatalf("same invocation changed result: %+v %s", changed, message)
	}
	otherInv := resultInv
	otherInv.CallID = "result-call-other"
	if changed, message := executeVerification(t, tool, otherInv, arguments); changed != nil || !strings.Contains(message, "不可变") {
		t.Fatalf("other invocation overwrote result: %+v %s", changed, message)
	}
	current, err := capability.store.findingCurrent(t.Context(), task.ID, created.FindingID)
	if err != nil || current.Status != "candidate" || current.RevisionID != created.RevisionID {
		t.Fatalf("verification changed candidate: %+v %v", current, err)
	}

	second, _, _, err := capability.store.saveVerification(t.Context(), "22222222-2222-4222-8222-222222222222", task.ID, created.FindingID, created.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if rejected, message := executeVerification(t, tool, resultInv, verificationArguments("counterevidence", evidence.EvidenceID, "counterevidence")); rejected != nil || !strings.Contains(message, "不可变") {
		t.Fatalf("counterevidence result was not immutable: %+v %s", rejected, message)
	}
	secondHTTPInv := operation.Invocation{SessionID: second.SessionID, TurnID: "second-http", CallID: "second-http-call"}
	_, secondEvidence := executeHTTP(t, httpTool, secondHTTPInv, "GET", task.Origins[0]+"/second", "", "")
	secondInv := operation.Invocation{SessionID: second.SessionID, TurnID: "second-result", CallID: "second-call"}
	if accepted, message := executeVerification(t, tool, secondInv, verificationArguments("counterevidence", secondEvidence.EvidenceID, "counterevidence")); accepted == nil || accepted["verdict"] != "counterevidence" {
		t.Fatalf("counterevidence with complete evidence rejected: %+v %s", accepted, message)
	}
	if accepted, message := executeVerification(t, tool, secondInv, `{"verdict":"inconclusive","method":"只读核对","summary":"证据不足","evidence":[]}`); accepted != nil || !strings.Contains(message, "不可变") {
		t.Fatalf("immutable result overwritten by inconclusive: %+v %s", accepted, message)
	}
	third, _, _, err := capability.store.saveVerification(t.Context(), "55555555-5555-4555-8555-555555555555", task.ID, created.FindingID, created.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	thirdInv := operation.Invocation{SessionID: third.SessionID, TurnID: "third-result", CallID: "third-call"}
	if accepted, message := executeVerification(t, tool, thirdInv, `{"verdict":"inconclusive","method":"只读核对","summary":"证据不足","evidence":[]}`); accepted == nil || accepted["verdict"] != "inconclusive" {
		t.Fatalf("inconclusive without supporting evidence rejected: %+v %s", accepted, message)
	}
	if rejected, message := executeVerification(t, tool, resultInv, verificationArguments("supported", "we_nonexistent", "supporting")); rejected != nil || !strings.Contains(message, "新HTTP证据") {
		t.Fatalf("fake evidence accepted: %+v %s", rejected, message)
	}

	fourth, _, _, err := capability.store.saveVerification(t.Context(), "66666666-6666-4666-8666-666666666666", task.ID, created.FindingID, created.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	fourthInv := operation.Invocation{SessionID: fourth.SessionID, TurnID: "fourth-result", CallID: "fourth-call"}
	if rejected, message := executeVerification(t, tool, fourthInv, verificationArguments("supported", originalEvidence.EvidenceID, "supporting")); rejected != nil || !strings.Contains(message, "本验证session") {
		t.Fatalf("original candidate evidence accepted: %+v %s", rejected, message)
	}
	if rejected, message := executeVerification(t, tool, fourthInv, verificationArguments("supported", evidence.EvidenceID, "supporting")); rejected != nil || !strings.Contains(message, "本验证session") {
		t.Fatalf("cross verification evidence accepted: %+v %s", rejected, message)
	}
	fourthHTTPInv := operation.Invocation{SessionID: fourth.SessionID, TurnID: "fourth-http", CallID: "fourth-http-call"}
	_, truncated := executeHTTP(t, httpTool, fourthHTTPInv, "GET", task.Origins[0]+"/truncated", "", "")
	model, err := capability.store.evidenceByID(t.Context(), task.ID, truncated.EvidenceID)
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
	if _, err := capability.store.orm.NewUpdate().Model(model).Column("capture_truncated", "flow_pb").WherePK().Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if rejected, message := executeVerification(t, tool, fourthInv, verificationArguments("supported", truncated.EvidenceID, "supporting")); rejected != nil || !strings.Contains(message, "完整捕获") {
		t.Fatalf("truncated evidence accepted: %+v %s", rejected, message)
	}
	fourthInv.CallID = "fourth-call-limitation"
	if accepted, message := executeVerification(t, tool, fourthInv, verificationArguments("inconclusive", truncated.EvidenceID, "limitation")); accepted == nil {
		t.Fatalf("truncated limitation rejected: %s", message)
	}
}

func TestWebAPIVerificationLegacyPreparation(t *testing.T) {
	database := t.TempDir() + "/hub.sqlite"
	legacy, err := webservice.NewSQLiteStore(database, legacyWebAPISchema())
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := openTaskStore(database)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := store.Save(t.Context(), "legacy-task", "旧任务", "保留旧行", []string{"http://127.0.0.1:8000"})
	if err != nil {
		t.Fatal(err)
	}
	inv := operation.Invocation{SessionID: outcome.Task.InitialSessionID, TurnID: "old-turn", CallID: "old-call"}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8000/old", nil)
	flow := traffic.FlowFromHTTP(request, nil, nil, nil)
	flow.Id = "legacy-evidence"
	claimed, fresh, err := store.claim(t.Context(), inv, outcome.Task.ID, requestSpec{method: "GET", url: request.URL.String(), fingerprint: "old-fingerprint", flow: flow}, "claimed", "旧证据")
	if err != nil || !fresh {
		t.Fatalf("insert old evidence: %+v %v %v", claimed, fresh, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := PrepareWebAPISchema(database); err != nil {
		t.Fatalf("prepare legacy: %v", err)
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
	defer reopened.Close()
	preservedTask, err := reopened.Get(t.Context(), outcome.Task.ID)
	if err != nil || preservedTask == nil || preservedTask.KnownInfo != "旧任务" {
		t.Fatalf("old task lost: %+v %v", preservedTask, err)
	}
	preservedEvidence, err := reopened.evidenceByID(t.Context(), outcome.Task.ID, claimed.ID)
	if err != nil || preservedEvidence == nil || preservedEvidence.Fingerprint != "old-fingerprint" {
		t.Fatalf("old evidence lost: %+v %v", preservedEvidence, err)
	}
	if err := PrepareWebAPISchema(database); err != nil {
		t.Fatalf("complete schema not accepted: %v", err)
	}
	if err := PrepareWebAPISchema(t.TempDir() + "/empty.sqlite"); err != nil {
		t.Fatalf("empty database should remain native-created: %v", err)
	}

	partialDatabase := t.TempDir() + "/partial.sqlite"
	partial, err := webservice.NewSQLiteStore(partialDatabase, legacyWebAPISchema())
	if err != nil {
		t.Fatal(err)
	}
	if err := partial.Close(); err != nil {
		t.Fatal(err)
	}
	partialStore, err := openTaskStore(partialDatabase)
	if err != nil {
		t.Fatal(err)
	}
	// 用明确未知附加表确保既不按旧库也不按新库静默升级。
	if _, err := partialStore.orm.NewCreateTable().Model((*unknownSchemaTable)(nil)).Table("webapi_unknown_extra").Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := partialStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := PrepareWebAPISchema(partialDatabase); err == nil {
		t.Fatal("unknown extra table accepted")
	}
	// 再用明确半成库检查缺第二张验证表的拒绝。
	halfPath := t.TempDir() + "/half.sqlite"
	half, err := webservice.NewSQLiteStore(halfPath, legacyWebAPISchema())
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
	if _, err := halfStore.orm.NewCreateTable().Model((*verificationModel)(nil)).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := halfStore.Close(); err != nil {
		t.Fatal(err)
	}
	if err := PrepareWebAPISchema(halfPath); err == nil {
		t.Fatal("partial verification schema accepted")
	}
}
