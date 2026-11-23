package webapi

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/aop/traffic"
)

// K2 漏洞报告导出测试：全部为临时库合成数据（example.test 演示资产、虚构候选、合成 Flow，无真实载荷）。

func (f *notifyFixture) export(method, path, body string) (int, string, map[string]string) {
	f.t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, request)
	headers := map[string]string{}
	for _, name := range []string{"Content-Type", "Cache-Control", "Content-Disposition"} {
		headers[name] = recorder.Header().Get(name)
	}
	return recorder.Code, recorder.Body.String(), headers
}

func exportFlow(rawURL string, requestBody string, response []byte, headers ...*traffic.Header) *traffic.Flow {
	return &traffic.Flow{
		Id: "we_export", Complete: true,
		Request: &traffic.HttpRequest{Method: "POST", Url: rawURL, Protocol: "HTTP/1.1", Body: []byte(requestBody), Headers: append([]*traffic.Header{
			{Name: "Host", Value: "api.example.test"}, {Name: "Cookie", Value: "sid=fixture-session-abcdef"}, {Name: "Authorization", Value: "Bearer fixture-token"},
		}, headers...)},
		Response: &traffic.HttpResponse{StatusCode: 200, ReasonPhrase: "OK",
			Headers: []*traffic.Header{{Name: "Content-Type", Value: "application/json"}, {Name: "Set-Cookie", Value: "sid=fixture-rotated"}}, Body: response},
	}
}

type exportCase struct {
	f                *notifyFixture
	reproKey         string
	legacyKey        string
	emptyKey         string
	steps            []requestModel
	verificationID   string
	normal, abnormal requestModel
}

