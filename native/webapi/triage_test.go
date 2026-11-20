package webapi

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

// 漏洞处置（V1）测试：全部为临时库合成数据（example.test 演示资产、虚构候选/负责人/备注）。
// globalFixture：进程时区 UTC+8，时钟固定 2026-11-19T04:00Z（本地 11-19 12:00）。

var (
	ordersKey = assetVulnID("A1", "GET https://shop.example.test/api/orders/{id}") // seed：new，首次存在 11-17（本地）
	debugKey  = assetVulnID("A1", "GET https://shop.example.test/debug")           // pending
	loginKey  = assetVulnID("A1", "POST https://shop.example.test/api/login")      // fixed
)

func (f *globalFixture) triage(key, body string) (int, TriageView, string) {
	f.t.Helper()
	status, raw := f.do("PUT", "/api/webapi/vulns/"+key+"/triage", body)
	var view TriageView
	_ = json.Unmarshal([]byte(raw), &view)
	return status, view, raw
}

func (f *globalFixture) mustTriage(key, body string) TriageView {
	f.t.Helper()
	status, view, raw := f.triage(key, body)
	if status != 200 {
		f.t.Fatalf("PUT triage %s %s = %d %s", key, body, status, raw)
	}
	return view
}

type globalVulnList struct {
	Items  []GlobalVuln   `json:"items"`
	Counts map[string]any `json:"counts"`
}

func (f *globalFixture) globalVulnList(query string) globalVulnList {
	f.t.Helper()
	var out globalVulnList
	f.getJSON("/api/webapi/vulns"+query, &out)
	return out
}

func globalItem(list globalVulnList, id string) *GlobalVuln {
	for index := range list.Items {
		if list.Items[index].ID == id {
			return &list.Items[index]
		}
	}
	return nil
}

func ids(items []GlobalVuln) string {
	out := []string{}
	for _, item := range items {
		out = append(out, item.ID)
	}
	return strings.Join(out, ",")
}

func due(view TriageView) string {
	if view.DueEffective == nil {
		return "null"
	}
	return *view.DueEffective
}

func newTriageFixture(t *testing.T) *globalFixture {
	f := newGlobalFixture(t)
	f.seed()
	if status, raw := f.do("PUT", "/api/webapi/assets/A1", `{"name":"演示商城","owner":"安全组","origins":["https://shop.example.test"]}`); status != 200 {
		t.Fatalf("asset owner = %d %s", status, raw)
	}
	return f
}

