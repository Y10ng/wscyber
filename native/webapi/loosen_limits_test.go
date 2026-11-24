package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/chainreactors/cyber/agent"
	aop "github.com/chainreactors/cyber/aop"
	toolpb "github.com/chainreactors/cyber/aop/tool"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/core/truncate"
)

// 放宽过紧限制（11-24）：全部为临时库合成数据（example.test 演示资产、虚构接口、本机 fixture），不调用真实模型。

func loosenServe(mux http.Handler, method, path, body string) (int, string) {
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(method, path, strings.NewReader(body)))
	return recorder.Code, recorder.Body.String()
}

// 候选 20 个 steps / 20 条证据、报告草稿单项 20 条证据都能保存，报告导出不报错。
func TestLoosenFindingAndReportDraftManyEvidence(t *testing.T) {
	capability, httpTool, task, inv, _, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("neutral fixture " + r.URL.Path))
	}))
	ids := make([]string, 0, 20)
	for index := 0; index < 20; index++ {
		inv.CallID = fmt.Sprintf("http-%d", index)
		_, evidence := executeHTTP(t, httpTool, inv, "GET", fmt.Sprintf("%s/items/%d", task.Origins[0], index), "", "")
		if evidence.Status != requestCompleted {
			t.Fatalf("fixture evidence %d = %+v", index, evidence)
		}
		ids = append(ids, evidence.EvidenceID)
	}
	type step struct {
		Text            string `json:"text"`
		EvidenceIndices []int  `json:"evidence_indices"`
	}
	type evidenceRef struct {
		EvidenceID string `json:"evidence_id"`
		Relation   string `json:"relation"`
	}
	steps, refs := []step{}, []evidenceRef{}
	all := []int{}
	for index, id := range ids {
		steps = append(steps, step{Text: fmt.Sprintf("请求第 %d 个演示条目", index), EvidenceIndices: []int{index}})
		refs = append(refs, evidenceRef{EvidenceID: id, Relation: "supporting"})
		all = append(all, index)
	}
	steps[0].EvidenceIndices = all
	raw, _ := json.Marshal(map[string]any{"content": map[string]any{"title": "演示条目批量可读", "description": "中性主张", "steps": steps, "evidence": refs}})
	finding, message := executeFinding(t, &findingTool{store: capability.store}, operation.Invocation{SessionID: inv.SessionID, TurnID: "finding-turn", CallID: "finding-20"}, string(raw))
	if finding == nil || len(finding.Evidence) != 20 || finding.Status != "candidate" {
		t.Fatalf("finding with 20 steps/evidence rejected: %s", message)
	}

	mux := http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	base := "/api/webapi/tasks/" + task.ID + "/reports/" + reportID
	status, body := loosenServe(mux, http.MethodPut, base, fmt.Sprintf(`{"items":[{"finding_id":%q,"revision_id":%q}]}`, finding.FindingID, finding.RevisionID))
	if status != 201 {
		t.Fatalf("PUT report = %d %s", status, body)
	}
	var created struct {
		Report ReportSummary `json:"report"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil || created.Report.SessionID == "" {
		t.Fatalf("report body = %s %v", body, err)
	}
	draft, _ := json.Marshal(map[string]any{"title": "演示报告", "overview": "中性概述", "items": []map[string]any{{
		"finding_id": finding.FindingID, "description": "中性描述", "reproduction": "依次请求 20 个演示条目", "evidence_ids": ids,
	}}})
	saved, message := executeReportDraft(t, capability.store, operation.Invocation{SessionID: created.Report.SessionID, TurnID: "report-turn", CallID: "draft-20"}, string(draft))
	if saved == nil || saved["report_id"] != reportID {
		t.Fatalf("draft with 20 evidence rejected: %s", message)
	}
	for _, format := range []string{"markdown", "json"} {
		if status, body := loosenServe(mux, http.MethodGet, base+"/export?format="+format, ""); status != 200 || !strings.Contains(body, ids[19]) {
			t.Fatalf("export %s = %d %s", format, status, body[:min(len(body), 300)])
		}
	}
}

// supported 带 5 个 key_steps、5 条 remediation、800 字 retest_method 能保存；漏洞卡、导出、证据图、推送预览对其不报错。
func TestLoosenVerificationReproManyStepsDownstream(t *testing.T) {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	f.launchManual("a1", "t1")
	fid, frev := f.addFinding("t1", "演示订单接口越权读取", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))
	v := f.newVerification("t1", fid, frev)
	evidence := []requestModel{}
	for index := 0; index < 5; index++ {
		evidence = append(evidence, f.addEvidence("t1", v.SessionID, cardFlow(fmt.Sprintf("https://api.example.test/orders/%d", 1002+index), "sid=demo", fmt.Sprintf(`{"owner":"user-%d"}`, index))))
	}
	steps := []map[string]any{}
	remediation := []string{}
	for index, item := range evidence {
		steps = append(steps, map[string]any{"title": fmt.Sprintf("第 %d 步：%s", index+1, strings.Repeat("读取演示订单", 20)), "evidence_id": item.ID, "response_marks": []string{fmt.Sprintf(`"owner":"user-%d"`, index)}})
		remediation = append(remediation, fmt.Sprintf("修复要点 %d：%s", index+1, strings.Repeat("校验订单归属", 40)))
	}
	retest := strings.Repeat("复", 800)
	payload := reproPayload(evidence[0].ID)
	payload["impact"] = strings.Repeat("影响", 150)
	payload["key_steps"] = steps
	payload["contrast"] = map[string]any{"normal_evidence_id": evidence[1].ID, "abnormal_evidence_id": evidence[0].ID, "note": strings.Repeat("差异", 150)}
	payload["remediation"] = remediation
	payload["retest_method"] = retest
	payload["suggested_severity"] = "high"
	payload["severity_reason"] = strings.Repeat("理由", 150)
	if result, message := f.callVerification(v, payload); result == nil || result["verdict"] != "supported" {
		t.Fatalf("loosened repro rejected: %s", message)
	}
	view, err := f.capability.store.verificationResult(t.Context(), v.VerificationID)
	if err != nil || view == nil || len(view.Payload.KeySteps) != 5 || len(view.Payload.Remediation) != 5 || utf8.RuneCountInString(view.Payload.RetestMethod) != 800 {
		t.Fatalf("stored payload = %+v %v", view, err)
	}

	key := vulnOf(f.vulns("a1", ""), fid).ID
	card := f.card(key)
	if card.Repro == nil || len(card.Repro.Steps) != 5 || len(card.Repro.Remediation) != 5 || card.Repro.RetestMethod != retest {
		t.Fatalf("card repro = %+v", card.Repro)
	}
	if last := card.Repro.Steps[4]; last.EvidenceID != evidence[4].ID || last.URL != "https://api.example.test/orders/1006" {
		t.Fatalf("card step 5 = %+v", last)
	}
	for _, path := range []string{
		"/api/webapi/vulns/" + key + "/card/export?format=markdown",
		"/api/webapi/vulns/" + key + "/card/export?format=json",
		"/api/webapi/vulns/" + key + "/card/notify/preview",
		"/api/webapi/vulns/" + key + "/card/curl?step=3",
		"/api/webapi/tasks/t1/evidence/" + evidence[4].ID + "/image.png?mark=" + `%22owner%22:%22user-4%22`,
	} {
		if status, raw, _ := f.export("GET", path, ""); status != 200 {
			t.Fatalf("GET %s = %d %s", path, status, raw[:min(len(raw), 300)])
		}
	}
	if status, body, _ := f.export("GET", "/api/webapi/vulns/"+key+"/card/export?format=markdown", ""); status != 200 || !strings.Contains(body, "第 5 步") || !strings.Contains(body, "修复要点 5") {
		t.Fatalf("markdown lacks step/remediation 5:\n%s", body[:min(len(body), 600)])
	}
	raw, _ := json.Marshal(map[string]any{"keys": []string{key}, "format": "markdown"})
	if status, body, _ := f.export("POST", "/api/webapi/vulns/export", string(raw)); status != 200 || !strings.Contains(body, "第 5 步") {
		t.Fatalf("batch export = %d", status)
	}
	// 纠正 1：curl/重放的 step 范围按条目实际步数（此处 5 步）：step=5 可用，step=6 返回 400。
	if status, raw, _ := f.export("GET", "/api/webapi/vulns/"+key+"/card/curl?step=5", ""); status != 200 || !strings.Contains(raw, "/orders/1006") {
		t.Fatalf("curl step 5 = %d %s", status, raw)
	}
	if status, raw, _ := f.export("GET", "/api/webapi/vulns/"+key+"/card/curl?step=6", ""); status != 400 || !strings.Contains(raw, "step 必须是 1..5") {
		t.Fatalf("curl step 6 = %d %s", status, raw)
	}
	if status, raw, _ := f.export("POST", "/api/webapi/vulns/"+key+"/card/replay", `{"step":6}`); status != 400 || !strings.Contains(raw, "step 必须是 1..5") {
		t.Fatalf("replay step 6 = %d %s", status, raw)
	}
}

// 存证与模型结果：10KiB 文本完整给模型；70KiB 存证只存前 64KiB（证据详情完整返回），
// 给模型的结果按传输预算缩短 preview（纠正 2），整体 ≤ modelToolResultBudget（48KiB）、是完整 JSON，两个截断标记照实。
func TestLoosenHTTPPreviewMatchesCapture(t *testing.T) {
	ten := strings.Repeat("0123456789abcdef", 640)       // 10KiB
	seventy := strings.Repeat("fedcba9876543210", 70*64) // 70KiB ASCII
	capability, tool, task, inv, _, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.URL.Path == "/seventy" {
			_, _ = w.Write([]byte(seventy))
			return
		}
		_, _ = w.Write([]byte(ten))
	}))
	inv.CallID = "ten"
	result, item := executeHTTP(t, tool, inv, "GET", task.Origins[0]+"/ten", "", "")
	if result.IsError || item.Preview != ten || len(item.Preview) != 10*1024 || item.PreviewTruncated || item.CaptureTruncated {
		t.Fatalf("10KiB preview len=%d preview_truncated=%v capture_truncated=%v", len(item.Preview), item.PreviewTruncated, item.CaptureTruncated)
	}
	inv.CallID = "seventy"
	result, item = executeHTTP(t, tool, inv, "GET", task.Origins[0]+"/seventy", "", "")
	text := coretool.ResultText(result)
	if result.IsError || len(text) > modelToolResultBudget || !json.Valid([]byte(text)) || item.EvidenceID == "" ||
		!item.CaptureTruncated || !item.PreviewTruncated || item.Preview == "" || !strings.HasPrefix(seventy, item.Preview) {
		t.Fatalf("70KiB tool result len=%d valid=%v evidence=%q preview_len=%d preview_truncated=%v capture_truncated=%v",
			len(text), json.Valid([]byte(text)), item.EvidenceID, len(item.Preview), item.PreviewTruncated, item.CaptureTruncated)
	}
	// 证据列表/详情接口：存证 preview 仍为完整 64KiB。
	mux := http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	if status, body := loosenServe(mux, http.MethodGet, "/api/webapi/tasks/"+task.ID+"/evidence?limit=200", ""); status != 200 || !strings.Contains(body, item.EvidenceID) {
		t.Fatalf("evidence list = %d", status)
	}
	status, body := loosenServe(mux, http.MethodGet, "/api/webapi/tasks/"+task.ID+"/evidence/"+item.EvidenceID, "")
	var detail struct {
		Preview string         `json:"preview"`
		Capture map[string]any `json:"capture"`
	}
	if status != 200 || json.Unmarshal([]byte(body), &detail) != nil || detail.Preview != seventy[:maxCaptureBytes] || detail.Capture["preview_limit"] != float64(maxCaptureBytes) {
		t.Fatalf("evidence detail = %d preview_len=%d capture=%v", status, len(detail.Preview), detail.Capture)
	}
}

// 纠正 2：大量需 JSON 转义的字符（引号、换行、控制字符）撑大序列化体积时，结果仍是完整 JSON 且 ≤ 预算。
func TestLoosenHTTPToolResultFitsBudgetWithEscapes(t *testing.T) {
	escaped := strings.Repeat("\"\n\x01<", 64*1024/4) // 64KiB，序列化后约 4 倍
	capability, tool, task, inv, _, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(escaped))
	}))
	inv.CallID = "escaped"
	result, item := executeHTTP(t, tool, inv, "GET", task.Origins[0]+"/escaped", "", "")
	text := coretool.ResultText(result)
	if result.IsError || len(text) > modelToolResultBudget || !json.Valid([]byte(text)) || item.EvidenceID == "" ||
		!item.PreviewTruncated || item.CaptureTruncated || item.Preview == "" || !strings.HasPrefix(escaped, item.Preview) {
		t.Fatalf("escaped tool result len=%d valid=%v preview_len=%d preview_truncated=%v capture_truncated=%v",
			len(text), json.Valid([]byte(text)), len(item.Preview), item.PreviewTruncated, item.CaptureTruncated)
	}
	model, err := capability.store.evidenceByID(t.Context(), task.ID, item.EvidenceID)
	if err != nil || model == nil || string(model.Preview) != escaped || model.PreviewTruncated {
		t.Fatalf("stored preview changed: len=%d truncated=%v err=%v", len(model.Preview), model.PreviewTruncated, err)
	}
	// base64 预览缩短时按 4 字节对齐，仍可解码。
	cut := cutModelPreview("base64:"+strings.Repeat("QUJD", 100), 7+10)
	if cut != "base64:QUJDQUJD" {
		t.Fatalf("base64 cut = %q", cut)
	}
}

// singleToolExecutor 把一个工具挂成上游 Executor，用于走上游 ExecuteToolRequest（含 boundToolResultOutput）。
type singleToolExecutor struct{ tool coretool.Tool }

func (e singleToolExecutor) ToolDefinitions() []*aop.ToolDefinition {
	return []*aop.ToolDefinition{e.tool.Definition()}
}

func (e singleToolExecutor) ExecuteTool(ctx context.Context, _ string, arguments string) (*aop.ToolResult, error) {
	return e.tool.Execute(ctx, arguments)
}

// 纠正 2：走上游真实的工具结果传输路径，64KiB 满预览的证据结果不触发上游截断标记，JSON 完整可解析。
func TestLoosenHTTPToolResultSurvivesUpstreamBudget(t *testing.T) {
	seventy := strings.Repeat("fedcba9876543210", 70*64)
	_, tool, task, inv, _, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(seventy))
	}))
	arguments, _ := json.Marshal(map[string]string{"method": "GET", "url": task.Origins[0] + "/list?size=2000"})
	event, err := coretool.ExecuteToolRequest(t.Context(), "op-upstream-budget", &toolpb.Call{SessionId: inv.SessionID, TurnId: inv.TurnID,
		Call: &aop.ToolCall{Id: "op-upstream-budget", Name: "webapi_http", Arguments: &aop.EncodedValue{Data: arguments, MediaType: "application/json"}}}, singleToolExecutor{tool: tool}, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := coretool.ResultText(event.GetToolResult())
	var decoded toolEvidenceResult
	if strings.Contains(text, "output too large") || strings.Contains(text, "[output truncated]") || json.Unmarshal([]byte(text), &decoded) != nil ||
		decoded.EvidenceID == "" || !decoded.CaptureTruncated || !decoded.PreviewTruncated {
		t.Fatalf("upstream bounded result len=%d tail=%q", len(text), text[max(0, len(text)-200):])
	}
	// 第二道边界：agent 循环给模型的投影按 agent.DefaultMaxResultSize 截头（与 modelToolResultMessage 同参数）。
	if projected := truncate.Head(text, truncate.Options{MaxBytes: agent.DefaultMaxResultSize}); projected.Truncated {
		t.Fatalf("model projection truncates result: len=%d limit=%d", len(text), agent.DefaultMaxResultSize)
	}
	if modelToolResultBudget >= agent.DefaultMaxResultSize || modelToolResultBudget >= 64<<10 {
		t.Fatalf("modelToolResultBudget=%d must stay below both upstream bounds", modelToolResultBudget)
	}
}

// 纠正 1：64KiB+ 中文文本被截断、截断点落在多字节字符中间时，预览仍是文本（不是 base64），两个截断标记照实为 true。
func TestLoosenHTTPPreviewTruncatedMultibyteStaysText(t *testing.T) {
	chinese := strings.Repeat("中文演示", 70*1024/12+1) // 每字 3 字节；65536 不是 3 的倍数，截断点落在字符中间
	if maxCaptureBytes%3 == 0 {
		t.Fatal("fixture assumes the capture limit splits a 3-byte rune")
	}
	capability, tool, task, inv, _, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(chinese))
	}))
	inv.CallID = "chinese"
	result, item := executeHTTP(t, tool, inv, "GET", task.Origins[0]+"/chinese", "", "")
	want := chinese[:maxCaptureBytes/3*3]
	// 给模型的结果按传输预算缩短（纠正 2），仍是文本、rune 边界完整；存证 preview 为完整文本。
	if result.IsError || strings.HasPrefix(item.Preview, "base64:") || item.Preview == "" || !strings.HasPrefix(want, item.Preview) || !utf8.ValidString(item.Preview) ||
		!item.PreviewTruncated || !item.CaptureTruncated {
		t.Fatalf("preview len=%d base64=%v preview_truncated=%v capture_truncated=%v", len(item.Preview), strings.HasPrefix(item.Preview, "base64:"), item.PreviewTruncated, item.CaptureTruncated)
	}
	stored, err := capability.store.evidenceByID(t.Context(), task.ID, item.EvidenceID)
	if err != nil || stored == nil || string(stored.Preview) != want || !stored.PreviewTruncated || !stored.CaptureTruncated {
		t.Fatalf("stored preview len=%d err=%v", len(stored.Preview), err)
	}
	// 未截断的二进制与真正的非法 UTF-8 不受影响：仍按二进制给 base64。
	if preview, _ := responsePreview(append([]byte(strings.Repeat("a", 10)), 0xff), "text/plain", true); !strings.HasPrefix(string(preview), "base64:") {
		t.Fatalf("invalid tail treated as text: %q", preview)
	}
	if preview, truncated := responsePreview([]byte("完整文本"), "text/plain", false); string(preview) != "完整文本" || truncated {
		t.Fatalf("short text = %q %v", preview, truncated)
	}
}