func newExportCase(t *testing.T) *exportCase {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"普通用户","username":"demo-user","secret":"Fixture-Pass-9"}`); status != 200 {
		t.Fatalf("credential = %d %s", status, raw)
	}
	f.launchManual("a1", "t1")
	c := &exportCase{f: f}

	fid, frev := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("POST", "https://api.example.test/orders/1002"))
	v := f.newVerification("t1", fid, frev)
	c.verificationID = v.VerificationID
	step1 := f.addEvidence("t1", v.SessionID, exportFlow("https://api.example.test/orders/1002?expand=1&pw=Fixture-Pass-9", `{"note":"demo-user / Fixture-Pass-9"}`,
		[]byte("{\"owner\":\"user-42\",\"md\":\"````code````\"}"), &traffic.Header{Name: "X-Trace", Value: "trace Fixture-Pass-9"}))
	step2 := f.addEvidence("t1", v.SessionID, exportFlow("https://api.example.test/orders/1002/items", "", []byte(strings.Repeat("a", 20000))))
	step3 := f.addEvidence("t1", v.SessionID, exportFlow("https://api.example.test/orders/1002/pdf", "", []byte{0, 1, 2, 3}))
	c.normal = f.addEvidence("t1", v.SessionID, exportFlow("https://api.example.test/orders/1001", "", []byte(`{"owner":"demo-user"}`)))
	c.abnormal = step1
	c.steps = []requestModel{step1, step2, step3}
	payload := map[string]any{
		"verdict": "supported", "method": "以普通用户重放订单请求", "summary": "他人订单可读", "limitations": "仅验证单个订单", "preconditions": "普通用户已登录（口令 Fixture-Pass-9）",
		"evidence": []map[string]any{{"evidence_id": step1.ID, "relation": "supporting"}},
		"impact":   "普通用户可读取他人订单",
		"key_steps": []map[string]any{
			{"title": "读取他人订单", "evidence_id": step1.ID, "request_marks": []string{"sid=fixture-session-abcdef", "pw=Fixture-Pass-9", "not-in-original"}, "response_marks": []string{`"owner":"user-42"`}},
			{"title": "读取订单明细", "evidence_id": step2.ID},
			{"title": "下载订单 PDF", "evidence_id": step3.ID},
		},
		"contrast":           map[string]any{"normal_evidence_id": c.normal.ID, "abnormal_evidence_id": step1.ID, "note": "本人订单与他人订单都返回 200"},
		"remediation":        []string{"按会话用户校验订单归属", "统一鉴权中间件"},
		"retest_method":      "重放第 1 步，期望 403",
		"suggested_severity": "high", "severity_reason": "可批量读取",
	}
	if result, message := f.callVerification(v, payload); result == nil {
		t.Fatalf("verification rejected: %s", message)
	}
	// 原件捕获截断（保存后标记，模拟只保存了前一部分响应）。
	if _, err := f.capability.store.db().ExecContext(t.Context(), `UPDATE webapi_requests SET capture_truncated = true WHERE id = ?`, step2.ID); err != nil {
		t.Fatal(err)
	}
	f.cardRetest("t1", fid, frev, "reproducible")
	c.reproKey = vulnOf(f.vulns("a1", ""), fid).ID

	legacy, legacyRev := f.addFinding("t1", "调试接口暴露", notifyContent(), evidenceAt("GET", "https://api.example.test/debug"))
	f.legacyVerify("t1", legacy, legacyRev)
	c.legacyKey = vulnOf(f.vulns("a1", ""), legacy).ID

	empty, _ := f.addFinding("t1", "导出接口未鉴权", notifyContent(), evidenceAt("GET", "https://api.example.test/export"))
	c.emptyKey = vulnOf(f.vulns("a1", ""), empty).ID
	return c
}

func TestVulnExportSingleMarkdown(t *testing.T) {
	c := newExportCase(t)
	f := c.f
	card := f.card(c.reproKey)
	status, body, headers := f.export("GET", "/api/webapi/vulns/"+c.reproKey+"/card/export?format=markdown", "")
	if status != 200 || headers["Content-Type"] != "text/markdown; charset=utf-8" || headers["Cache-Control"] != "no-store" ||
		headers["Content-Disposition"] != `attachment; filename="vuln-`+card.Header.Code+`.md"` {
		t.Fatalf("export = %d %+v %s", status, headers, body)
	}
	for _, secret := range []string{"Fixture-Pass-9", "fixture-token", "fixture-session-abcdef", "fixture-rotated"} {
		if strings.Contains(body, secret) {
			t.Fatalf("export leaks %q:\n%s", secret, body)
		}
	}
	if !strings.HasPrefix(body, "> "+vulnExportDeclaration+"\n\n## "+card.Header.Code+" "+card.Header.TitleTemplate+"\n") {
		t.Fatalf("head =\n%s", body[:min(len(body), 400)])
	}
	if strings.Contains(body, "## 汇总") {
		t.Fatal("single export must not have summary table")
	}
	for _, want := range []string{
		"- 资产：演示库存系统\n- 位置：POST https://api.example.test/orders/{id}\n- 等级：未定级（AI 建议：高危）\n- 状态：新发现\n- 处置：待处理\n- 负责人：测试组\n",
		"- 首次发现：" + f.capability.notifyLocalTime(card.Header.FirstSeenAt) + "\n",
		"### 影响\n\n普通用户可读取他人订单\n",
		"### 前置条件\n\n普通用户已登录（口令 ******）\n",
		"#### 第 1 步：读取他人订单\n\n`POST https://api.example.test/orders/1002?expand=1&pw=******` → 200（证据 " + c.steps[0].ID + "）\n",
		"关键片段：\n\n- 请求：`******`\n- 请求：`pw=******`\n- 响应：`\"owner\":\"user-42\"`\n- " + vulnExportMarkMissing + "\n",
		// 响应里有 4 个连续反引号：围栏 5 个。
		"`````http\nHTTP/1.1 200 OK\nContent-Type: application/json\nSet-Cookie: ******\n\n{\"owner\":\"user-42\",\"md\":\"````code````\"}\n`````\n",
		"```http\nPOST /orders/1002?expand=1&pw=****** HTTP/1.1\nHost: api.example.test\nCookie: ******\nAuthorization: ******\nX-Trace: trace ******\n\n{\"note\":\"demo-user / ******\"}\n```\n",
		"遮盖版 curl：\n\n```bash\n# 已遮盖敏感头与测试账号 secret（******），重放前请替换为有效值\ncurl -sS -X POST 'https://api.example.test/orders/1002?expand=1&pw=******' \\\n",
		"  -H 'Authorization: ******' \\\n",
		"> 原件捕获已截断：响应正文只保存了前 20000 字节。\n",
		"> 响应正文超过 16 KiB，已截断（原 20000 字节）。\n",
		"> 响应正文为二进制（4 字节），未内联。\n",
		"### 正常对照\n\n本人订单与他人订单都返回 200\n\n- 正常：`POST https://api.example.test/orders/1001` → 200\n- 异常：`POST https://api.example.test/orders/1002?expand=1&pw=******` → 200\n",
		"### 修复建议\n\n1. 按会话用户校验订单归属\n2. 统一鉴权中间件\n",
		"### 复测方法\n\n重放第 1 步，期望 403\n",
		"### 最近复测结论\n\n仍可复现（",
		"### 最近重放\n\n暂无重放\n",
		"### 验证记录\n\n- " + card.Appendix.Verifications[0].FindingNumber + " · 支持（",
		"  - 方法：以普通用户重放订单请求\n  - 局限：仅验证单个订单\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in\n%s", want, body)
		}
	}
	// 截断后的正文正好 16 KiB；二进制正文不内联。
	if !strings.Contains(body, "\n\n"+strings.Repeat("a", vulnExportBodyLimit)+"\n```") || strings.Contains(body, strings.Repeat("a", vulnExportBodyLimit+1)) {
		t.Fatal("truncated body not 16 KiB")
	}
	if strings.Contains(body, "\x00") {
		t.Fatal("binary body inlined")
	}
	// 同输入字节一致；只读，不登记审计。
	before := len(auditRows(t, f.capability))
	if _, again, _ := f.export("GET", "/api/webapi/vulns/"+c.reproKey+"/card/export?format=markdown", ""); again != body {
		t.Fatal("markdown export not deterministic")
	}
	if after := len(auditRows(t, f.capability)); after != before {
		t.Fatalf("export audited: %d → %d", before, after)
	}

	// 无最小复现：写未提供与原因文案，其余段照常。
	_, legacy, _ := f.export("GET", "/api/webapi/vulns/"+c.legacyKey+"/card/export?format=markdown", "")
	if !strings.Contains(legacy, "\n最小复现：未提供（该判读产生于最小复现功能之前）\n") || !strings.Contains(legacy, "### 验证记录\n\n- ") ||
		strings.Contains(legacy, "### 影响") || !strings.Contains(legacy, "### 最近复测结论\n\n暂无复测\n") {
		t.Fatalf("legacy export =\n%s", legacy)
	}
	_, empty, _ := f.export("GET", "/api/webapi/vulns/"+c.emptyKey+"/card/export?format=markdown", "")
	if !strings.Contains(empty, "最小复现：未提供（还没有独立验证判读）") || !strings.Contains(empty, "### 验证记录\n\n暂无判读\n") || !strings.Contains(empty, "- 等级：未定级\n") {
		t.Fatalf("empty export =\n%s", empty)
	}
	// 人工定级与期限。
	if status, _, raw := f.do("PUT", "/api/webapi/vulns/"+c.reproKey+"/triage", `{"severity":"critical","owner":"值班 A","due_date":"2026-12-01","disposition":"fixing"}`); status != 200 {
		t.Fatalf("triage = %d %s", status, raw)
	}
	_, triaged, _ := f.export("GET", "/api/webapi/vulns/"+c.reproKey+"/card/export?format=markdown", "")
	if !strings.Contains(triaged, "- 等级：严重\n- 状态：新发现\n- 处置：修复中\n- 负责人：值班 A\n- 修复期限：2026-12-01\n") {
		t.Fatalf("triaged export =\n%s", triaged)
	}
	// 错误。
	for path, want := range map[string]int{
		"/api/webapi/vulns/" + c.reproKey + "/card/export":            400,
		"/api/webapi/vulns/" + c.reproKey + "/card/export?format=pdf": 400,
		"/api/webapi/vulns/a%25b/card/export?format=json":             400,
		"/api/webapi/vulns/no-such/card/export?format=json":           404,
	} {
		if status, raw, _ := f.export("GET", path, ""); status != want {
			t.Fatalf("%s = %d %s", path, status, raw)
		}
	}
}

