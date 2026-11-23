package webapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/uptrace/bun"
)

// 资产漏洞库（C3）测试：全部为合成数据（example.test 演示资产、虚构候选与证据）。

func evidenceAt(method, url string) []FindingEvidenceSnapshot {
	return []FindingEvidenceSnapshot{{Relation: "supporting", Method: method, URL: url}}
}

func (f *scheduleFixture) launchManual(assetID, taskID string) {
	f.t.Helper()
	if status, _, raw := f.do("PUT", "/api/webapi/assets/"+assetID+"/tasks/"+taskID, ""); status != 201 {
		f.t.Fatalf("launch %s = %d %s", taskID, status, raw)
	}
	time.Sleep(2 * time.Millisecond) // 关联时间严格递增
}

type vulnList struct {
	AssetID      string         `json:"asset_id"`
	LatestTaskID *string        `json:"latest_task_id"`
	Counts       map[string]int `json:"counts"`
	Vulns        []AssetVuln    `json:"vulns"`
}

func (f *scheduleFixture) vulns(assetID, query string) vulnList {
	f.t.Helper()
	status, _, raw := f.do("GET", "/api/webapi/assets/"+assetID+"/vulns"+query, "")
	if status != 200 {
		f.t.Fatalf("vulns = %d %s", status, raw)
	}
	var out vulnList
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func vulnOf(list vulnList, findingID string) *AssetVuln {
	for index := range list.Vulns {
		for _, member := range list.Vulns[index].Members {
			if member.FindingID == findingID {
				return &list.Vulns[index]
			}
		}
	}
	return nil
}

func (f *scheduleFixture) vulnDo(method, path, body string) (int, AssetVuln, string) {
	f.t.Helper()
	status, _, raw := f.do(method, path, body)
	var view AssetVuln
	_ = json.Unmarshal([]byte(raw), &view)
	return status, view, raw
}

func TestWebAPIVulnEndpointKey(t *testing.T) {
	cases := []struct {
		evidence []FindingEvidenceSnapshot
		want     string
	}{
		{evidenceAt("get", "HTTPS://API.Example.test:443/orders/1002?expand=1#x"), "GET https://api.example.test/orders/{id}"},
		{evidenceAt("GET", "https://api.example.test/users/3f2504e0-4f89-11d3-9a0c-0305e82c3301/profile/"), "GET https://api.example.test/users/{id}/profile"},
		{evidenceAt("PUT", "http://example.test:8080/files/0123456789abcdef0123/meta"), "PUT http://example.test:8080/files/{id}/meta"},
		{evidenceAt("GET", "https://api.example.test/files/abcdef12/meta"), "GET https://api.example.test/files/abcdef12/meta"}, // 短 hex 不替换
		{evidenceAt("GET", "https://api.example.test/"), "GET https://api.example.test/"},
		{evidenceAt("GET", "https://api.example.test"), "GET https://api.example.test/"},
		{evidenceAt("GET", "https://api.example.test/v2/items///"), "GET https://api.example.test/v2/items"},
		// 第一条 supporting 优先；无 supporting 取第一条任意关系。
		{[]FindingEvidenceSnapshot{{Relation: "limitation", Method: "GET", URL: "https://api.example.test/a"}, {Relation: "supporting", Method: "POST", URL: "https://api.example.test/b/7"}}, "POST https://api.example.test/b/{id}"},
		{[]FindingEvidenceSnapshot{{Relation: "limitation", Method: "GET", URL: "https://api.example.test/a/9"}, {Relation: "counterevidence", Method: "POST", URL: "https://api.example.test/b"}}, "GET https://api.example.test/a/{id}"},
		{nil, "finding:wf_1"},
		{evidenceAt("GET", "not a url"), "finding:wf_1"},
	}
	for _, c := range cases {
		if got := vulnEndpointKey("wf_1", c.evidence); got != c.want {
			t.Errorf("key(%+v) = %q, want %q", c.evidence, got, c.want)
		}
	}
	if assetVulnID("a1", "GET https://api.example.test/orders/{id}") != assetVulnID("a1", "GET https://api.example.test/orders/{id}") ||
		assetVulnID("a1", "k") == assetVulnID("a2", "k") || assetVulnID("a1", "k") == scheduleUUID("a1", "k") {
		t.Fatal("assetVulnID not deterministic/namespaced")
	}
}

func TestWebAPIVulnSyncAndStatus(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-18T09:30:00Z")
	f.createAsset("a1")
	f.launchManual("a1", "t1")
	f1, f1rev := f.addFinding("t1", "订单接口越权读取", simpleContent(), evidenceAt("GET", "https://api.example.test/orders/1002?x=1"))
	f2, _ := f.addFinding("t1", "订单创建缺少校验", simpleContent(), evidenceAt("POST", "https://api.example.test/orders"))
	f3, f3rev := f.addFinding("t1", "无证据候选", simpleContent(), nil)

	list := f.vulns("a1", "")
	if len(list.Vulns) != 3 || list.LatestTaskID == nil || *list.LatestTaskID != "t1" || list.Counts["pending"] != 3 {
		t.Fatalf("initial list = %+v", list)
	}
	v1 := vulnOf(list, f1)
	if v1.ID != assetVulnID("a1", "GET https://api.example.test/orders/{id}") || v1.EndpointKey != "GET https://api.example.test/orders/{id}" || v1.Members[0].Link != "auto" ||
		v1.Title != "订单接口越权读取" || v1.LastEvent != nil || v1.Members[0].Number == "" || v1.Members[0].InitialSessionID != f.sessionOf("t1") {
		t.Fatalf("v1 = %+v", v1)
	}
	if v3 := vulnOf(list, f3); v3.EndpointKey != "finding:"+f3 {
		t.Fatalf("v3 = %+v", v3)
	}
	if vulnOf(list, f2).ID == v1.ID {
		t.Fatal("different endpoints merged")
	}

	// 判读：f1 supported → new；f3 只有反证 → unconfirmed；f2 无结果 → pending。
	f.addVerification("t1", f1, f1rev, "supported")
	f.addVerification("t1", f3, f3rev, "counterevidence")
	list = f.vulns("a1", "")
	if vulnOf(list, f1).Status != VulnNew || vulnOf(list, f3).Status != VulnUnconfirmed || vulnOf(list, f2).Status != VulnPending ||
		list.Vulns[0].ID != v1.ID || list.Vulns[2].Status != VulnUnconfirmed || list.Counts["new"] != 1 {
		t.Fatalf("round1 statuses = %+v", list)
	}
	if v := vulnOf(list, f1); v.LastConfirmedAt == nil || v.LastEvent == nil || v.LastEvent.Kind != "verify" || v.LastEvent.Value != "supported" ||
		v.LastEvent.TaskID != "t1" || v.LastEvent.FindingID != f1 || *v.Members[0].LatestVerdict != "supported" || v.Members[0].LatestRetest != nil || v.FixedAt != nil {
		t.Fatalf("v1 after verify = %+v", v)
	}

	// 已入库成员不再重新计算：f1 改修订换了端点也不挪动。
	newRev := newFindingRevisionID()
	payload, _ := json.Marshal(storedFindingPayload{Canonical: "{}", Content: findingContent{Title: "订单接口越权读取（改）"}, Evidence: evidenceAt("GET", "https://api.example.test/other")})
	if err := f.capability.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&findingRevisionModel{ID: newRev, FindingID: f1, TaskID: "t1", Number: 1, Revision: 2, SessionID: "s", TurnID: "t", CallID: "c2-" + f1, PayloadJSON: string(payload), Title: "订单接口越权读取（改）", CreatedAt: nowRFC3339()}).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.capability.store.db().ExecContext(t.Context(), "UPDATE webapi_findings SET current_revision = 2, current_revision_id = ? WHERE id = ?", newRev, f1); err != nil {
		t.Fatal(err)
	}

	// 第二轮：同端点新候选归入同一条目；条目最早成员不在最近任务 → present。
	f.launchManual("a1", "t2")
	f4, _ := f.addFinding("t2", "订单接口越权读取（第二轮）", simpleContent(), evidenceAt("GET", "https://api.example.test/orders/2001/"))
	list = f.vulns("a1", "")
	v := vulnOf(list, f4)
	if v == nil || v.ID != v1.ID || v.MemberCount != 2 || v.TaskCount != 2 || v.Status != VulnPresent || *list.LatestTaskID != "t2" ||
		v.Members[0].FindingID != f4 || v.Title != "订单接口越权读取（第二轮）" || v.Members[1].Title != "订单接口越权读取（改）" || len(list.Vulns) != 3 {
		t.Fatalf("round2 = %+v", list)
	}
	// 复测 fixed_in_scope → fixed；之后的 inconclusive 不是决定性事件。
	f.addRetest("t1", f1, newRev, "fixed_in_scope")
	f.addRetest("t1", f1, newRev, "inconclusive")
	list = f.vulns("a1", "")
	if v := vulnOf(list, f1); v.Status != VulnFixed || v.FixedAt == nil || v.LastEvent.Kind != "retest" || v.LastEvent.Value != "inconclusive" || list.Vulns[len(list.Vulns)-1].ID != v1.ID {
		t.Fatalf("fixed = %+v", list)
	}
	// 第三轮再发现并 supported → present（最早成员在 t1）。
	f.launchManual("a1", "t3")
	f5, f5rev := f.addFinding("t3", "订单接口越权读取（第三轮）", simpleContent(), evidenceAt("GET", "https://api.example.test/orders/3001"))
	f.addVerification("t3", f5, f5rev, "supported")
	list = f.vulns("a1", "")
	if v := vulnOf(list, f5); v.ID != v1.ID || v.Status != VulnPresent || v.FixedAt != nil || v.MemberCount != 3 || v.LastConfirmedAt == nil || v.LastEvent.FindingID != f5 {
		t.Fatalf("rediscovered = %+v", v)
	}
	// 状态过滤与 counts（counts 始终是全量）。
	filtered := f.vulns("a1", "?status=present")
	if len(filtered.Vulns) != 1 || filtered.Vulns[0].ID != v1.ID || filtered.Counts["present"] != 1 || filtered.Counts["pending"] != 1 || filtered.Counts["unconfirmed"] != 1 {
		t.Fatalf("filtered = %+v", filtered)
	}
	if status, _, _ := f.do("GET", "/api/webapi/assets/a1/vulns?status=bogus", ""); status != 400 {
		t.Fatalf("bad status = %d", status)
	}
	if status, _, _ := f.do("GET", "/api/webapi/assets/missing/vulns", ""); status != 404 {
		t.Fatalf("missing asset = %d", status)
	}
	// 同步幂等：重复 GET 不增成员行。
	var members int
	_ = f.capability.store.db().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM webapi_asset_vuln_members").Scan(&members)
	f.vulns("a1", "")
	var again int
	_ = f.capability.store.db().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM webapi_asset_vuln_members").Scan(&again)
	if members != 5 || again != 5 {
		t.Fatalf("members %d → %d", members, again)
	}
	status, view, raw := f.vulnDo("GET", "/api/webapi/assets/a1/vulns/"+v1.ID, "")
	if status != 200 || view.ID != v1.ID || view.MemberCount != 3 {
		t.Fatalf("get vuln = %d %s", status, raw)
	}
	if status, _, _ := f.do("GET", "/api/webapi/assets/a1/vulns/nope", ""); status != 404 {
		t.Fatalf("missing vuln = %d", status)
	}
}