func TestTriagePutValidationNotFoundAndNoChange(t *testing.T) {
	f := newTriageFixture(t)
	for _, body := range []string{
		`{"severity":"urgent"}`, `{"severity":"HIGH"}`, `{"disposition":""}`, `{"disposition":"closed"}`,
		`{"owner":"` + strings.Repeat("名", 101) + `"}`, `{"note":"` + strings.Repeat("备", 2001) + `"}`,
		`{"due_date":"2026-02-30"}`, `{"due_date":"2026-1-01"}`, `{"due_date":"2026/11/20"}`, `{"severity":1}`,
		`{"extra":1}`, `not json`, ``, `{}{}`,
	} {
		if status, _, raw := f.triage(ordersKey, body); status != 400 {
			t.Errorf("body %q = %d %s, want 400", body, status, raw)
		}
	}
	if status, _, _ := f.triage("a%2Fb", `{}`); status != 400 {
		t.Errorf("bad key = %d", status)
	}
	// 不存在、资产候选冒充 finding: 键、缺失候选：404。
	for _, key := range []string{"no-such-vuln", "finding:wf_F1a", "finding%3Awf_missing"} {
		if status, _, raw := f.triage(key, `{"severity":"high"}`); status != 404 {
			t.Errorf("key %s = %d %s, want 404", key, status, raw)
		}
	}

	// 无记录：默认 open、负责人兜底为资产负责人、无期限。
	list := f.globalVulnList("")
	initial := globalItem(list, ordersKey).Triage
	if initial == nil || initial.Disposition != DispositionOpen || initial.Severity != "" || initial.OwnerEffective != "安全组" || initial.Owner != "" ||
		initial.UpdatedAt != nil || initial.DueEffective != nil || initial.Overdue || initial.VulnKey != ordersKey {
		t.Fatalf("initial triage = %+v", initial)
	}
	// 无记录时提交默认值：不建记录。
	if view := f.mustTriage(debugKey, `{"disposition":"open","severity":"","owner":"  "}`); view.UpdatedAt != nil {
		t.Fatalf("default-only put created record: %+v", view)
	}
	requireAudit(t, f.cap, "vuln.triage", 0)

	// 部分更新：严重度 + 负责人（去首尾空白）；SLA 高危 7 天自首次存在（本地 11-17）起算。
	view := f.mustTriage(ordersKey, `{"severity":"high","owner":" 张三 ","note":"演示备注：内部工单 DEMO-1"}`)
	if view.Severity != SeverityHigh || view.Owner != "张三" || view.OwnerEffective != "张三" || view.Disposition != DispositionOpen || due(view) != "2026-11-24" ||
		view.Overdue || view.UpdatedAt == nil || view.Note != "演示备注：内部工单 DEMO-1" || view.DueDate != "" {
		t.Fatalf("after put = %+v", view)
	}
	first := *view.UpdatedAt
	time.Sleep(2 * time.Millisecond)
	// 无变化：不改 updated_at、不登记审计。
	again := f.mustTriage(ordersKey, `{"severity":"high","owner":"张三"}`)
	if again.UpdatedAt == nil || *again.UpdatedAt != first {
		t.Fatalf("no-op changed updated_at: %v -> %v", first, again.UpdatedAt)
	}
	row := requireAudit(t, f.cap, "vuln.triage", 1)
	detail := auditDetail(t, row)
	changes := detail["changes"].(map[string]any)
	if row.TargetID != ordersKey || row.AssetID != "A1" || detail["note_changed"] != true || changes["severity"].(map[string]any)["to"] != "high" ||
		changes["owner"].(map[string]any)["from"] != "" || changes["disposition"] != nil || !strings.Contains(row.Summary, "严重度 未定级 → 高危") {
		t.Fatalf("audit = %s %s", row.Summary, row.DetailJSON)
	}
	assertNoSecrets(t, f.cap, "DEMO-1")
	// 只改其他字段：保留已设字段（部分更新）。
	view = f.mustTriage(ordersKey, `{"disposition":"fixing"}`)
	if view.Severity != SeverityHigh || view.Owner != "张三" || view.Disposition != DispositionFixing || *view.UpdatedAt == first {
		t.Fatalf("partial = %+v", view)
	}
	// 清空负责人 → 回落到资产负责人。
	if view = f.mustTriage(ordersKey, `{"owner":""}`); view.OwnerEffective != "安全组" || view.Owner != "" {
		t.Fatalf("owner fallback = %+v", view)
	}
	requireAudit(t, f.cap, "vuln.triage", 3)

	// /vulns 与 /assets/{id}/vulns 每项带同一 triage。
	if item := globalItem(f.globalVulnList(""), ordersKey); item.Triage.Disposition != DispositionFixing || item.Triage.Severity != SeverityHigh {
		t.Fatalf("global triage = %+v", item.Triage)
	}
	var assetList struct {
		Vulns []AssetVuln `json:"vulns"`
	}
	f.getJSON("/api/webapi/assets/A1/vulns", &assetList)
	for _, item := range assetList.Vulns {
		if item.Triage == nil || item.Triage.VulnKey != item.ID {
			t.Fatalf("asset vuln triage = %+v", item)
		}
		if item.ID == ordersKey && (item.Triage.Severity != SeverityHigh || due(*item.Triage) != "2026-11-24") {
			t.Fatalf("asset orders triage = %+v", item.Triage)
		}
	}
	var single AssetVuln
	f.getJSON("/api/webapi/assets/A1/vulns/"+ordersKey, &single)
	if single.Triage == nil || single.Triage.Disposition != DispositionFixing {
		t.Fatalf("single triage = %+v", single.Triage)
	}

	// 无资产候选：finding:<id>，原样与 URL 编码都可；无资产负责人。
	view = f.mustTriage("finding:wf_F3", `{"severity":"critical"}`)
	if view.VulnKey != "finding:wf_F3" || due(view) != "2026-11-22" || view.OwnerEffective != "" {
		t.Fatalf("standalone = %+v", view)
	}
	if view = f.mustTriage("finding%3Awf_F3", `{"owner":"李四"}`); view.Severity != SeverityCritical || view.Owner != "李四" {
		t.Fatalf("encoded standalone = %+v", view)
	}
	var stored vulnTriageModel
	if err := f.cap.store.orm.NewSelect().Model(&stored).Where("vuln_key = ?", "finding:wf_F3").Scan(t.Context()); err != nil || stored.AssetID != "" {
		t.Fatalf("stored standalone = %+v %v", stored, err)
	}

	// 归档资产的条目（经资产漏洞库同步后）也可设置。
	f.getJSON("/api/webapi/assets/A2/vulns", &assetList)
	if status, _, raw := f.triage(assetVulnID("A2", "GET https://old.example.test/search"), `{"severity":"low"}`); status != 200 {
		t.Fatalf("archived asset vuln = %d %s", status, raw)
	}
}