func TestVulnExportSingleJSON(t *testing.T) {
	c := newExportCase(t)
	f := c.f
	status, body, headers := f.export("GET", "/api/webapi/vulns/"+c.reproKey+"/card/export?format=json", "")
	code := f.card(c.reproKey).Header.Code
	if status != 200 || headers["Content-Type"] != "application/json" || headers["Cache-Control"] != "no-store" || headers["Content-Disposition"] != `attachment; filename="vuln-`+code+`.json"` {
		t.Fatalf("json export = %d %+v %s", status, headers, body)
	}
	for _, secret := range []string{"Fixture-Pass-9", "fixture-token", "fixture-session-abcdef"} {
		if strings.Contains(body, secret) {
			t.Fatalf("json leaks %q", secret)
		}
	}
	var report VulnExport
	if err := json.Unmarshal([]byte(body), &report); err != nil {
		t.Fatal(err)
	}
	if report.Schema != vulnExportSchema || len(report.Items) != 1 {
		t.Fatalf("report = %+v", report)
	}
	item := report.Items[0]
	if item.Key != c.reproKey || item.Code != code || item.Asset == nil || item.Asset.Name != "演示库存系统" || item.Location != "POST https://api.example.test/orders/{id}" ||
		item.Severity != "" || item.SuggestedSeverity != "high" || item.SeverityReason != "可批量读取" || item.Status != VulnNew || item.Disposition != DispositionOpen ||
		item.Owner != "测试组" || item.ReproMissingReason != nil || item.Retest == nil || item.Retest.Conclusion != "reproducible" || len(item.Replays) != 0 || len(item.Verifications) != 1 {
		t.Fatalf("item = %+v", item)
	}
	repro := item.Repro
	if repro == nil || repro.VerificationID != c.verificationID || repro.Preconditions != "普通用户已登录（口令 ******）" || len(repro.Steps) != 3 || len(repro.Remediation) != 2 ||
		repro.Contrast == nil || repro.Contrast.Normal.EvidenceID != c.normal.ID || repro.Contrast.Abnormal.URL != "https://api.example.test/orders/1002?expand=1&pw=******" {
		t.Fatalf("repro = %+v", repro)
	}
	s1, s2, s3 := repro.Steps[0], repro.Steps[1], repro.Steps[2]
	if s1.Index != 1 || s1.EvidenceStatus != evidenceStatusOK || !s1.MarkMissing || strings.Join(s1.RequestMarksFound, "|") != "******|pw=******" ||
		strings.Join(s1.ResponseMarksFound, "|") != `"owner":"user-42"` || !strings.Contains(s1.RequestRaw, "Authorization: ******\n") || !strings.Contains(s1.RequestRaw, "Cookie: ******\n") ||
		s1.RequestBodyBytes != len(`{"note":"demo-user / Fixture-Pass-9"}`) || !strings.HasPrefix(s1.Curl, "# 已遮盖") || s1.CaptureTruncated || s1.ResponseBodyTruncated {
		t.Fatalf("step1 = %+v", s1)
	}
	if !s2.CaptureTruncated || !s2.ResponseBodyTruncated || s2.ResponseBodyBytes != 20000 || s2.ResponseBodyBinary ||
		len(s2.ResponseRaw)-strings.Index(s2.ResponseRaw, "\n\n")-2 != vulnExportBodyLimit {
		t.Fatalf("step2 flags = %+v", s2.ResponseBodyBytes)
	}
	if !s3.ResponseBodyBinary || s3.ResponseBodyBytes != 4 || !strings.HasSuffix(s3.ResponseRaw, "Set-Cookie: ******\n") || s3.ResponseBodyTruncated {
		t.Fatalf("step3 = %q", s3.ResponseRaw)
	}
	if _, again, _ := f.export("GET", "/api/webapi/vulns/"+c.reproKey+"/card/export?format=json", ""); again != body {
		t.Fatal("json export not deterministic")
	}
	// 无最小复现：repro null、原因保留，验证记录照常。
	_, legacy, _ := f.export("GET", "/api/webapi/vulns/"+c.legacyKey+"/card/export?format=json", "")
	var legacyReport VulnExport
	if err := json.Unmarshal([]byte(legacy), &legacyReport); err != nil || legacyReport.Items[0].Repro != nil || legacyReport.Items[0].ReproMissingReason == nil ||
		*legacyReport.Items[0].ReproMissingReason != reproMissingNoSteps || len(legacyReport.Items[0].Verifications) != 1 {
		t.Fatalf("legacy json = %s", legacy)
	}
	// 证据行缺失：步骤标记 missing，不给原文。
	if _, err := f.capability.store.db().ExecContext(t.Context(), `DELETE FROM webapi_requests WHERE id = ?`, c.steps[2].ID); err != nil {
		t.Fatal(err)
	}
	_, missing, _ := f.export("GET", "/api/webapi/vulns/"+c.reproKey+"/card/export?format=markdown", "")
	if !strings.Contains(missing, "#### 第 3 步：下载订单 PDF\n\n证据原件不存在（"+c.steps[2].ID+"）。\n") {
		t.Fatalf("missing evidence export =\n%s", missing)
	}
}