func TestWebAPIVulnManualOps(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-18T09:30:00Z")
	f.createAsset("a1")
	f.createAsset("a2")
	f.launchManual("a1", "t1")
	f.launchManual("a2", "t9")
	f1, _ := f.addFinding("t1", "列表接口越权", simpleContent(), evidenceAt("GET", "https://api.example.test/items/1"))
	f2, _ := f.addFinding("t1", "列表接口越权（另一参数）", simpleContent(), evidenceAt("GET", "https://api.example.test/items/2"))
	f3, _ := f.addFinding("t1", "导出接口信息泄露", simpleContent(), evidenceAt("GET", "https://api.example.test/export"))
	other, _ := f.addFinding("t9", "其他资产候选", simpleContent(), nil)
	list := f.vulns("a1", "")
	auto := vulnOf(list, f1).ID
	exportVuln := vulnOf(list, f3).ID
	if vulnOf(list, f2).ID != auto || len(list.Vulns) != 2 {
		t.Fatalf("initial = %+v", list)
	}
	f.vulns("a2", "")

	// 标题覆盖与清除。
	if status, view, raw := f.vulnDo("PUT", "/api/webapi/assets/a1/vulns/"+auto, `{"title":"  物品接口越权  "}`); status != 200 || view.Title != "物品接口越权" {
		t.Fatalf("title = %d %s", status, raw)
	}
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a1/vulns/"+auto, `{"title":"x","extra":1}`); status != 400 {
		t.Fatalf("unknown field = %d", status)
	}
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a1/vulns/missing", `{"title":"x"}`); status != 404 {
		t.Fatalf("title missing vuln = %d", status)
	}
	if status, view, _ := f.vulnDo("PUT", "/api/webapi/assets/a1/vulns/"+auto, `{"title":""}`); status != 200 || view.Title != "列表接口越权（另一参数）" {
		t.Fatalf("title cleared = %+v", view)
	}

	// 拆分：f2 移入新的人工条目（客户端 id）。
	status, view, raw := f.vulnDo("PUT", "/api/webapi/assets/a1/vulns/manual-1/members/"+f2, "")
	if status != 201 || view.ID != "manual-1" || view.EndpointKey != "" || view.MemberCount != 1 || view.Members[0].Link != "manual" {
		t.Fatalf("split = %d %s", status, raw)
	}
	if status, view, _ := f.vulnDo("PUT", "/api/webapi/assets/a1/vulns/manual-1/members/"+f2, "{}"); status != 200 || view.MemberCount != 1 {
		t.Fatalf("split replay = %d", status)
	}
	// 非本资产候选 409、不存在 404；条目 id 被其他资产占用 409；未知字段 400。
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a1/vulns/manual-1/members/"+other, ""); status != 409 {
		t.Fatalf("other asset finding = %d", status)
	}
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a1/vulns/manual-1/members/wf_missing", ""); status != 404 {
		t.Fatalf("missing finding = %d", status)
	}
	a2Vuln := f.vulns("a2", "").Vulns[0].ID
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a1/vulns/"+a2Vuln+"/members/"+f1, ""); status != 409 {
		t.Fatalf("vid of other asset = %d", status)
	}
	if status, _, _ := f.do("PUT", "/api/webapi/assets/a1/vulns/manual-1/members/"+f1, `{"x":1}`); status != 400 {
		t.Fatalf("member unknown field = %d", status)
	}

	// 同端点新候选仍按端点键落入 auto 条目；人工归并不被同步覆盖。
	f4, _ := f.addFinding("t1", "列表接口越权（第三个）", simpleContent(), evidenceAt("GET", "https://api.example.test/items/3"))
	list = f.vulns("a1", "")
	if vulnOf(list, f4).ID != auto || vulnOf(list, f2).ID != "manual-1" || vulnOf(list, f2).Members[0].Link != "manual" {
		t.Fatalf("after resync = %+v", list)
	}

	// 合并：export 条目并入 manual-1；export（auto）被移空后列表不返回、GET 404。
	if status, _, _ := f.do("POST", "/api/webapi/assets/a1/vulns/manual-1/merge", `{"from":"manual-1"}`); status != 400 {
		t.Fatalf("self merge = %d", status)
	}
	if status, _, _ := f.do("POST", "/api/webapi/assets/a1/vulns/manual-1/merge", `{"from":"nope"}`); status != 404 {
		t.Fatalf("merge missing = %d", status)
	}
	if status, _, _ := f.do("POST", "/api/webapi/assets/a1/vulns/nope/merge", `{"from":"manual-1"}`); status != 404 {
		t.Fatalf("merge into missing = %d", status)
	}
	if status, _, _ := f.do("POST", "/api/webapi/assets/a1/vulns/manual-1/merge", `{"from":"`+exportVuln+`","x":1}`); status != 400 {
		t.Fatalf("merge unknown field = %d", status)
	}
	status, view, raw = f.vulnDo("POST", "/api/webapi/assets/a1/vulns/manual-1/merge", `{"from":"`+exportVuln+`"}`)
	if status != 200 || view.MemberCount != 2 {
		t.Fatalf("merge = %d %s", status, raw)
	}
	for _, member := range view.Members {
		if member.Link != "manual" {
			t.Fatalf("merged link = %+v", member)
		}
	}
	if status, _, _ := f.do("GET", "/api/webapi/assets/a1/vulns/"+exportVuln, ""); status != 404 {
		t.Fatalf("emptied auto vuln = %d", status)
	}
	if exists, _ := findOptional[assetVulnModel](t.Context(), f.capability.store.orm, "id = ?", exportVuln); exists == nil {
		t.Fatal("emptied auto vuln row deleted")
	}
	// 同键新候选仍落入被移空的 auto 条目。
	f5, _ := f.addFinding("t1", "导出接口信息泄露（再次）", simpleContent(), evidenceAt("GET", "https://api.example.test/export?fmt=csv"))
	if v := vulnOf(f.vulns("a1", ""), f5); v == nil || v.ID != exportVuln || v.MemberCount != 1 {
		t.Fatalf("refill auto = %+v", v)
	}

	// 人工条目被移空则删除：把 manual-1 两成员都移回 auto 条目。
	for _, findingID := range []string{f2, f3} {
		if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/vulns/"+auto+"/members/"+findingID, ""); status != 200 {
			t.Fatalf("move back = %d %s", status, raw)
		}
	}
	if exists, _ := findOptional[assetVulnModel](t.Context(), f.capability.store.orm, "id = ?", "manual-1"); exists != nil {
		t.Fatal("empty manual vuln kept")
	}
	if v := vulnOf(f.vulns("a1", ""), f3); v.ID != auto || v.MemberCount != 4 {
		t.Fatalf("auto after move back = %+v", v)
	}
	// 合并后从人工条目移空也删除。
	f.do("PUT", "/api/webapi/assets/a1/vulns/manual-2/members/"+f1, "")
	f.do("PUT", "/api/webapi/assets/a1/vulns/manual-3/members/"+f2, "")
	if status, _, _ := f.do("POST", "/api/webapi/assets/a1/vulns/manual-2/merge", `{"from":"manual-3"}`); status != 200 {
		t.Fatalf("merge manual = %d", status)
	}
	if exists, _ := findOptional[assetVulnModel](t.Context(), f.capability.store.orm, "id = ?", "manual-3"); exists != nil {
		t.Fatal("merged manual vuln kept")
	}
}