func TestTriageDueAndOverdueBoundaries(t *testing.T) {
	f := newTriageFixture(t)
	// 人工期限覆盖：昨天 → 逾期；今天 → 不逾期（今天 = 期限）。
	view := f.mustTriage(ordersKey, `{"severity":"high","due_date":"2026-11-18"}`)
	if due(view) != "2026-11-18" || !view.Overdue {
		t.Fatalf("yesterday = %+v", view)
	}
	if view = f.mustTriage(ordersKey, `{"due_date":"2026-11-19"}`); due(view) != "2026-11-19" || view.Overdue {
		t.Fatalf("today = %+v", view)
	}
	// 处置 risk_accepted / false_positive 不算逾期；fixing 算。
	f.mustTriage(ordersKey, `{"due_date":"2026-11-10"}`)
	for _, item := range []struct {
		disposition string
		overdue     bool
	}{{DispositionRiskAccepted, false}, {DispositionFalsePositive, false}, {DispositionFixing, true}, {DispositionOpen, true}} {
		if view = f.mustTriage(ordersKey, `{"disposition":"`+item.disposition+`"}`); view.Overdue != item.overdue {
			t.Errorf("disposition %s overdue = %v", item.disposition, view.Overdue)
		}
	}
	// 清除人工期限 → 回到 SLA；info 与未定级无期限。
	if view = f.mustTriage(ordersKey, `{"due_date":""}`); due(view) != "2026-11-24" {
		t.Fatalf("sla due = %+v", view)
	}
	for _, severity := range []string{"info", ""} {
		if view = f.mustTriage(ordersKey, `{"severity":"`+severity+`"}`); view.DueEffective != nil || view.Overdue {
			t.Fatalf("severity %q = %+v", severity, view)
		}
	}
	// 未定级 + 人工期限：照样有期限与逾期。
	if view = f.mustTriage(ordersKey, `{"due_date":"2026-11-01"}`); due(view) != "2026-11-01" || !view.Overdue {
		t.Fatalf("manual due without severity = %+v", view)
	}
	// 已修复条目：有期限但不逾期；无“存在”事件（pending）的条目：SLA 无期限。
	if view = f.mustTriage(loginKey, `{"severity":"critical","due_date":"2026-11-01"}`); !strings.HasPrefix(due(view), "2026-11-01") || view.Overdue {
		t.Fatalf("fixed = %+v", view)
	}
	if view = f.mustTriage(debugKey, `{"severity":"critical"}`); view.DueEffective != nil {
		t.Fatalf("pending sla = %+v", view)
	}
	// 首次存在按 Hub 本地日：UTC 11-18 16:30 = 本地 11-19，低危 90 天 → 2027-02-17（按 UTC 会是 02-16）。
	f.finding("F5", "T3", "本地日边界", "GET", "https://api.example.test/local", "2026-11-18T16:00:00Z")
	f.verify("v5", "T3", "F5", "s-v5", "supported", "2026-11-18T16:30:00Z")
	if view = f.mustTriage("finding:wf_F5", `{"severity":"low"}`); due(view) != "2027-02-17" {
		t.Fatalf("local day = %+v", view)
	}
}