func TestVulnExportMulti(t *testing.T) {
	c := newExportCase(t)
	f := c.f
	keys := []string{c.legacyKey, c.reproKey, c.emptyKey}
	raw, _ := json.Marshal(map[string]any{"keys": keys, "format": "markdown"})
	status, body, headers := f.export("POST", "/api/webapi/vulns/export", string(raw))
	if status != 200 || headers["Content-Disposition"] != `attachment; filename="vulns-report.md"` || headers["Cache-Control"] != "no-store" {
		t.Fatalf("multi = %d %+v %s", status, headers, body)
	}
	codes := []string{}
	for _, key := range keys {
		codes = append(codes, f.card(key).Header.Code)
	}
	if !strings.HasPrefix(body, "> "+vulnExportDeclaration+"\n\n## 汇总（3 条）\n\n| 编号 | 标题 | 资产 | 等级 | 状态 | 处置 | 期限 |\n|---|---|---|---|---|---|---|\n| "+codes[0]+" | 调试接口暴露 | 演示库存系统 | ") {
		t.Fatalf("multi head =\n%s", body[:min(len(body), 600)])
	}
	if !strings.Contains(body, "| "+codes[1]+" | 订单接口越权读取 | 演示库存系统 | 未定级（AI 建议：高危） | 新发现 | 待处理 | ") {
		t.Fatalf("summary row missing:\n%s", body[:min(len(body), 800)])
	}
	last := -1
	for _, code := range codes {
		at := strings.Index(body, "\n## "+code+" ")
		if at <= last {
			t.Fatalf("section order: %s at %d after %d", code, at, last)
		}
		last = at
	}
	if _, again, _ := f.export("POST", "/api/webapi/vulns/export", string(raw)); again != body {
		t.Fatal("multi export not deterministic")
	}
	// JSON：顺序同请求。
	raw, _ = json.Marshal(map[string]any{"keys": []string{c.reproKey, c.legacyKey}, "format": "json"})
	status, body, headers = f.export("POST", "/api/webapi/vulns/export", string(raw))
	var report VulnExport
	if status != 200 || headers["Content-Disposition"] != `attachment; filename="vulns-report.json"` || json.Unmarshal([]byte(body), &report) != nil ||
		len(report.Items) != 2 || report.Items[0].Key != c.reproKey || report.Items[1].Key != c.legacyKey {
		t.Fatalf("multi json = %d %s", status, body)
	}
	// 归档资产的条目仍可导出。
	archived := strings.TrimSuffix(assetBody, "}") + `,"archived":true}`
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1", archived); status != 200 {
		t.Fatalf("archive = %d %s", status, raw)
	}
	raw, _ = json.Marshal(map[string]any{"keys": []string{c.reproKey}, "format": "markdown"})
	if status, body, _ := f.export("POST", "/api/webapi/vulns/export", string(raw)); status != 200 || !strings.Contains(body, "## "+codes[1]+" ") {
		t.Fatalf("archived export = %d %s", status, body)
	}
	// 错误。
	tooMany := []string{}
	for index := range vulnExportMaxKeys + 1 {
		tooMany = append(tooMany, fmt.Sprintf("k%d", index))
	}
	for _, bad := range []any{
		map[string]any{"keys": []string{}, "format": "markdown"},
		map[string]any{"format": "markdown"},
		map[string]any{"keys": tooMany, "format": "markdown"},
		map[string]any{"keys": []string{c.reproKey, c.reproKey}, "format": "markdown"},
		map[string]any{"keys": []string{c.reproKey}, "format": "pdf"},
		map[string]any{"keys": []string{c.reproKey}},
		map[string]any{"keys": []string{c.reproKey}, "format": "json", "mask": 0},
		map[string]any{"keys": []string{"a/b"}, "format": "json"},
	} {
		raw, _ := json.Marshal(bad)
		if status, body, _ := f.export("POST", "/api/webapi/vulns/export", string(raw)); status != 400 {
			t.Fatalf("bad %s = %d %s", raw, status, body)
		}
	}
	if status, body, _ := f.export("POST", "/api/webapi/vulns/export", ""); status != 400 {
		t.Fatalf("empty body = %d %s", status, body)
	}
	raw, _ = json.Marshal(map[string]any{"keys": []string{c.reproKey, "finding:no-such"}, "format": "json"})
	if status, body, _ := f.export("POST", "/api/webapi/vulns/export", string(raw)); status != 404 || !strings.Contains(body, "finding:no-such") {
		t.Fatalf("missing key = %d %s", status, body)
	}
}

func TestVulnExportFenceAndMarks(t *testing.T) {
	if got := codeFence("no ticks"); got != "```" {
		t.Fatalf("fence = %q", got)
	}
	if got := codeFence("a ``` b `````` c"); got != "```````" {
		t.Fatalf("fence = %q", got)
	}
	marks := exportMarks([]string{"fixture-token", "Bearer fixture-token extra", "ok-mark", "x Secret-12 y"}, []string{"Bearer fixture-token"}, exportMasker{secrets: []string{"Secret-12"}})
	if strings.Join(marks, "|") != "******|******|ok-mark|x ****** y" {
		t.Fatalf("marks = %q", marks)
	}
}
