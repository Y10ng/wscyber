package webapi

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

func (f *authzFixture) setPolicy(t *testing.T, scope, approval string) {
	t.Helper()
	body := mustToolArgumentsJSON(t, map[string]any{"scope": scope, "approval": approval})
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/"+f.task.ID+"/policy", body); status != 200 {
		t.Fatalf("policy = %d %s", status, raw)
	}
}

func (f *authzFixture) requestCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := f.capability.store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM webapi_requests WHERE task_id = ?`, f.task.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestWebAPIAuthzCompareVariantsEvidenceSourceAndAPI(t *testing.T) {
	f := newAuthzFixture(t)
	original := f.original(t)
	f.inv.CallID = "compare-main"
	result, text := f.invokeCompare(t, f.inv, original.EvidenceID, []authzCompareVariantInput{
		{Label: "用户 B", SetHeaders: map[string]string{"Authorization": "Bearer bob-token"}},
		{Label: "匿名", Anonymous: true},
		{Label: "占位符 B", SetHeaders: map[string]string{"Authorization": "Bearer {{cred:bob.secret}}", "X-Password": "{{cred:bob.secret}}"}},
	})
	if result.IsError {
		t.Fatalf("compare = %s", text)
	}
	for _, want := range []string{"| 用户 B |", "| 匿名 |", "| 占位符 B |", authzHintDifferent, authzHintDenied,
		"不是越权漏洞结论", "webapi_finding", "contrast", "原请求与变体证据"} {
		if !strings.Contains(text, want) {
			t.Fatalf("compare result lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, authzAliceSecret) || strings.Contains(text, authzBobSecret) {
		t.Fatalf("compare result leaked secret: %s", text)
	}
	if f.target.count() != 4 {
		t.Fatalf("target hits = %d, want original + 3 variants", f.target.count())
	}
	hits := f.target.hits
	for _, hit := range hits {
		if hit.method != http.MethodPost || hit.path != "/resource" || hit.body != `{"action":"read"}` {
			t.Fatalf("variant changed method/url/body: %+v", hit)
		}
	}
	if hits[2].header.Get("Authorization") != "" || hits[2].header.Get("Cookie") != "" {
		t.Fatalf("anonymous retained auth headers: %+v", hits[2].header)
	}
	if hits[3].header.Get("Authorization") != "Bearer "+authzBobSecret || hits[3].header.Get("X-Password") != authzBobSecret {
		t.Fatalf("placeholder headers = %+v", hits[3].header)
	}

	rows, err := f.capability.store.listAuthzCompares(t.Context(), f.task.ID, original.EvidenceID)
	if err != nil || len(rows) != 3 {
		t.Fatalf("compare rows = %+v %v", rows, err)
	}
	byLabel := map[string]AuthzCompareItem{}
	for _, row := range rows {
		byLabel[row.Label] = row
		if row.TaskID != f.task.ID || row.SessionID != f.task.InitialSessionID || row.OriginalEvidenceID != original.EvidenceID || row.VariantEvidenceID == "" {
			t.Fatalf("compare row ownership = %+v", row)
		}
		model, err := f.capability.store.evidenceByID(t.Context(), f.task.ID, row.VariantEvidenceID)
		if err != nil || model == nil || model.SessionID != f.task.InitialSessionID || !strings.HasPrefix(model.CallID, authzCompareCallPrefix) {
			t.Fatalf("variant evidence = %+v %v", model, err)
		}
		if metadata := evidenceMetadata(*model); metadata.Source != evidenceSourceAuthz {
			t.Fatalf("variant source = %+v", metadata)
		}
	}
	if byLabel["用户 B"].Status != "sent" || byLabel["用户 B"].Hint != authzHintDifferent || byLabel["用户 B"].HTTPStatus == nil || *byLabel["用户 B"].HTTPStatus != 200 ||
		byLabel["匿名"].Status != "sent" || byLabel["匿名"].Hint != authzHintDenied || byLabel["匿名"].HTTPStatus == nil || *byLabel["匿名"].HTTPStatus != 401 {
		t.Fatalf("compare hints = %+v", byLabel)
	}

	status, value, raw := f.do("GET", "/api/webapi/tasks/t1/authz-compares?evidence_id="+original.EvidenceID, "")
	if status != 200 || len(value["items"].([]any)) != 3 {
		t.Fatalf("filtered API = %d %s", status, raw)
	}
	status, value, raw = f.do("GET", "/api/webapi/tasks/t1/authz-compares", "")
	if status != 200 || len(value["items"].([]any)) != 3 {
		t.Fatalf("all API = %d %s", status, raw)
	}
	if status, _, _ := f.do("GET", "/api/webapi/tasks/missing/authz-compares", ""); status != 404 {
		t.Fatalf("missing task API = %d", status)
	}

	// replay/compare 来源也可作为下一次原证据。
	f.inv.CallID = "compare-of-compare"
	result, text = f.invokeCompare(t, f.inv, byLabel["用户 B"].VariantEvidenceID, []authzCompareVariantInput{{Label: "再次匿名", Anonymous: true}})
	if result.IsError || f.target.count() != 5 {
		t.Fatalf("compare of compare = %s, hits=%d", text, f.target.count())
	}
	_, value, _ = f.do("GET", "/api/webapi/tasks/t1/authz-compares", "")
	if len(value["items"].([]any)) != 4 {
		t.Fatalf("all comparisons after nested compare = %s", value["items"])
	}
}

func TestWebAPIAuthzCompareRejectsInvalidOriginalAndArgumentsWithoutSend(t *testing.T) {
	f := newAuthzFixture(t)
	variant := []authzCompareVariantInput{{Label: "匿名", Anonymous: true}}
	before := f.target.count()
	for _, evidenceID := range []string{"", "missing"} {
		result, text := f.invokeCompare(t, f.inv, evidenceID, variant)
		if !result.IsError || !strings.Contains(text, "0发送") {
			t.Fatalf("missing original %q = %s", evidenceID, text)
		}
	}
	// 未发送 denied 证据不能作为原请求。
	f.inv.CallID = "denied-original"
	_, denied, _ := f.invokeHTTP(t, f.inv, "GET", "http://127.0.0.1:1/denied", "", nil)
	result, text := f.invokeCompare(t, f.inv, denied.EvidenceID, variant)
	if !result.IsError || !strings.Contains(text, "sent=1") {
		t.Fatalf("denied original = %s", text)
	}
	// 其它任务的已发送证据也不能跨任务引用。
	other, err := f.capability.store.Save(t.Context(), "t2", "其它任务", "隔离证据", []string{originOf(t, f.target.server.URL)})
	if err != nil {
		t.Fatal(err)
	}
	otherInv := operation.Invocation{SessionID: other.Task.InitialSessionID, TurnID: "turn-other", CallID: "original"}
	_, otherEvidence, _ := f.invokeHTTP(t, otherInv, "GET", f.target.server.URL+"/resource", "", map[string]string{"Authorization": "Bearer alice-token"})
	before = f.target.count()
	result, text = f.invokeCompare(t, f.inv, otherEvidence.EvidenceID, variant)
	if !result.IsError || !strings.Contains(text, "不存在于本任务") {
		t.Fatalf("cross-task original = %s", text)
	}
	invalidVariants := [][]authzCompareVariantInput{nil, {
		{Label: "1"}, {Label: "2"}, {Label: "3"}, {Label: "4"}, {Label: "5"},
	}, {{Label: ""}}, {{Label: strings.Repeat("名", 41)}}, {{Label: "bad anonymous", Anonymous: true, SetHeaders: map[string]string{"cookie": "sid=x"}}}}
	for _, variants := range invalidVariants {
		result, text := f.invokeCompare(t, f.inv, otherEvidence.EvidenceID, variants)
		if !result.IsError || !strings.Contains(text, "0发送") {
			t.Fatalf("invalid variants = %s", text)
		}
	}
	if f.target.count() != before {
		t.Fatalf("invalid calls sent requests: %d→%d", before, f.target.count())
	}
	rows, err := f.capability.store.listAuthzCompares(t.Context(), f.task.ID, "")
	if err != nil || len(rows) != 0 {
		t.Fatalf("invalid calls wrote comparisons: %+v %v", rows, err)
	}
}

func TestWebAPIAuthzCompareReviewRequiredHasNoRequestEvidence(t *testing.T) {
	f := newAuthzFixture(t)
	f.setPolicy(t, ScopeOrigins, ApprovalOff)
	f.inv.CallID = "delete-original"
	_, original, text := f.invokeHTTP(t, f.inv, "DELETE", f.target.server.URL+"/resource", "", map[string]string{"Authorization": "Bearer alice-token"})
	if original.Status != requestCompleted {
		t.Fatalf("original = %s", text)
	}
	f.setPolicy(t, ScopeOrigins, ApprovalHighRisk)
	beforeHits, beforeRequests := f.target.count(), f.requestCount(t)
	f.inv.CallID = "compare-review"
	result, text := f.invokeCompare(t, f.inv, original.EvidenceID, []authzCompareVariantInput{{Label: "危险变体"}})
	if result.IsError || !strings.Contains(text, "review_required") || !strings.Contains(text, "改用 webapi_http 走人工审批") || !strings.Contains(text, authzHintNotSent) {
		t.Fatalf("review result = %s", text)
	}
	if f.target.count() != beforeHits || f.requestCount(t) != beforeRequests {
		t.Fatalf("review sent/wrote request: hits %d→%d requests %d→%d", beforeHits, f.target.count(), beforeRequests, f.requestCount(t))
	}
	rows, err := f.capability.store.listAuthzCompares(t.Context(), f.task.ID, original.EvidenceID)
	if err != nil || len(rows) != 1 || rows[0].Status != "review_required" || rows[0].VariantEvidenceID != "" || rows[0].Hint != authzHintNotSent || rows[0].HTTPStatus != nil {
		t.Fatalf("review row = %+v %v", rows, err)
	}
	if blocked, err := f.capability.store.unresolvedTurn(t.Context(), f.inv.SessionID, f.inv.TurnID); err != nil || blocked {
		t.Fatalf("review_required blocked turn: %v %v", blocked, err)
	}
}

func TestWebAPIAuthzCompareDeniedOriginRecordsUnsent(t *testing.T) {
	f := newAuthzFixture(t)
	outsider := newAuthzTarget(t)
	f.setPolicy(t, ScopeUnrestricted, ApprovalOff)
	f.inv.CallID = "outside-original"
	_, original, text := f.invokeHTTP(t, f.inv, "GET", outsider.server.URL+"/resource", "", map[string]string{"Authorization": "Bearer alice-token"})
	if original.Status != requestCompleted {
		t.Fatalf("outside original = %s", text)
	}
	f.setPolicy(t, ScopeOrigins, ApprovalOff)
	f.inv.CallID = "outside-compare"
	result, text := f.invokeCompare(t, f.inv, original.EvidenceID, []authzCompareVariantInput{{Label: "越界变体"}})
	if result.IsError || !strings.Contains(text, "| denied |") || outsider.count() != 1 {
		t.Fatalf("denied compare = %s hits=%d", text, outsider.count())
	}
	rows, err := f.capability.store.listAuthzCompares(t.Context(), f.task.ID, original.EvidenceID)
	if err != nil || len(rows) != 1 || rows[0].Status != "denied" || rows[0].Hint != authzHintNotSent || rows[0].VariantEvidenceID == "" {
		t.Fatalf("denied row = %+v %v", rows, err)
	}
	model, err := f.capability.store.evidenceByID(t.Context(), f.task.ID, rows[0].VariantEvidenceID)
	if err != nil || model == nil || model.Status != "denied" || !model.Sent.Valid || model.Sent.Bool || evidenceMetadata(*model).Source != evidenceSourceAuthz {
		t.Fatalf("denied evidence = %+v %v", model, err)
	}
}

func TestWebAPIAuthzCompareUnknownNeverRetries(t *testing.T) {
	f := newAuthzFixture(t)
	f.inv.CallID = "unknown-original"
	_, original, text := f.invokeHTTP(t, f.inv, "GET", f.target.server.URL+"/disconnect", "", map[string]string{"Authorization": "Bearer alice-token"})
	if original.Status != requestCompleted {
		t.Fatalf("original = %s", text)
	}
	variant := []authzCompareVariantInput{{Label: "断开", SetHeaders: map[string]string{"Authorization": "Bearer bob-token"}}}
	f.inv.CallID = "unknown-compare"
	result, text := f.invokeCompare(t, f.inv, original.EvidenceID, variant)
	if result.IsError || !strings.Contains(text, "| unknown |") || !strings.Contains(text, "不自动重试") || f.target.count() != 2 {
		t.Fatalf("unknown compare = %s hits=%d", text, f.target.count())
	}
	if blocked, err := f.capability.store.unresolvedTurn(t.Context(), f.inv.SessionID, f.inv.TurnID); err != nil || !blocked {
		t.Fatalf("unknown did not block turn: %v %v", blocked, err)
	}
	rows, err := f.capability.store.listAuthzCompares(t.Context(), f.task.ID, original.EvidenceID)
	if err != nil || len(rows) != 1 || rows[0].Status != "unknown" || rows[0].Hint != authzHintNotSent || rows[0].VariantEvidenceID == "" {
		t.Fatalf("unknown row = %+v %v", rows, err)
	}
	// 新调用也不得绕过同指纹 unknown 保护；不新增目标请求或请求证据，只追加一条对比记录。
	beforeRequests := f.requestCount(t)
	f.inv.CallID = "unknown-compare-retry"
	result, text = f.invokeCompare(t, f.inv, original.EvidenceID, variant)
	if result.IsError || !strings.Contains(text, "不自动重试") || f.target.count() != 2 || f.requestCount(t) != beforeRequests {
		t.Fatalf("unknown retry = %s hits=%d requests=%d", text, f.target.count(), f.requestCount(t))
	}
	rows, err = f.capability.store.listAuthzCompares(t.Context(), f.task.ID, original.EvidenceID)
	if err != nil || len(rows) != 2 || rows[0].VariantEvidenceID != rows[1].VariantEvidenceID {
		t.Fatalf("unknown retry rows = %+v %v", rows, err)
	}
}

func TestWebAPIAuthzComparisonHintBranches(t *testing.T) {
	flow := func(status int32, body, location string) *traffic.Flow {
		headers := []*traffic.Header{}
		if location != "" {
			headers = append(headers, &traffic.Header{Name: "Location", Value: location})
		}
		return &traffic.Flow{Request: &traffic.HttpRequest{Method: "GET", Url: "https://api.example.test/object"}, Response: &traffic.HttpResponse{StatusCode: status, Headers: headers, Body: []byte(body)}}
	}
	status := func(value int64) *int64 { return &value }
	cases := []struct {
		name                string
		originalStatus      int64
		original, variant   *traffic.Flow
		status              *int64
		wantHint            string
		wantSame, wantIdent bool
		wantRatio           float64
	}{
		{"identical", 200, flow(200, "abc", ""), flow(200, "abc", ""), status(200), authzHintSame, true, true, 1},
		{"similar length", 200, flow(200, strings.Repeat("a", 100), ""), flow(200, strings.Repeat("b", 105), ""), status(200), authzHintSame, true, false, 1.05},
		{"different", 200, flow(200, "a", ""), flow(200, "much longer response", ""), status(200), authzHintDifferent, true, false, 20},
		{"unauthorized wins over same", 403, flow(403, "same", ""), flow(403, "same", ""), status(403), authzHintDenied, true, true, 1},
		{"login redirect", 200, flow(200, "private", ""), flow(302, "", "HTTPS://api.example.test/LOGIN?next=/object"), status(302), authzHintDenied, false, false, 0},
		{"other redirect", 200, flow(200, "private", ""), flow(302, "", "/other"), status(302), authzHintDifferent, false, false, 0},
		{"not found", 404, flow(404, "missing", ""), flow(404, "missing", ""), status(404), authzHintNotFound, true, true, 1},
		{"both empty", 200, flow(200, "", ""), flow(200, "", ""), status(200), authzHintSame, true, true, 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := AuthzCompareVariant{HTTPStatus: test.status, BodyBytes: len(test.variant.GetResponse().GetBody())}
			fillAuthzComparison(&result, test.originalStatus, test.original, test.variant)
			if result.Hint != test.wantHint || result.SameStatus != test.wantSame || result.BodyIdentical != test.wantIdent || result.LengthRatio != test.wantRatio {
				t.Fatalf("comparison = %+v, want hint=%s same=%t identical=%t ratio=%v", result, test.wantHint, test.wantSame, test.wantIdent, test.wantRatio)
			}
		})
	}
	if got := authzComparisonHint(false, false, 0, nil, nil); got != authzHintNotSent {
		t.Fatalf("not sent hint = %s", got)
	}
}

func TestWebAPIAuthzCompareAvailableToSubsession(t *testing.T) {
	f := newAuthzFixture(t)
	found := false
	for _, name := range oracleExplorerTools {
		found = found || name == authzCompareToolName
	}
	if !found || !strings.Contains(oracleExplorerSystemPrompt("O-001"), authzCompareToolName) {
		t.Fatalf("subagent tools/prompt = %v", oracleExplorerTools)
	}
	child := &taskSubsessionModel{SessionID: "child-authz", TaskID: f.task.ID, ParentSessionID: f.task.InitialSessionID,
		ParentCallID: "parent-call", OracleID: "synthetic", AgentName: OracleExplorerName, Status: subsessionRunning, CreatedAt: nowRFC3339()}
	if _, err := f.capability.store.orm.NewInsert().Model(child).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	inv := operation.Invocation{SessionID: child.SessionID, TurnID: "child-turn", CallID: "child-original"}
	_, original, text := f.invokeHTTP(t, inv, "GET", f.target.server.URL+"/resource", "", map[string]string{"Authorization": "Bearer alice-token"})
	if original.Status != requestCompleted {
		t.Fatalf("child original = %s", text)
	}
	inv.CallID = "child-compare"
	result, text := f.invokeCompare(t, inv, original.EvidenceID, []authzCompareVariantInput{{Label: "子会话匿名", Anonymous: true}})
	if result.IsError {
		t.Fatalf("child compare = %s", text)
	}
	rows, err := f.capability.store.listAuthzCompares(t.Context(), f.task.ID, original.EvidenceID)
	if err != nil || len(rows) != 1 || rows[0].SessionID != child.SessionID {
		t.Fatalf("child compare rows = %+v %v", rows, err)
	}
	model, err := f.capability.store.evidenceByID(t.Context(), f.task.ID, rows[0].VariantEvidenceID)
	if err != nil || model == nil || model.SessionID != child.SessionID || evidenceMetadata(*model).Source != evidenceSourceAuthz {
		t.Fatalf("child evidence = %+v %v", model, err)
	}
}

func TestWebAPIAuthzCompareNotOverallApprovalGated(t *testing.T) {
	f := newAuthzFixture(t)
	f.inv.CallID = "read-original"
	_, original, text := f.invokeHTTP(t, f.inv, "GET", f.target.server.URL+"/resource", "", map[string]string{"Authorization": "Bearer alice-token"})
	if original.Status != requestCompleted {
		t.Fatalf("original = %s", text)
	}
	f.setPolicy(t, ScopeOrigins, ApprovalAll)
	h := newApprovalHarness(t, f.capability)
	inv := f.inv
	inv.CallID = "approval-all-compare"
	args := mustToolArgumentsJSON(t, authzCompareArguments{EvidenceID: original.EvidenceID, Variants: []authzCompareVariantInput{{Label: "匿名", Anonymous: true}}})
	result, err := h.execute(t.Context(), inv, authzCompareToolName, args, f.compare.Execute)
	if err != nil || result.IsError || !strings.Contains(coretool.ResultText(result), "| sent |") || len(h.states()) != 0 {
		t.Fatalf("approval=all compare result=%s err=%v states=%v", coretool.ResultText(result), err, h.states())
	}
}

func TestWebAPIAuthzCompareSchemaUpgradeFromM5aAndListLimit(t *testing.T) {
	database := filepath.Join(t.TempDir(), "hub.sqlite")
	stage, err := webservice.NewSQLiteStore(database, webapiSchemaStage(41, 37))
	if err != nil {
		t.Fatal(err)
	}
	_ = stage.Close()
	store, err := openTaskStore(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(t.Context(), "m5a-task", "旧任务", "M5a 阶段保留行", []string{"https://api.example.test"}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	if err := PrepareWebAPISchema(database); err != nil {
		t.Fatalf("prepare M5a stage: %v", err)
	}
	complete, err := webservice.NewSQLiteStore(database, SchemaModule())
	if err != nil {
		t.Fatalf("full schema after M5a upgrade: %v", err)
	}
	_ = complete.Close()
	reopened, err := openTaskStore(database)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if task, err := reopened.Get(t.Context(), "m5a-task"); err != nil || task == nil {
		t.Fatalf("old task lost: %v", err)
	}
	base := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	for index := 0; index < 105; index++ {
		row := &authzCompareModel{ID: fmt.Sprintf("row-%03d", index), TaskID: "m5a-task", SessionID: "s", OriginalEvidenceID: fmt.Sprintf("original-%d", index%2),
			Label: "合成", Status: "review_required", Hint: authzHintNotSent, CreatedAt: base.Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano)}
		if _, err := reopened.orm.NewInsert().Model(row).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	items, err := reopened.listAuthzCompares(t.Context(), "m5a-task", "")
	if err != nil || len(items) != 100 || items[0].ID != "row-104" || items[99].ID != "row-005" {
		t.Fatalf("limited items = %d first=%+v last=%+v err=%v", len(items), items[0], items[len(items)-1], err)
	}
	filtered, err := reopened.listAuthzCompares(t.Context(), "m5a-task", "original-0")
	if err != nil || len(filtered) != 53 {
		t.Fatalf("filtered items = %d %v", len(filtered), err)
	}
	if err := PrepareWebAPISchema(database); err != nil {
		t.Fatalf("complete schema not accepted: %v", err)
	}
}