// 复测按条目去重：同条目多候选只复测代表、本轮已有成员的条目不复测、fixed/pending/unconfirmed 不复测。
func TestWebAPIVulnScheduleRetestDedup(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-18T09:30:00Z")
	f.createAsset("a1")
	f.putSchedule("a1", `{"cron":"0 * * * *","auto_verify":false}`)
	f.setNow("2026-11-18T10:00:10Z")
	f.tick()
	run1 := f.runFor("a1", "2026-11-18T10:00:00Z")
	t1 := run1.TaskID
	a1, a1rev := f.addFinding(t1, "订单越权 A", simpleContent(), evidenceAt("GET", "https://api.example.test/orders/1"))
	a2, a2rev := f.addFinding(t1, "订单越权 A'", simpleContent(), evidenceAt("GET", "https://api.example.test/orders/2"))
	b1, b1rev := f.addFinding(t1, "资料越权 B", simpleContent(), evidenceAt("GET", "https://api.example.test/profile/7"))
	c1, c1rev := f.addFinding(t1, "已修复 C", simpleContent(), evidenceAt("POST", "https://api.example.test/login"))
	f.addFinding(t1, "未判读 D", simpleContent(), evidenceAt("GET", "https://api.example.test/search"))
	e1, e1rev := f.addFinding(t1, "被反证 E", simpleContent(), evidenceAt("GET", "https://api.example.test/debug"))
	f.addVerification(t1, a2, a2rev, "supported")
	time.Sleep(2 * time.Millisecond)
	f.addVerification(t1, a1, a1rev, "supported")
	time.Sleep(2 * time.Millisecond)
	f.addRetest(t1, a2, a2rev, "reproducible") // A 的最新“存在”事件属于 a2 → 代表 a2
	f.addVerification(t1, b1, b1rev, "supported")
	f.addVerification(t1, c1, c1rev, "supported")
	f.addRetest(t1, c1, c1rev, "fixed_in_scope")
	f.addVerification(t1, e1, e1rev, "counterevidence")
	f.endResearch(t1)
	f.tick()
	if items := f.followups(run1.ID); len(items) != 0 {
		t.Fatalf("run1 followups = %+v", items)
	}

	// 第二轮：B 被本轮重新发现（由本轮验证覆盖，不复测）。
	f.setNow("2026-11-18T11:00:10Z")
	f.tick()
	run2 := f.runFor("a1", "2026-11-18T11:00:00Z")
	t2 := run2.TaskID
	f.addFinding(t2, "资料越权 B（第二轮）", simpleContent(), evidenceAt("GET", "https://api.example.test/profile/8"))
	f.endResearch(t2)
	f.tick()
	items := f.followups(run2.ID)
	if len(items) != 1 || items[0].Kind != FollowupRetest || items[0].FindingID != a2 || items[0].TaskID != t1 || items[0].RevisionID != a2rev ||
		items[0].TargetID != followupTargetID(run2.ID, a2, a2rev, "retest") || items[0].Status != FollowupLaunched {
		t.Fatalf("run2 retests = %+v", items)
	}
	// 规划前已同步：B 条目含两轮成员。
	list := f.vulns("a1", "")
	if v := vulnOf(list, b1); v.MemberCount != 2 || v.Status != VulnPresent {
		t.Fatalf("B = %+v", v)
	}
	if vulnOf(list, c1).Status != VulnFixed || vulnOf(list, e1).Status != VulnUnconfirmed || vulnOf(list, a1).Status != VulnPresent {
		t.Fatalf("statuses = %+v", list)
	}
}