func TestVulnSLAGetPutAndDueEffective(t *testing.T) {
	f := newTriageFixture(t)
	var sla VulnSLA
	f.getJSON("/api/webapi/vuln-sla", &sla)
	if sla.CriticalDays != 3 || sla.HighDays != 7 || sla.MediumDays != 30 || sla.LowDays != 90 || sla.UpdatedAt != nil {
		t.Fatalf("default sla = %+v", sla)
	}
	for _, body := range []string{`{"critical_days":0}`, `{"high_days":366}`, `{"low_days":1.5}`, `{"low_days":"3"}`, `{"days":3}`, `not json`, ``} {
		if status, raw := f.do("PUT", "/api/webapi/vuln-sla", body); status != 400 {
			t.Errorf("sla %q = %d %s", body, status, raw)
		}
	}
	f.mustTriage(ordersKey, `{"severity":"high"}`)
	status, raw := f.do("PUT", "/api/webapi/vuln-sla", `{"high_days":1,"critical_days":2}`)
	if status != 200 || json.Unmarshal([]byte(raw), &sla) != nil || sla.HighDays != 1 || sla.CriticalDays != 2 || sla.MediumDays != 30 || sla.UpdatedAt == nil {
		t.Fatalf("put sla = %d %s", status, raw)
	}
	updated := *sla.UpdatedAt
	// 修改后读时生效：首次存在 11-17 + 1 = 11-18 < 今天 11-19 → 逾期。
	if item := globalItem(f.globalVulnList(""), ordersKey); due(*item.Triage) != "2026-11-18" || !item.Triage.Overdue {
		t.Fatalf("after sla = %+v", item.Triage)
	}
	// 期限恰为今天不逾期。
	f.do("PUT", "/api/webapi/vuln-sla", `{"high_days":2}`)
	if item := globalItem(f.globalVulnList(""), ordersKey); due(*item.Triage) != "2026-11-19" || item.Triage.Overdue {
		t.Fatalf("boundary sla = %+v", item.Triage)
	}
	row := requireAudit(t, f.cap, "vuln.sla", 2)
	detail := auditDetail(t, row)
	if detail["from"].(map[string]any)["high_days"] != float64(1) || detail["to"].(map[string]any)["high_days"] != float64(2) || !strings.Contains(row.Summary, "高危 1 → 2 天") {
		t.Fatalf("sla audit = %s %s", row.Summary, row.DetailJSON)
	}
	// 无变化：不改 updated_at、不登记。
	var stored VulnSLA
	f.getJSON("/api/webapi/vuln-sla", &stored)
	before := *stored.UpdatedAt
	time.Sleep(2 * time.Millisecond)
	status, raw = f.do("PUT", "/api/webapi/vuln-sla", `{"high_days":2,"critical_days":2}`)
	if status != 200 || json.Unmarshal([]byte(raw), &sla) != nil || sla.UpdatedAt == nil || *sla.UpdatedAt != before || before == updated {
		t.Fatalf("noop sla = %d %s (before %s, first %s)", status, raw, before, updated)
	}
	f.getJSON("/api/webapi/vuln-sla", &stored)
	if *stored.UpdatedAt != before {
		t.Fatalf("noop changed updated_at %s -> %s", before, *stored.UpdatedAt)
	}
	requireAudit(t, f.cap, "vuln.sla", 2)
}

func TestTriageGlobalFiltersCountsAndOverview(t *testing.T) {
	f := newTriageFixture(t)
	// seed 全局条目：orders new、debug pending、login fixed（A1）；finding:wf_F3 new、finding:wf_F4 unconfirmed。
	f.mustTriage(ordersKey, `{"severity":"high"}`)
	f.mustTriage("finding:wf_F3", `{"severity":"critical","disposition":"fixing"}`)
	f.mustTriage(debugKey, `{"severity":"medium","disposition":"false_positive"}`)
	f.mustTriage(loginKey, `{"severity":"low"}`)
	f.do("PUT", "/api/webapi/vuln-sla", `{"high_days":1}`) // orders 期限 11-18 → 逾期

	all := f.globalVulnList("")
	if got := ids(all.Items); got != ordersKey+",finding:wf_F3,"+debugKey+",finding:wf_F4,"+loginKey &&
		got != "finding:wf_F3,"+ordersKey+","+debugKey+",finding:wf_F4,"+loginKey {
		t.Fatalf("default order changed: %s", got)
	}
	bySeverity := all.Counts["by_severity"].(map[string]any)
	if all.Counts["overdue"] != float64(1) || bySeverity["high"] != float64(1) || bySeverity["critical"] != float64(1) || bySeverity["medium"] != float64(0) ||
		bySeverity["low"] != float64(0) || bySeverity["none"] != float64(0) || bySeverity["info"] != float64(0) || all.Counts["new"] != float64(2) || all.Counts["fixed"] != float64(1) {
		t.Fatalf("counts = %+v", all.Counts)
	}
	for query, want := range map[string]string{
		"?severity=high,critical":                  ordersKey + ",finding:wf_F3",
		"?severity=none":                           "finding:wf_F4",
		"?severity=%20low%20":                      loginKey,
		"?disposition=false_positive":              debugKey,
		"?disposition=fixing,false_positive":       "finding:wf_F3," + debugKey,
		"?overdue=1":                               ordersKey,
		"?overdue=0&status=fixed":                  loginKey,
		"?severity=high,critical&disposition=open": ordersKey,
		"?severity=critical&overdue=1":             "",
	} {
		list := f.globalVulnList(query)
		got := []string{}
		for _, item := range list.Items {
			got = append(got, item.ID)
		}
		// 两个 new 条目同状态内按事件时间排序；这里只比较集合。
		if !sameSet(got, want) {
			t.Errorf("%s = %v, want %s", query, got, want)
		}
		// counts 不受 status/severity/disposition/overdue 影响。
		if list.Counts["overdue"] != float64(1) || list.Counts["new"] != float64(2) {
			t.Errorf("%s counts = %+v", query, list.Counts)
		}
	}
	// q 影响 counts。
	if list := f.globalVulnList("?q=订单"); list.Counts["overdue"] != float64(1) || list.Counts["by_severity"].(map[string]any)["critical"] != float64(0) {
		t.Fatalf("q counts = %+v", list.Counts)
	}
	for _, query := range []string{"?severity=urgent", "?severity=high,", "?disposition=closed", "?overdue=2", "?overdue=true"} {
		if status, raw := f.do("GET", "/api/webapi/vulns"+query, ""); status != 400 {
			t.Errorf("%s = %d %s", query, status, raw)
		}
	}
	var overview Overview
	f.getJSON("/api/webapi/overview", &overview)
	if overview.Vulns.Overdue != 1 || overview.Vulns.OpenBySeverity["high"] != 1 || overview.Vulns.OpenBySeverity["critical"] != 1 ||
		overview.Vulns.OpenBySeverity["medium"] != 0 || overview.Vulns.OpenBySeverity["none"] != 0 || len(overview.Vulns.OpenBySeverity) != 6 || overview.Vulns.Counts["new"] != 2 {
		t.Fatalf("overview vulns = %+v", overview.Vulns)
	}
}

func sameSet(got []string, want string) bool {
	expected := map[string]bool{}
	if want != "" {
		for _, id := range strings.Split(want, ",") {
			expected[id] = true
		}
	}
	if len(got) != len(expected) {
		return false
	}
	for _, id := range got {
		if !expected[id] {
			return false
		}
	}
	return true
}

func triageKeys(t *testing.T, f *globalFixture) map[string]vulnTriageModel {
	t.Helper()
	records, err := f.cap.store.triageRecords(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func TestTriageMergeInheritAndSplit(t *testing.T) {
	f := newTriageFixture(t)
	merge := func(into, from string) {
		t.Helper()
		if status, raw := f.do("POST", "/api/webapi/assets/A1/vulns/"+into+"/merge", `{"from":"`+from+`"}`); status != 200 {
			t.Fatalf("merge %s <- %s = %d %s", into, from, status, raw)
		}
	}
	f.mustTriage(debugKey, `{"severity":"medium","owner":"王五","note":"合并前备注"}`)
	// 目标无记录：继承 from 的记录；from 记录删除。
	merge(loginKey, debugKey)
	records := triageKeys(t, f)
	if _, ok := records[debugKey]; ok || records[loginKey].Severity != SeverityMedium || records[loginKey].Owner != "王五" || records[loginKey].Note != "合并前备注" || records[loginKey].AssetID != "A1" {
		t.Fatalf("inherit = %+v", records)
	}
	// 目标有记录：保留目标的；from 记录删除。
	f.mustTriage(ordersKey, `{"severity":"high"}`)
	merge(ordersKey, loginKey)
	records = triageKeys(t, f)
	if _, ok := records[loginKey]; ok || records[ordersKey].Severity != SeverityHigh || records[ordersKey].Owner != "" {
		t.Fatalf("keep target = %+v", records)
	}
	// 拆分出的新人工条目无记录；人工条目移空删除时其处置记录一并删除。
	if status, raw := f.do("PUT", "/api/webapi/assets/A1/vulns/manual-split/members/wf_F1c", ""); status != 201 {
		t.Fatalf("split = %d %s", status, raw)
	}
	var split AssetVuln
	f.getJSON("/api/webapi/assets/A1/vulns/manual-split", &split)
	if split.Triage == nil || split.Triage.UpdatedAt != nil || split.Triage.Disposition != DispositionOpen {
		t.Fatalf("split triage = %+v", split.Triage)
	}
	f.mustTriage("manual-split", `{"severity":"low"}`)
	if status, raw := f.do("PUT", "/api/webapi/assets/A1/vulns/"+ordersKey+"/members/wf_F1c", ""); status != 200 {
		t.Fatalf("move back = %d %s", status, raw)
	}
	if _, ok := triageKeys(t, f)["manual-split"]; ok {
		t.Fatal("emptied manual vuln triage kept")
	}
	if status, _, _ := f.triage("manual-split", `{"severity":"low"}`); status != 404 {
		t.Fatalf("emptied manual vuln triage put = %d", status)
	}
	if status, _, _ := f.triage(debugKey, `{"severity":"low"}`); status != 404 {
		t.Fatalf("emptied auto vuln triage put = %d", status)
	}
}

func TestTriageNotifyFalsePositiveRiskAcceptedAndSeverity(t *testing.T) {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	f.launchManual("a1", "t1")
	f.enable(false, "")
	flow := notifyFlow("GET", "https://api.example.test/orders/1002", "", `{"order":"1002"}`)
	f1, f1rev := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))
	f.vulns("a1", "") // 同步出条目
	vulnID := assetVulnID("a1", "GET https://api.example.test/orders/{id}")
	put := func(key, body string) {
		t.Helper()
		if status, _, raw := f.do("PUT", "/api/webapi/vulns/"+key+"/triage", body); status != 200 {
			t.Fatalf("triage %s = %d %s", key, status, raw)
		}
	}
	markdown := func() string {
		t.Helper()
		f.wecom.reset()
		f.deliver()
		got := f.wecom.received()
		if len(got) == 0 || got[0]["msgtype"] != "markdown" {
			t.Fatalf("sent = %+v", got)
		}
		return got[0]["markdown"].(map[string]any)["content"].(string)
	}

	// 误报：不登记。
	put(vulnID, `{"severity":"high","disposition":"false_positive"}`)
	f.verify("t1", f1, f1rev, "supported", flow)
	if items := f.notifications(); len(items) != 0 {
		t.Fatalf("false_positive registered %+v", items)
	}
	// 接受风险：照常登记；消息标题带等级。
	put(vulnID, `{"disposition":"risk_accepted"}`)
	f.verify("t1", f1, f1rev, "supported", flow)
	if items := f.notifications(); len(items) != 1 || items[0].Kind != NotifyKindNew {
		t.Fatalf("risk_accepted new = %+v", items)
	}
	if content := markdown(); !strings.HasPrefix(content, "**【新确认漏洞】【高危】订单接口越权读取**\n") {
		t.Fatalf("markdown = %s", content)
	}
	// 接受风险下复测已修复后再 supported：照常推 regression。
	f.addRetest("t1", f1, f1rev, "fixed_in_scope")
	time.Sleep(2 * time.Millisecond)
	f.verify("t1", f1, f1rev, "supported", flow)
	if items := f.notifications(); len(items) != 2 || items[1].Kind != NotifyKindRegression {
		t.Fatalf("risk_accepted regression = %+v", items)
	}
	put(vulnID, `{"severity":"critical"}`) // 发送时取当前等级
	if content := markdown(); !strings.HasPrefix(content, "**【漏洞复现】【严重】订单接口越权读取**\n") {
		t.Fatalf("regression markdown = %s", content)
	}
	// 改回误报：之后的 regression 也不登记。
	f.addRetest("t1", f1, f1rev, "fixed_in_scope")
	time.Sleep(2 * time.Millisecond)
	put(vulnID, `{"disposition":"false_positive"}`)
	f.verify("t1", f1, f1rev, "supported", flow)
	if items := f.notifications(); len(items) != 2 {
		t.Fatalf("false_positive regression registered %+v", items)
	}

	// 无资产候选：finding:<id> 的处置同样生效；未定级不加等级。
	f.plainTask("plain-1")
	p1, p1rev := f.addFinding("plain-1", "无资产误报", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))
	put("finding:"+p1, `{"disposition":"false_positive"}`)
	f.verify("plain-1", p1, p1rev, "supported", flow)
	p2, p2rev := f.addFinding("plain-1", "无资产候选", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/2002"))
	f.verify("plain-1", p2, p2rev, "supported", flow)
	items := f.notifications()
	if len(items) != 3 || items[2].DedupeKey != "finding:"+p2 {
		t.Fatalf("standalone = %+v", items)
	}
	if content := markdown(); !strings.HasPrefix(content, "**【新确认漏洞】无资产候选**\n") {
		t.Fatalf("no severity markdown = %s", content)
	}
}

func TestTriageScheduleRetestSkipsFalsePositive(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-18T09:30:00Z")
	f.createAsset("a1")
	f.putSchedule("a1", `{"cron":"0 * * * *","auto_verify":false}`)
	f.setNow("2026-11-18T10:00:10Z")
	f.tick()
	t1 := f.runFor("a1", "2026-11-18T10:00:00Z").TaskID
	a, arev := f.addFinding(t1, "误报 A", simpleContent(), evidenceAt("GET", "https://api.example.test/orders/1"))
	b, brev := f.addFinding(t1, "接受风险 B", simpleContent(), evidenceAt("GET", "https://api.example.test/profile/7"))
	c, crev := f.addFinding(t1, "修复中 C", simpleContent(), evidenceAt("GET", "https://api.example.test/debug"))
	f.addVerification(t1, a, arev, "supported")
	f.addVerification(t1, b, brev, "supported")
	f.addVerification(t1, c, crev, "supported")
	f.endResearch(t1)
	f.tick()
	list := f.vulns("a1", "")
	for finding, body := range map[string]string{a: `{"disposition":"false_positive"}`, b: `{"disposition":"risk_accepted"}`, c: `{"disposition":"fixing"}`} {
		if status, _, raw := f.do("PUT", "/api/webapi/vulns/"+vulnOf(list, finding).ID+"/triage", body); status != 200 {
			t.Fatalf("triage = %d %s", status, raw)
		}
	}
	f.setNow("2026-11-18T11:00:10Z")
	f.tick()
	run2 := f.runFor("a1", "2026-11-18T11:00:00Z")
	f.endResearch(run2.TaskID)
	f.tick()
	items := f.followups(run2.ID)
	got := map[string]bool{}
	for _, item := range items {
		if item.Kind != FollowupRetest {
			t.Fatalf("unexpected followup %+v", item)
		}
		got[item.FindingID] = true
	}
	if len(items) != 2 || got[a] || !got[b] || !got[c] {
		t.Fatalf("retests = %+v (a=%s b=%s c=%s)", items, a, b, c)
	}
}

// B3（二十六表）与 D2（二十五表）旧库补处置与 SLA 两表；补后完整原生校验通过且可写可查。
func TestTriageSchemaUpgradeFromKnownStages(t *testing.T) {
	for _, stage := range []struct{ models, indexes int }{{26, 29}, {25, 25}} {
		database := filepath.Join(t.TempDir(), "hub.sqlite")
		old, err := webservice.NewSQLiteStore(database, webapiSchemaStage(stage.models, stage.indexes))
		if err != nil {
			t.Fatal(err)
		}
		_ = old.Close()
		if _, err := webservice.NewSQLiteStore(database, SchemaModule()); err == nil {
			t.Fatalf("stage %v already complete", stage)
		}
		if err := PrepareWebAPISchema(database); err != nil {
			t.Fatalf("prepare stage %v: %v", stage, err)
		}
		complete, err := webservice.NewSQLiteStore(database, SchemaModule())
		if err != nil {
			t.Fatalf("full validation after stage %v: %v", stage, err)
		}
		_ = complete.Close()
		f := openAssetFixture(t, database)
		if status, value, raw := f.do("PUT", "/api/webapi/vuln-sla", `{"low_days":60}`); status != 200 || value["low_days"] != float64(60) {
			t.Fatalf("sla after upgrade = %d %s", status, raw)
		}
		if status, value, _ := f.do("GET", "/api/webapi/vulns", ""); status != 200 || value["counts"].(map[string]any)["overdue"] != float64(0) {
			t.Fatalf("vulns after upgrade = %d %v", status, value)
		}
		if err := PrepareWebAPISchema(database); err != nil {
			t.Fatalf("prepare complete again: %v", err)
		}
	}
}
