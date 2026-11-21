package webapi

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/operation"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
	"google.golang.org/protobuf/proto"
)

// 企业微信推送（D2）测试：httptest 模拟企业微信机器人接口，全部为 example.test 合成数据，
// 不向任何真实外部地址发请求。

type fakeWeCom struct {
	t      *testing.T
	mu     sync.Mutex
	bodies []map[string]any
	codes  []int // 依次作为响应 errcode；用完后为 0
	server *httptest.Server
}

func newFakeWeCom(t *testing.T) *fakeWeCom {
	t.Helper()
	f := &fakeWeCom{t: t}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil || r.Method != http.MethodPost || r.URL.Query().Get("key") != "fixture-key-abcd" {
			http.Error(w, "bad request", 400)
			return
		}
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		code := 0
		if len(f.codes) > 0 {
			code, f.codes = f.codes[0], f.codes[1:]
		}
		f.mu.Unlock()
		msg := "ok"
		if code != 0 {
			msg = fmt.Sprintf("fixture error %d", code)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"errcode": code, "errmsg": msg})
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeWeCom) webhook() string {
	return f.server.URL + "/cgi-bin/webhook/send?key=fixture-key-abcd"
}

func (f *fakeWeCom) respond(codes ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes = append(f.codes, codes...)
}

func (f *fakeWeCom) received() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.bodies...)
}

func (f *fakeWeCom) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bodies = nil
}

type notifyFixture struct {
	*scheduleFixture
	wecom *fakeWeCom
}

func newNotifyFixture(t *testing.T) *notifyFixture {
	t.Helper()
	f := &notifyFixture{scheduleFixture: newScheduleFixture(t, "2026-11-19T06:00:00Z"), wecom: newFakeWeCom(t)}
	f.capability.notify.allowHTTP = true
	f.capability.scheduleLoc = time.FixedZone("CST", 8*3600)
	return f
}

func (f *notifyFixture) configure(body string) map[string]any {
	f.t.Helper()
	status, value, raw := f.do("PUT", "/api/webapi/notify/wecom", body)
	if status != 200 {
		f.t.Fatalf("PUT notify = %d %s", status, raw)
	}
	return value
}

func (f *notifyFixture) enable(requireConfirm bool, baseURL string) {
	f.t.Helper()
	f.configure(fmt.Sprintf(`{"webhook":%q,"enabled":true,"require_confirm":%v,"base_url":%q}`, f.wecom.webhook(), requireConfirm, baseURL))
}

func notifyFlow(method, rawURL, requestBody, responseBody string) *traffic.Flow {
	return &traffic.Flow{
		Id: "we_notify", Complete: true,
		Request: &traffic.HttpRequest{Method: method, Url: rawURL, Protocol: "HTTP/1.1",
			Headers: []*traffic.Header{{Name: "Host", Value: "api.example.test"}, {Name: "Cookie", Value: "sid=fixture-session-abcdef"}},
			Body:    []byte(requestBody)},
		Response: &traffic.HttpResponse{StatusCode: 200, ReasonPhrase: "OK",
			Headers: []*traffic.Header{{Name: "Content-Type", Value: "application/json"}}, Body: []byte(responseBody)},
	}
}

// addEvidence 写一条已完成的 HTTP 证据（合成 Flow）。
func (f *notifyFixture) addEvidence(taskID, sessionID string, flow *traffic.Flow) requestModel {
	f.t.Helper()
	raw, err := proto.Marshal(flow)
	if err != nil {
		f.t.Fatal(err)
	}
	id := "we_" + newSessionID()
	model := requestModel{ID: id, TaskID: taskID, SessionID: sessionID, TurnID: "turn-" + id, CallID: "call-" + id, Fingerprint: "fp-" + id,
		Method: flow.Request.Method, URL: flow.Request.Url, Status: requestCompleted, Sent: sql.NullBool{Bool: true, Valid: true},
		StatusCode: sql.NullInt64{Int64: 200, Valid: true}, Preview: []byte{}, FlowPB: raw, CreatedAt: nowRFC3339(), CompletedAt: sql.NullString{String: nowRFC3339(), Valid: true}}
	if _, err := f.capability.store.orm.NewInsert().Model(&model).Exec(f.t.Context()); err != nil {
		f.t.Fatal(err)
	}
	return model
}

// verify 走真实验证结果工具（webapi_verification_result）保存判读，触发登记。
func (f *notifyFixture) verify(taskID, findingID, revisionID, verdict string, flow *traffic.Flow) VerificationSummary {
	f.t.Helper()
	summary, _, _, err := f.capability.store.saveVerification(f.t.Context(), "v-"+newSessionID(), taskID, findingID, revisionID)
	if err != nil {
		f.t.Fatal(err)
	}
	evidence := f.addEvidence(taskID, summary.SessionID, flow)
	relation := map[string]string{"supported": "supporting", "counterevidence": "counterevidence", "inconclusive": "limitation"}[verdict]
	inv := operation.Invocation{SessionID: summary.SessionID, TurnID: "verify-turn", CallID: "verify-call-" + summary.VerificationID}
	if result, message := executeVerification(f.t, &verificationTool{store: f.capability.store}, inv, verificationArguments(verdict, evidence.ID, relation)); result == nil {
		f.t.Fatalf("verification result rejected: %s", message)
	}
	time.Sleep(2 * time.Millisecond) // 结果时间严格递增
	f.advance(time.Second)           // 通知 created_at 严格递增
	return *summary
}

func (f *notifyFixture) notifications() []notificationModel {
	f.t.Helper()
	var items []notificationModel
	if err := f.capability.store.orm.NewSelect().Model(&items).OrderExpr("created_at ASC, id ASC").Scan(f.t.Context()); err != nil {
		f.t.Fatal(err)
	}
	return items
}

func (f *notifyFixture) notificationByID(id string) notificationModel {
	f.t.Helper()
	item, err := f.capability.store.notification(f.t.Context(), id)
	if err != nil || item == nil {
		f.t.Fatalf("notification %s = %v %v", id, item, err)
	}
	return *item
}

func (f *notifyFixture) deliver() {
	f.t.Helper()
	if err := f.capability.deliverNotifications(f.t.Context()); err != nil {
		f.t.Fatalf("deliver: %v", err)
	}
}

func (f *notifyFixture) advance(d time.Duration) { f.now = f.now.Add(d) }

func (f *notifyFixture) plainTask(id string) {
	f.t.Helper()
	body := `{"known_info":"演示接口","objective":"检查越权","authorization":{"confirmed":true,"origins":["https://api.example.test"]}}`
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/"+id, body); status != 201 {
		f.t.Fatalf("create task = %d %s", status, raw)
	}
}

func notifyContent() findingContent {
	return findingContent{
		Description: "订单详情接口未校验订单归属，普通用户可读取他人订单。影响范围包括地址与手机号。",
		Steps: []findingStepInput{
			{Text: "以普通用户 demo-user 登录演示环境"},
			{Text: "请求 GET /orders/1002（属于其他用户的订单）"},
			{Text: "响应 200 并返回\n他人订单的收货信息"},
			{Text: "第四步不应出现在推送中"},
		},
	}
}

func TestNotifySettingsAPI(t *testing.T) {
	f := newNotifyFixture(t)
	f.capability.notify.allowHTTP = false
	status, value, _ := f.do("GET", "/api/webapi/notify/wecom", "")
	if status != 200 || value["enabled"] != false || value["has_webhook"] != false || value["webhook_hint"] != "" || value["updated_at"] != nil {
		t.Fatalf("default GET = %d %+v", status, value)
	}
	if status, _, _ := f.do("POST", "/api/webapi/notify/wecom/test", ""); status != 409 {
		t.Fatalf("test unconfigured = %d", status)
	}
	for _, body := range []string{
		`{"webhook":"http://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x","enabled":true}`,
		`{"webhook":"ftp://qyapi.weixin.qq.com/send","enabled":true}`,
		`{"webhook":"qyapi.weixin.qq.com/send?key=x","enabled":true}`,
		`{"webhook":"https://user:pw@qyapi.weixin.qq.com/send?key=x","enabled":true}`,
		`{"webhook":"https://qyapi.weixin.qq.com/send?key=x","enabled":true,"extra":1}`,
		`{"enabled":true,"base_url":"ftp://wscyber.example.test"}`,
		`{"enabled":true,"base_url":"https://wscyber.example.test/a(b)"}`,
	} {
		if status, _, raw := f.do("PUT", "/api/webapi/notify/wecom", body); status != 400 {
			t.Errorf("PUT %s = %d %s", body, status, raw)
		}
	}
	// 仅保存（不发送）：真实地址形态只用于校验与提示，测试中不会向其发请求。
	realShape := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=00000000-1111-2222-3333-44445555abcd"
	_, _, raw := f.do("PUT", "/api/webapi/notify/wecom", `{"webhook":"`+realShape+`","enabled":true,"require_confirm":true,"base_url":"https://wscyber.example.test/"}`)
	var view NotifySettingsView
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Enabled || !view.HasWebhook || view.WebhookHint != "qyapi.weixin.qq.com …abcd" || !view.RequireConfirm || view.BaseURL != "https://wscyber.example.test" || view.UpdatedAt == nil {
		t.Fatalf("PUT view = %+v", view)
	}
	if strings.Contains(raw, "44445555abcd") {
		t.Fatal("PUT 响应回显了 webhook key")
	}
	stored, _ := f.capability.store.notifySettings(t.Context())
	if stored == nil || !strings.HasPrefix(stored.WebhookCipher, secretCipherV1) || strings.Contains(stored.WebhookCipher, "qyapi") {
		t.Fatalf("stored cipher = %+v", stored)
	}
	if info, err := os.Stat(filepath.Join(filepath.Dir(f.database), secretKeyFileName)); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("key file = %v %v", info, err)
	}
	// AAD 绑定 notify:wecom：同密钥不能当作资产测试账号密文解开。
	aead, _ := f.capability.secrets.load(false)
	if _, err := openSecret(aead, "wecom", "notify", stored.WebhookCipher); err == nil {
		t.Fatal("webhook 密文未绑定 AAD")
	}
	// "" 与省略都保留原值；改其他字段不要求重填。
	for _, body := range []string{`{"webhook":"","enabled":false,"require_confirm":false,"base_url":""}`, `{"enabled":true}`} {
		_, value, raw = f.do("PUT", "/api/webapi/notify/wecom", body)
		if value["has_webhook"] != true || value["webhook_hint"] != "qyapi.weixin.qq.com …abcd" || strings.Contains(raw, "44445555abcd") {
			t.Fatalf("keep webhook %s = %s", body, raw)
		}
	}
	_, value, raw = f.do("GET", "/api/webapi/notify/wecom", "")
	if value["enabled"] != true || value["require_confirm"] != false || value["base_url"] != "" || strings.Contains(raw, "44445555abcd") {
		t.Fatalf("GET after keep = %s", raw)
	}
	_, value, _ = f.do("PUT", "/api/webapi/notify/wecom", `{"webhook":"-","enabled":true}`)
	if value["has_webhook"] != false || value["webhook_hint"] != "" {
		t.Fatalf("clear webhook = %+v", value)
	}
	if status, _, _ := f.do("POST", "/api/webapi/notify/wecom/test", ""); status != 409 {
		t.Fatalf("test after clear = %d", status)
	}

	// 测试推送：只发往 httptest 模拟接口。
	f.capability.notify.allowHTTP = true
	f.enable(false, "")
	status, value, raw = f.do("POST", "/api/webapi/notify/wecom/test", "")
	if status != 200 || value["ok"] != true || value["errcode"] != float64(0) {
		t.Fatalf("test push = %d %s", status, raw)
	}
	got := f.wecom.received()
	if len(got) != 1 || got[0]["msgtype"] != "markdown" || got[0]["markdown"].(map[string]any)["content"] != "wscyber 推送测试" {
		t.Fatalf("test push body = %+v", got)
	}
	f.wecom.respond(93000)
	status, value, raw = f.do("POST", "/api/webapi/notify/wecom/test", "")
	if status != 200 || value["ok"] != false || value["errcode"] != float64(93000) || strings.Contains(raw, "fixture-key-abcd") {
		t.Fatalf("test push error = %d %s", status, raw)
	}
}

func TestNotifyRegistrationAndDedupe(t *testing.T) {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	f.launchManual("a1", "t1")
	flow := notifyFlow("GET", "https://api.example.test/orders/1002", "", `{"order":"1002"}`)
	f1, f1rev := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))

	// 未配置 webhook：不登记。
	f.verify("t1", f1, f1rev, "supported", flow)
	if items := f.notifications(); len(items) != 0 {
		t.Fatalf("unconfigured registered %+v", items)
	}
	// 已配置但未启用：不登记。
	f.configure(fmt.Sprintf(`{"webhook":%q,"enabled":false}`, f.wecom.webhook()))
	f.verify("t1", f1, f1rev, "supported", flow)
	if items := f.notifications(); len(items) != 0 {
		t.Fatalf("disabled registered %+v", items)
	}

	f.enable(false, "")
	f2, f2rev := f.addFinding("t1", "订单接口越权读取（另一订单）", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/2002"))
	f3, f3rev := f.addFinding("t1", "用户导出未鉴权", notifyContent(), evidenceAt("GET", "https://api.example.test/export"))
	// 非 supported 不登记。
	f.verify("t1", f3, f3rev, "counterevidence", notifyFlow("GET", "https://api.example.test/export", "", "{}"))
	f.verify("t1", f3, f3rev, "inconclusive", notifyFlow("GET", "https://api.example.test/export", "", "{}"))
	if items := f.notifications(); len(items) != 0 {
		t.Fatalf("non-supported registered %+v", items)
	}

	// f1 此前已有 supported（未启用时）；启用后首个 supported 仍登记一次 new。
	v1 := f.verify("t1", f1, f1rev, "supported", flow)
	vulnID := assetVulnID("a1", "GET https://api.example.test/orders/{id}")
	items := f.notifications()
	if len(items) != 1 || items[0].Kind != NotifyKindNew || items[0].Status != NotifyQueued || items[0].DedupeKey != vulnID || items[0].VulnID != vulnID ||
		items[0].VerificationID != v1.VerificationID || items[0].TaskID != "t1" || items[0].FindingID != f1 || !items[0].NextAttemptAt.Valid || items[0].Attempts != 0 {
		t.Fatalf("registered = %+v", items)
	}
	// 同一 Invocation 回放结果：不重复。
	inv := operation.Invocation{SessionID: v1.SessionID, TurnID: "verify-turn", CallID: "verify-call-" + v1.VerificationID}
	var evidenceID string
	_ = f.capability.store.db.QueryRowContext(t.Context(), `SELECT id FROM webapi_requests WHERE session_id = ?`, v1.SessionID).Scan(&evidenceID)
	if result, message := executeVerification(t, &verificationTool{store: f.capability.store}, inv, verificationArguments("supported", evidenceID, "supporting")); result == nil {
		t.Fatalf("replay rejected: %s", message)
	}
	// 同一漏洞条目的第二个候选（不同任务）supported：不重复推。
	f.launchManual("a1", "t2")
	f4, f4rev := f.addFinding("t2", "订单接口越权读取（第二轮）", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/3003"))
	f.verify("t2", f4, f4rev, "supported", flow)
	f.verify("t1", f2, f2rev, "supported", flow)
	if items := f.notifications(); len(items) != 1 {
		t.Fatalf("same vuln pushed again: %+v", items)
	}

	// 复测已修复后再次 supported：登记 regression；再一次 supported 不再重复。
	f.addRetest("t2", f4, f4rev, "fixed_in_scope")
	time.Sleep(2 * time.Millisecond)
	vr := f.verify("t2", f4, f4rev, "supported", flow)
	f.verify("t1", f2, f2rev, "supported", flow)
	items = f.notifications()
	if len(items) != 2 || items[1].Kind != NotifyKindRegression || items[1].DedupeKey != vulnID || items[1].VerificationID != vr.VerificationID || items[1].FindingID != f4 {
		t.Fatalf("regression = %+v", items)
	}
	// 复测仍可复现（reproducible）不算修复：之后 supported 不推。
	f.addRetest("t2", f4, f4rev, "reproducible")
	time.Sleep(2 * time.Millisecond)
	f.verify("t2", f4, f4rev, "supported", flow)
	if items := f.notifications(); len(items) != 2 {
		t.Fatalf("reproducible treated as fixed: %+v", items)
	}

	// 未关联资产的任务：去重键 finding:<id>。
	f.plainTask("plain-1")
	p1, p1rev := f.addFinding("plain-1", "无资产候选", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))
	f.verify("plain-1", p1, p1rev, "supported", flow)
	f.verify("plain-1", p1, p1rev, "supported", flow)
	items = f.notifications()
	if len(items) != 3 || items[2].DedupeKey != "finding:"+p1 || items[2].VulnID != "" || items[2].Kind != NotifyKindNew {
		t.Fatalf("plain = %+v", items)
	}

	// 列表：倒序、asset、title、vuln_id、过滤、分页。
	status, _, raw := f.do("GET", "/api/webapi/notifications?limit=2", "")
	var list struct {
		Items      []NotificationView `json:"items"`
		HasMore    bool               `json:"has_more"`
		NextCursor *string            `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(raw), &list); err != nil || status != 200 {
		t.Fatalf("list = %d %s", status, raw)
	}
	if len(list.Items) != 2 || !list.HasMore || list.NextCursor == nil || list.Items[0].FindingID != p1 || list.Items[0].Asset != nil || list.Items[0].VulnID != nil ||
		list.Items[1].Kind != NotifyKindRegression || list.Items[1].Asset == nil || list.Items[1].Asset.Name != "演示库存系统" || list.Items[1].Title != "订单接口越权读取（第二轮）" ||
		list.Items[1].VulnID == nil || *list.Items[1].VulnID != vulnID || list.Items[1].FindingNumber == "" || list.Items[1].VerificationSessionID != vr.SessionID || list.Items[1].Status != NotifyQueued {
		t.Fatalf("list page1 = %s", raw)
	}
	_, _, raw = f.do("GET", "/api/webapi/notifications?limit=2&cursor="+*list.NextCursor, "")
	_ = json.Unmarshal([]byte(raw), &list)
	if len(list.Items) != 1 || list.HasMore || list.Items[0].Kind != NotifyKindNew || list.Items[0].FindingID != f1 {
		t.Fatalf("list page2 = %s", raw)
	}
	for _, query := range []string{"?status=bogus", "?limit=0", "?limit=201", "?cursor=@@"} {
		if status, _, _ := f.do("GET", "/api/webapi/notifications"+query, ""); status != 400 {
			t.Errorf("list %s = %d", query, status)
		}
	}
	if _, value, _ := f.do("GET", "/api/webapi/notifications?status=sent", ""); len(value["items"].([]any)) != 0 {
		t.Fatalf("status filter = %+v", value)
	}
}

func TestNotifyRequireConfirmFlow(t *testing.T) {
	f := newNotifyFixture(t)
	f.enable(true, "")
	f.plainTask("t1")
	flow := notifyFlow("GET", "https://api.example.test/orders/1002", "", "{}")
	a, arev := f.addFinding("t1", "候选甲", notifyContent(), nil)
	b, brev := f.addFinding("t1", "候选乙", notifyContent(), nil)
	f.verify("t1", a, arev, "supported", flow)
	f.verify("t1", b, brev, "supported", flow)
	items := f.notifications()
	if len(items) != 2 || items[0].Status != NotifyPendingConfirm || items[0].NextAttemptAt.Valid {
		t.Fatalf("pending = %+v", items)
	}
	first, second := items[0].ID, items[1].ID
	// 待确认的不投递。
	f.deliver()
	if got := f.wecom.received(); len(got) != 0 {
		t.Fatalf("pending delivered: %+v", got)
	}
	post := func(id, action string) (int, map[string]any) {
		status, value, _ := f.do("POST", "/api/webapi/notifications/"+id+"/"+action, "")
		return status, value
	}
	if status, _ := post(first, "retry"); status != 409 {
		t.Fatalf("retry pending = %d", status)
	}
	if status, _ := post("wn_missing", "confirm"); status != 404 {
		t.Fatalf("confirm missing = %d", status)
	}
	status, value := post(first, "confirm")
	if status != 200 || value["status"] != NotifyQueued || value["next_attempt_at"] == nil {
		t.Fatalf("confirm = %d %+v", status, value)
	}
	if status, _ := post(first, "confirm"); status != 409 {
		t.Fatalf("confirm twice = %d", status)
	}
	if status, _ := post(first, "dismiss"); status != 409 {
		t.Fatalf("dismiss queued = %d", status)
	}
	if status, value := post(second, "dismiss"); status != 200 || value["status"] != NotifyDismissed {
		t.Fatalf("dismiss pending = %d %+v", status, value)
	}
	// 投递失败 3 次 → failed → retry（attempts 清零）→ 成功。
	f.wecom.respond(93000, 93000, 93000)
	for range 3 {
		f.deliver()
		f.advance(10 * time.Minute)
	}
	if item := f.notificationByID(first); item.Status != NotifyFailed || item.Attempts != 3 {
		t.Fatalf("not failed: %+v", item)
	}
	status, value = post(first, "retry")
	if status != 200 || value["status"] != NotifyQueued || value["attempts"] != float64(0) {
		t.Fatalf("retry = %d %+v", status, value)
	}
	f.deliver()
	if item := f.notificationByID(first); item.Status != NotifySent || !item.SentAt.Valid {
		t.Fatalf("after retry: %+v", item)
	}
	if status, _ := post(first, "dismiss"); status != 409 {
		t.Fatalf("dismiss sent = %d", status)
	}
	if status, _ := post(second, "retry"); status != 409 {
		t.Fatalf("retry dismissed = %d", status)
	}
}

func TestNotifyDeliveryMarkdownAndImage(t *testing.T) {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"普通用户","username":"demo-user","secret":"Fixture-Pass-9"}`); status != 200 {
		t.Fatalf("credential = %d %s", status, raw)
	}
	f.launchManual("a1", "t1")
	f.enable(false, "https://wscyber.example.test/")
	content := notifyContent()
	content.Steps[0].Text = "以 demo-user / Fixture-Pass-9 登录演示环境"
	fid, frev := f.addFinding("t1", "订单接口越权读取", content, evidenceAt("GET", "https://api.example.test/orders/1002"))
	flow := notifyFlow("POST", "https://api.example.test/orders/1002?expand=1", `{"user":"demo-user","password":"Fixture-Pass-9"}`, `{"order":"1002","owner":"user-42"}`)
	v := f.verify("t1", fid, frev, "supported", flow)
	// 本用例覆盖旧结果（K1 前无最小复现）的文案；带最小复现的推送见 vuln_card_test.go。
	if _, err := f.capability.store.db.ExecContext(t.Context(), `UPDATE webapi_verification_results SET payload_json = json_remove(payload_json, '$.payload.impact', '$.payload.key_steps') WHERE verification_id = ?`, v.VerificationID); err != nil {
		t.Fatal(err)
	}
	var resultAt string
	_ = f.capability.store.db.QueryRowContext(t.Context(), `SELECT created_at FROM webapi_verification_results WHERE verification_id = ?`, v.VerificationID).Scan(&resultAt)
	local := parseAt(resultAt).In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04")

	f.deliver()
	got := f.wecom.received()
	if len(got) != 2 || got[0]["msgtype"] != "markdown" || got[1]["msgtype"] != "image" {
		t.Fatalf("sent = %+v", got)
	}
	want := "**【新确认漏洞】订单接口越权读取**\n" +
		"> 资产：演示库存系统\n" +
		"> 位置：POST https://api.example.test/orders/1002?expand=1\n" +
		"> 判读：独立验证支持（" + local + "）\n" +
		"危害：订单详情接口未校验订单归属，普通用户可读取他人订单。\n" +
		"复现要点：\n" +
		"1. 以 demo-user / ****** 登录演示环境\n" +
		"2. 请求 GET /orders/1002（属于其他用户的订单）\n" +
		"3. 响应 200 并返回 他人订单的收货信息\n" +
		"[在平台查看](https://wscyber.example.test/?page=vulns&vuln=" + vulnOf(f.vulns("a1", ""), fid).ID + ")"
	markdown := got[0]["markdown"].(map[string]any)["content"].(string)
	if markdown != want {
		t.Fatalf("markdown =\n%s\nwant\n%s", markdown, want)
	}
	image := got[1]["image"].(map[string]any)
	decoded, err := base64.StdEncoding.DecodeString(image["base64"].(string))
	if err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(decoded)
	if image["md5"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("md5 = %v want %x", image["md5"], sum)
	}
	if img, err := png.Decode(bytes.NewReader(decoded)); err != nil || img.Bounds().Dx() != evidenceImageWidth {
		t.Fatalf("png decode = %v %v", img, err)
	}
	// 图片与“遮盖开 + 测试账号替换”的确定性渲染逐字节一致。
	scrubbed := proto.Clone(flow).(*traffic.Flow)
	scrubFlow(scrubbed, []string{"Fixture-Pass-9"})
	if bytes.Contains(scrubbed.Request.Body, []byte("Fixture-Pass-9")) || !bytes.Contains(scrubbed.Request.Body, []byte(notifySecretMask)) {
		t.Fatalf("scrubFlow body = %s", scrubbed.Request.Body)
	}
	var evidence requestModel
	_ = f.capability.store.orm.NewSelect().Model(&evidence).Where("session_id = ?", v.SessionID).Scan(t.Context())
	expected, err := renderEvidenceImage(scrubbed, requestEvidenceImageMeta(evidence), evidenceImageOptions{})
	if err != nil || !bytes.Equal(expected, decoded) {
		t.Fatalf("image differs from masked render (err %v)", err)
	}
	item := f.notifications()[0]
	if item.Status != NotifySent || !item.SentAt.Valid || item.Attempts != 0 || item.LastError != "" || item.NextAttemptAt.Valid {
		t.Fatalf("after send = %+v", item)
	}
	// 已发送的不再投递。
	f.wecom.reset()
	f.advance(time.Hour)
	f.deliver()
	if len(f.wecom.received()) != 0 {
		t.Fatal("sent notification delivered again")
	}
}

func TestNotifyMarkdownTruncationAndNoBaseURL(t *testing.T) {
	fields := notifyFields{
		Regression: true, Title: "上传接口\n路径穿越", Location: "PUT https://api.example.test/files", VerifiedAt: "2026-11-19 14:00",
		Danger: capRunes(oneLine(firstSentence("Attacker can overwrite files. Second sentence.")), notifyDangerRunes),
		Steps:  []string{capRunes(oneLine(strings.Repeat("步", 100)), notifyStepRunes)},
	}
	want := "**【漏洞复现】上传接口 路径穿越**\n> 资产：未关联资产\n> 位置：PUT https://api.example.test/files\n> 判读：独立验证支持（2026-11-19 14:00）\n" +
		"危害：Attacker can overwrite files.\n复现要点：\n1. " + strings.Repeat("步", 79) + "…"
	if got := composeNotifyMarkdown(fields); got != want {
		t.Fatalf("no base_url markdown =\n%s\nwant\n%s", got, want)
	}
	if got := firstSentence("第一句没有句号\n第二行"); got != "第一句没有句号" {
		t.Fatalf("firstSentence newline = %q", got)
	}
	if got := capRunes(oneLine(firstSentence(strings.Repeat("长", 200)+"。")), notifyDangerRunes); utf8.RuneCountInString(got) != notifyDangerRunes || !strings.HasSuffix(got, "…") {
		t.Fatalf("danger cap = %q", got)
	}
	// 超长：位置（URL）极长时截断标题外字段，总长 ≤4096 字节，标题与链接完整、结构保留。
	fields.Title = strings.Repeat("题", 80)
	fields.Location = "GET https://api.example.test/search?q=" + strings.Repeat("a", 6000)
	fields.Link = "https://wscyber.example.test/sessions/0123456789abcdef"
	fields.Note = notifyImageTooLarge
	got := composeNotifyMarkdown(fields)
	if len(got) > wecomMarkdownLimit || len(got) < wecomMarkdownLimit-200 {
		t.Fatalf("truncated len = %d", len(got))
	}
	lines := strings.Split(got, "\n")
	if lines[0] != "**【漏洞复现】"+strings.Repeat("题", 80)+"**" || !strings.HasPrefix(lines[2], "> 位置：GET https://api.example.test/search?q=aaa") || !strings.HasSuffix(lines[2], "…") ||
		lines[len(lines)-2] != "[在平台查看](https://wscyber.example.test/sessions/0123456789abcdef)" || lines[len(lines)-1] != notifyImageTooLarge || lines[1] != "> 资产：未关联资产" {
		t.Fatalf("truncated lines = %q", lines)
	}
}

func TestNotifyRetryFailedAndThrottle(t *testing.T) {
	f := newNotifyFixture(t)
	f.enable(false, "")
	f.plainTask("t1")
	flow := notifyFlow("GET", "https://api.example.test/orders/1002", "", "{}")
	a, arev := f.addFinding("t1", "候选甲", notifyContent(), nil)
	b, brev := f.addFinding("t1", "候选乙", notifyContent(), nil)
	f.verify("t1", a, arev, "supported", flow)
	f.verify("t1", b, brev, "supported", flow)
	items := f.notifications()
	first, second := items[0].ID, items[1].ID

	// 45009：不计次、推迟 60s，本轮停止（第二条不发送）。
	f.wecom.respond(wecomErrRateLimited)
	f.deliver()
	if got := f.wecom.received(); len(got) != 1 {
		t.Fatalf("after 45009 sent %d", len(got))
	}
	item := f.notificationByID(first)
	if item.Status != NotifyQueued || item.Attempts != 0 || item.NextAttemptAt.String != notifyTime(f.now.Add(60*time.Second)) || !strings.Contains(item.LastError, "45009") {
		t.Fatalf("throttled = %+v", item)
	}
	if other := f.notificationByID(second); other.Attempts != 0 || other.LastError != "" {
		t.Fatalf("second touched: %+v", other)
	}
	// 第二条仍到期：下一轮发送它；第一条未到期不动。
	f.wecom.reset()
	f.deliver()
	if got := f.wecom.received(); len(got) != 2 || f.notificationByID(second).Status != NotifySent || f.notificationByID(first).Status != NotifyQueued {
		t.Fatalf("second round = %d %+v", len(got), f.notificationByID(first))
	}

	// errcode≠0：1 → 2 → 3 次，间隔 1 分钟、5 分钟，第 3 次 failed。
	f.wecom.reset()
	f.advance(60 * time.Second)
	f.wecom.respond(93000, 93000, 93000)
	f.deliver()
	item = f.notificationByID(first)
	if item.Status != NotifyQueued || item.Attempts != 1 || item.NextAttemptAt.String != notifyTime(f.now.Add(time.Minute)) || !strings.HasPrefix(item.LastError, "markdown：errcode 93000") {
		t.Fatalf("attempt1 = %+v", item)
	}
	f.advance(59 * time.Second)
	f.deliver() // 未到期
	if got := f.wecom.received(); len(got) != 1 {
		t.Fatalf("not-due delivered: %d", len(got))
	}
	f.advance(time.Second)
	f.deliver()
	item = f.notificationByID(first)
	if item.Attempts != 2 || item.NextAttemptAt.String != notifyTime(f.now.Add(5*time.Minute)) {
		t.Fatalf("attempt2 = %+v", item)
	}
	f.advance(5 * time.Minute)
	f.deliver()
	item = f.notificationByID(first)
	if item.Status != NotifyFailed || item.Attempts != 3 || item.NextAttemptAt.Valid || strings.Contains(item.LastError, "fixture-key") {
		t.Fatalf("attempt3 = %+v", item)
	}
	f.advance(time.Hour)
	f.deliver()
	if got := f.wecom.received(); len(got) != 3 {
		t.Fatalf("failed delivered again: %d", len(got))
	}

	// 图片发送失败也计次；网络错误不外露 webhook 地址。
	_, _ = f.capability.store.orm.NewUpdate().Model((*notificationModel)(nil)).Set("status = ?", NotifyQueued).Set("attempts = 0").Set("next_attempt_at = ?", notifyTime(f.now)).Where("id = ?", first).Exec(t.Context())
	f.wecom.respond(0, 40008)
	f.deliver()
	if item = f.notificationByID(first); item.Attempts != 1 || !strings.HasPrefix(item.LastError, "图片：errcode 40008") {
		t.Fatalf("image failure = %+v", item)
	}
	f.wecom.server.Close()
	f.advance(time.Minute)
	f.deliver()
	if item = f.notificationByID(first); item.Attempts != 2 || !strings.HasPrefix(item.LastError, "markdown：网络错误") || strings.Contains(item.LastError, "fixture-key") || strings.Contains(item.LastError, "/cgi-bin/") {
		t.Fatalf("network failure = %+v", item)
	}
}

func TestNotifyRateLimitAndImageSize(t *testing.T) {
	f := newNotifyFixture(t)
	f.enable(false, "")
	f.plainTask("t1")
	var body strings.Builder
	for i := 1; i <= 150; i++ {
		fmt.Fprintf(&body, "{\"row\":%d,\"name\":\"item-%03d\"}\n", i, i)
	}
	flow := notifyFlow("GET", "https://api.example.test/items", "", body.String())
	a, arev := f.addFinding("t1", "候选甲", notifyContent(), nil)
	v := f.verify("t1", a, arev, "supported", flow)
	// 同一验证再挂 11 条不同去重键的通知（共 12 条，每条 markdown + 图片 = 2 次发送）。
	for i := range 11 {
		model := &notificationModel{ID: newNotificationID(), DedupeKey: fmt.Sprintf("finding:extra-%d", i), Kind: NotifyKindNew, TaskID: "t1", FindingID: a, VerificationID: v.VerificationID,
			Status: NotifyQueued, NextAttemptAt: sql.NullString{String: notifyTime(f.now), Valid: true}, CreatedAt: notifyTime(f.now)}
		if _, err := f.capability.store.insertNotification(t.Context(), model); err != nil {
			t.Fatal(err)
		}
	}
	countSent := func() int {
		n := 0
		for _, item := range f.notifications() {
			if item.Status == NotifySent {
				n++
			}
		}
		return n
	}
	f.deliver()
	if got := len(f.wecom.received()); got != 18 || countSent() != 9 {
		t.Fatalf("rate window: sent %d messages, %d notifications", got, countSent())
	}
	f.advance(30 * time.Second)
	f.deliver()
	if got := len(f.wecom.received()); got != 18 {
		t.Fatalf("window not respected: %d", got)
	}
	f.advance(31 * time.Second)
	f.deliver()
	if got := len(f.wecom.received()); got != 24 || countSent() != 12 {
		t.Fatalf("after window: %d messages, %d sent", got, countSent())
	}

	// 图片超限：降到 30 行重渲；仍超则不发图、markdown 末尾注明。
	var evidence requestModel
	_ = f.capability.store.orm.NewSelect().Model(&evidence).Where("session_id = ?", v.SessionID).Scan(t.Context())
	full, _ := renderEvidenceImage(flow, requestEvidenceImageMeta(evidence), evidenceImageOptions{})
	short, _ := renderEvidenceImage(flow, requestEvidenceImageMeta(evidence), evidenceImageOptions{MaxLines: 30})
	if len(short) >= len(full) {
		t.Fatalf("30 行图不小于 60 行：%d %d", len(short), len(full))
	}
	requeue := func(key string) {
		model := &notificationModel{ID: newNotificationID(), DedupeKey: key, Kind: NotifyKindNew, TaskID: "t1", FindingID: a, VerificationID: v.VerificationID,
			Status: NotifyQueued, NextAttemptAt: sql.NullString{String: notifyTime(f.now), Valid: true}, CreatedAt: notifyTime(f.now)}
		if _, err := f.capability.store.insertNotification(t.Context(), model); err != nil {
			t.Fatal(err)
		}
	}
	f.advance(2 * time.Minute)
	f.wecom.reset()
	f.capability.notify.imageLimit = len(short)
	requeue("finding:size-30")
	f.deliver()
	got := f.wecom.received()
	if len(got) != 2 {
		t.Fatalf("30-line image: %d messages", len(got))
	}
	decoded, _ := base64.StdEncoding.DecodeString(got[1]["image"].(map[string]any)["base64"].(string))
	if !bytes.Equal(decoded, short) || strings.Contains(got[0]["markdown"].(map[string]any)["content"].(string), notifyImageTooLarge) {
		t.Fatal("expected 30-line image")
	}
	f.wecom.reset()
	f.capability.notify.imageLimit = len(short) - 1
	requeue("finding:too-large")
	f.deliver()
	got = f.wecom.received()
	if len(got) != 1 || !strings.HasSuffix(got[0]["markdown"].(map[string]any)["content"].(string), "\n"+notifyImageTooLarge) {
		t.Fatalf("too large = %+v", got)
	}
	if items := f.notifications(); items[len(items)-1].Status != NotifySent {
		t.Fatalf("markdown-only not sent: %+v", items[len(items)-1])
	}
}

func TestNotifyGoroutineDeliversAndStopsOnClose(t *testing.T) {
	f := newNotifyFixture(t)
	f.capability.scheduleNow = nil // 后台 goroutine 用真实时钟
	f.capability.notify.interval = 20 * time.Millisecond
	f.enable(false, "")
	f.plainTask("t1")
	a, arev := f.addFinding("t1", "候选甲", notifyContent(), nil)
	f.verify("t1", a, arev, "supported", notifyFlow("GET", "https://api.example.test/orders/1002", "", "{}"))
	// 上次 Hub 中断在 sending 的通知：启动时回到 queued 并投递。
	id := f.notifications()[0].ID
	if _, err := f.capability.store.db.ExecContext(t.Context(), `UPDATE webapi_notifications SET status = ? WHERE id = ?`, NotifySending, id); err != nil {
		t.Fatal(err)
	}
	f.capability.startNotifier()
	deadline := time.Now().Add(10 * time.Second)
	for f.notificationByID(id).Status != NotifySent {
		if time.Now().After(deadline) {
			t.Fatalf("goroutine did not deliver: %+v", f.notificationByID(id))
		}
		time.Sleep(10 * time.Millisecond)
	}
	done := f.capability.notify.done
	closed := make(chan struct{})
	go func() {
		_ = f.capability.Close(context.Background())
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return")
	}
	select {
	case <-done:
	default:
		t.Fatal("notifier goroutine still running after Close")
	}
	if _, ok := notifyCapabilities.Load(f.capability.store); ok {
		t.Fatal("capability still registered after Close")
	}
	before := len(f.wecom.received())
	time.Sleep(100 * time.Millisecond)
	if after := len(f.wecom.received()); after != before {
		t.Fatalf("sent after Close: %d → %d", before, after)
	}
}

// U2a/D1 阶段（二十三表）旧库补推送两表；补表后完整原生校验通过且可写入。
func TestNotifySchemaUpgradeFromD1Stage(t *testing.T) {
	database := filepath.Join(t.TempDir(), "hub.sqlite")
	stage, err := webservice.NewSQLiteStore(database, webapiSchemaStage(23, 22))
	if err != nil {
		t.Fatal(err)
	}
	_ = stage.Close()
	if err := PrepareWebAPISchema(database); err != nil {
		t.Fatalf("prepare D1 stage: %v", err)
	}
	complete, err := webservice.NewSQLiteStore(database, SchemaModule())
	if err != nil {
		t.Fatalf("full native validation after prepare: %v", err)
	}
	_ = complete.Close()
	capability, err := NewCapability(database)
	if err != nil {
		t.Fatal(err)
	}
	defer capability.Close(context.Background())
	if err := capability.store.putNotifySettings(t.Context(), &notifySettingsModel{Enabled: true, UpdatedAt: nowRFC3339()}); err != nil {
		t.Fatalf("settings after upgrade: %v", err)
	}
	model := &notificationModel{ID: newNotificationID(), DedupeKey: "finding:x", Kind: NotifyKindNew, TaskID: "t", FindingID: "x", VerificationID: "v", Status: NotifyQueued, CreatedAt: notifyTime(time.Now())}
	if inserted, err := capability.store.insertNotification(t.Context(), model); err != nil || !inserted {
		t.Fatalf("insert after upgrade: %v %v", inserted, err)
	}
	model.ID = newNotificationID()
	model.Kind = NotifyKindRegression
	if inserted, err := capability.store.insertNotification(t.Context(), model); err != nil || !inserted {
		t.Fatalf("regression insert: %v %v", inserted, err)
	}
	model.ID = newNotificationID()
	if inserted, _ := capability.store.insertNotification(t.Context(), model); inserted {
		t.Fatal("unique (dedupe_key, kind, verification_id) not enforced")
	}
	if err := PrepareWebAPISchema(database); err != nil {
		t.Fatalf("prepare complete schema again: %v", err)
	}
}

// ---- K2：推送以漏洞卡为准、漏洞卡人工推送 ----

func (f *notifyFixture) cardNotify(key, body string) (int, map[string]any, string) {
	f.t.Helper()
	return f.do("POST", "/api/webapi/vulns/"+key+"/card/notify", body)
}

func (f *notifyFixture) notifyPreview(key string) (int, NotifyPreview, string) {
	f.t.Helper()
	status, _, raw := f.do("GET", "/api/webapi/vulns/"+key+"/card/notify/preview", "")
	var preview NotifyPreview
	_ = json.Unmarshal([]byte(raw), &preview)
	return status, preview, raw
}

func vulnNotifyAudits(t *testing.T, f *notifyFixture) []auditEventModel {
	return auditByAction(auditRows(t, f.capability), "vuln.notify")
}

// 自动推送：标题取漏洞卡 header.title（人工改名优先），链接为漏洞卡 URL（finding: 键经 QueryEscape）。
func TestNotifyUsesCardTitleAndLink(t *testing.T) {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	f.launchManual("a1", "t1")
	f.enable(false, "https://wscyber.example.test/")
	flow := notifyFlow("GET", "https://api.example.test/orders/1002", "", `{"order":"1002"}`)
	fid, frev := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))
	f.verify("t1", fid, frev, "supported", flow)
	vid := vulnOf(f.vulns("a1", ""), fid).ID
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/vulns/"+vid, `{"title":"人工命名：订单越权"}`); status != 200 {
		t.Fatalf("rename = %d %s", status, raw)
	}
	f.deliver()
	got := f.wecom.received()
	if len(got) == 0 {
		t.Fatal("nothing sent")
	}
	lines := strings.Split(got[0]["markdown"].(map[string]any)["content"].(string), "\n")
	if lines[0] != "**【新确认漏洞】人工命名：订单越权**" {
		t.Fatalf("title line = %q", lines[0])
	}
	if want := "[在平台查看](https://wscyber.example.test/?page=vulns&vuln=" + vid + ")"; lines[len(lines)-1] != want {
		t.Fatalf("link line = %q want %q", lines[len(lines)-1], want)
	}
	// 无资产：键 finding:<id>，链接转义冒号；无改名时用最新成员修订标题。
	f.wecom.reset()
	f.plainTask("plain-1")
	p1, p1rev := f.addFinding("plain-1", "无资产候选", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))
	f.verify("plain-1", p1, p1rev, "supported", flow)
	f.deliver()
	got = f.wecom.received()
	lines = strings.Split(got[0]["markdown"].(map[string]any)["content"].(string), "\n")
	if lines[0] != "**【新确认漏洞】无资产候选**" || lines[len(lines)-1] != "[在平台查看](https://wscyber.example.test/?page=vulns&vuln=finding%3A"+p1+")" {
		t.Fatalf("plain lines = %q", lines)
	}
	// 漏洞卡取不到标题（键不存在）时退回修订标题。
	if title := f.capability.notifyVulnTitle(t.Context(), "no-such-key"); title != "" {
		t.Fatalf("missing key title = %q", title)
	}
}

func TestVulnCardManualNotify(t *testing.T) {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/credentials/c1", `{"label":"普通用户","username":"demo-user","secret":"Fixture-Pass-9"}`); status != 200 {
		t.Fatalf("credential = %d %s", status, raw)
	}
	f.launchManual("a1", "t1")
	fid, frev := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))
	v := f.newVerification("t1", fid, frev)
	step := f.addEvidence("t1", v.SessionID, cardFlow("https://api.example.test/orders/1002", "sid=fixture-session-abcdef", `{"owner":"user-42"}`))
	payload := reproPayload(step.ID)
	payload["key_steps"] = []map[string]any{{"title": "读取他人订单", "evidence_id": step.ID, "response_marks": []string{`"owner":"user-42"`}}}
	if result, message := f.callVerification(v, payload); result == nil {
		t.Fatalf("verification rejected: %s", message)
	}
	// 更新的旧 supported（无 key_steps）：人工推送仍选与卡片 repro 同一条。
	f.legacyVerify("t1", fid, frev)
	vid := vulnOf(f.vulns("a1", ""), fid).ID
	if card := f.card(vid); card.Repro == nil || card.Repro.VerificationID != v.VerificationID || card.Notify.Configured || len(card.Notify.Items) != 0 {
		t.Fatalf("card before = %+v", card.Notify)
	}

	// 未配置：推送 409 not_configured；预览照常（无 base_url 不带链接）。
	if status, value, raw := f.cardNotify(vid, ""); status != 409 || value["code"] != "not_configured" {
		t.Fatalf("unconfigured = %d %s", status, raw)
	}
	status, preview, raw := f.notifyPreview(vid)
	if status != 200 || preview.VerificationID != v.VerificationID || !strings.HasPrefix(preview.Markdown, "**【漏洞通报】订单接口越权读取**") ||
		strings.Contains(preview.Markdown, "在平台查看") || preview.Image == nil || preview.Image.EvidenceID != step.ID || preview.Image.TaskID != "t1" ||
		len(preview.Image.Marks) != 1 || preview.Image.Marks[0] != `"owner":"user-42"` || preview.ImageNote != "" {
		t.Fatalf("preview = %d %s", status, raw)
	}

	// 启用且要求确认：人工推送仍直接 queued、立即到期。
	f.enable(true, "https://wscyber.example.test")
	_, preview, _ = f.notifyPreview(vid)
	status, value, raw := f.cardNotify(vid, "{}")
	if status != 201 || value["kind"] != NotifyKindManual || value["status"] != NotifyQueued || value["dedupe_key"] != vid || value["verification_id"] != v.VerificationID {
		t.Fatalf("manual = %d %s", status, raw)
	}
	first := value["id"].(string)
	if item := f.notificationByID(first); !item.NextAttemptAt.Valid || item.NextAttemptAt.String != notifyTime(f.now) || item.VulnID != vid {
		t.Fatalf("manual row = %+v", item)
	}
	// 进行中：409 in_flight 并带 notification_id；不登记审计。
	if status, value, raw := f.cardNotify(vid, ""); status != 409 || value["code"] != "in_flight" || value["notification_id"] != first {
		t.Fatalf("in flight = %d %s", status, raw)
	}
	if audits := vulnNotifyAudits(t, f); len(audits) != 1 || audits[0].TargetKind != "vuln" || audits[0].TargetID != vid || audits[0].Actor != auditActorUser ||
		!strings.Contains(audits[0].DetailJSON, `"notification_id":"`+first+`"`) || !strings.Contains(audits[0].DetailJSON, `"kind":"manual"`) ||
		!strings.Contains(audits[0].DetailJSON, `"verification_id":"`+v.VerificationID+`"`) || !strings.Contains(audits[0].DetailJSON, `"vuln_key":"`+vid+`"`) {
		t.Fatalf("audits = %+v", audits)
	}
	// 投递：发送内容与预览逐字一致。
	f.deliver()
	got := f.wecom.received()
	if len(got) != 2 || got[1]["msgtype"] != "image" {
		t.Fatalf("sent = %+v", got)
	}
	if sent := got[0]["markdown"].(map[string]any)["content"].(string); sent != preview.Markdown || !strings.Contains(sent, "[在平台查看](https://wscyber.example.test/?page=vulns&vuln="+vid+")") {
		t.Fatalf("sent markdown =\n%s\npreview\n%s", sent, preview.Markdown)
	}
	if item := f.notificationByID(first); item.Status != NotifySent {
		t.Fatalf("after deliver = %+v", item)
	}
	// 终态后可再推：复用同一行重新入队并再次发送。
	f.advance(time.Minute)
	status, value, raw = f.cardNotify(vid, "")
	if status != 201 || value["id"] != first || value["status"] != NotifyQueued || value["sent_at"] != nil || value["attempts"].(float64) != 0 {
		t.Fatalf("again = %d %s", status, raw)
	}
	f.wecom.reset()
	f.deliver()
	if len(f.wecom.received()) != 2 || f.notificationByID(first).Status != NotifySent {
		t.Fatalf("second delivery = %+v", f.wecom.received())
	}
	if audits := vulnNotifyAudits(t, f); len(audits) != 2 {
		t.Fatalf("audits after re-push = %d", len(audits))
	}
	card := f.card(vid)
	if !card.Notify.Configured || !card.Notify.RequireConfirm || len(card.Notify.Items) != 1 || card.Notify.Items[0].ID != first || card.Notify.Items[0].Kind != NotifyKindManual {
		t.Fatalf("card notify = %+v", card.Notify)
	}
	manualEvents := 0
	for _, event := range card.Lifecycle {
		if event.Kind == "notify" && event.Title == "企业微信人工推送成功" {
			manualEvents++
		}
	}
	if manualEvents != 2 {
		t.Fatalf("lifecycle manual events = %d: %+v", manualEvents, card.Lifecycle)
	}
	// 未知字段 400；键非法 400；不存在 404。
	if status, _, _ := f.cardNotify(vid, `{"force":true}`); status != 400 {
		t.Fatalf("unknown field = %d", status)
	}
	if status, _, _ := f.cardNotify("a%25b", ""); status != 400 {
		t.Fatalf("bad key = %d", status)
	}
	if status, _, _ := f.cardNotify("no-such", ""); status != 404 {
		t.Fatalf("missing = %d", status)
	}
	if status, _, _ := f.notifyPreview("no-such"); status != 404 {
		t.Fatalf("preview missing = %d", status)
	}

	// 自动通知待确认（pending_confirm）也算进行中；忽略后可人工推送。
	f2, f2rev := f.addFinding("t1", "导出接口未鉴权", notifyContent(), evidenceAt("GET", "https://api.example.test/export"))
	f.verify("t1", f2, f2rev, "supported", notifyFlow("GET", "https://api.example.test/export", "", "{}"))
	key2 := vulnOf(f.vulns("a1", ""), f2).ID
	var pending string
	for _, item := range f.notifications() {
		if item.DedupeKey == key2 {
			pending = item.ID
		}
	}
	if status, value, raw := f.cardNotify(key2, ""); status != 409 || value["code"] != "in_flight" || value["notification_id"] != pending || pending == "" {
		t.Fatalf("pending in flight = %d %s", status, raw)
	}
	if status, _, raw := f.do("POST", "/api/webapi/notifications/"+pending+"/dismiss", ""); status != 200 {
		t.Fatalf("dismiss = %d %s", status, raw)
	}
	if status, _, raw := f.cardNotify(key2, ""); status != 201 {
		t.Fatalf("after dismiss = %d %s", status, raw)
	}
	// 误报：409 false_positive。
	if status, _, raw := f.do("PUT", "/api/webapi/vulns/"+vid+"/triage", `{"disposition":"false_positive"}`); status != 200 {
		t.Fatalf("triage = %d %s", status, raw)
	}
	if status, value, raw := f.cardNotify(vid, ""); status != 409 || value["code"] != "false_positive" {
		t.Fatalf("false positive = %d %s", status, raw)
	}
	// 无 supported：推送与预览都 409 no_supported。
	f3, f3rev := f.addFinding("t1", "搜索接口注入", notifyContent(), evidenceAt("GET", "https://api.example.test/search"))
	f.verify("t1", f3, f3rev, "counterevidence", notifyFlow("GET", "https://api.example.test/search", "", "{}"))
	key3 := vulnOf(f.vulns("a1", ""), f3).ID
	if status, value, raw := f.cardNotify(key3, ""); status != 409 || value["code"] != "no_supported" {
		t.Fatalf("no supported = %d %s", status, raw)
	}
	if status, _, raw := f.notifyPreview(key3); status != 409 || !strings.Contains(raw, `"code":"no_supported"`) {
		t.Fatalf("preview no supported = %d %s", status, raw)
	}
	// 拒绝不登记审计：只有 3 次成功。
	if audits := vulnNotifyAudits(t, f); len(audits) != 3 {
		t.Fatalf("vuln.notify audits = %d", len(audits))
	}
	// 未启用：not_configured。
	f.configure(fmt.Sprintf(`{"webhook":%q,"enabled":false}`, f.wecom.webhook()))
	if status, value, _ := f.cardNotify(key2, ""); status != 409 || value["code"] != "not_configured" {
		t.Fatalf("disabled = %d %+v", status, value)
	}
}

// 人工推送不破坏 new/regression 去重：manual 行不占 (dedupe_key, new|regression, verification_id)；fixed 后 supported 照常登记 regression。
func TestVulnCardManualNotifyKeepsDedupe(t *testing.T) {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	f.launchManual("a1", "t1")
	f.enable(false, "")
	flow := notifyFlow("GET", "https://api.example.test/orders/1002", "", `{"order":"1002"}`)
	fid, frev := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))
	v1 := f.verify("t1", fid, frev, "supported", flow)
	vid := vulnOf(f.vulns("a1", ""), fid).ID
	f.deliver()
	if status, value, raw := f.cardNotify(vid, ""); status != 201 || value["verification_id"] != v1.VerificationID {
		t.Fatalf("manual = %d %s", status, raw)
	}
	f.deliver()
	// 同一验证再次触发登记：new 不重复（已有通知）。
	if err := f.capability.registerVerificationNotification(t.Context(), v1.VerificationID); err != nil {
		t.Fatal(err)
	}
	f.addRetest("t1", fid, frev, "fixed_in_scope")
	time.Sleep(2 * time.Millisecond)
	vr := f.verify("t1", fid, frev, "supported", flow)
	kinds := map[string]int{}
	for _, item := range f.notifications() {
		kinds[item.Kind]++
		if item.Kind == NotifyKindRegression && item.VerificationID != vr.VerificationID {
			t.Fatalf("regression = %+v", item)
		}
	}
	if kinds[NotifyKindNew] != 1 || kinds[NotifyKindManual] != 1 || kinds[NotifyKindRegression] != 1 {
		t.Fatalf("kinds = %+v", kinds)
	}
	f.deliver()
	// regression 消息的标题前缀不变。
	last := ""
	for _, body := range f.wecom.received() {
		if body["msgtype"] == "markdown" {
			last = body["markdown"].(map[string]any)["content"].(string)
		}
	}
	if !strings.HasPrefix(last, "**【漏洞复现】") {
		t.Fatalf("regression markdown = %s", last)
	}
}

// 卡片 notify 段：该键最近 5 条（任何 kind/状态），新到旧。
func TestVulnCardNotifySectionLatestFive(t *testing.T) {
	f := newNotifyFixture(t)
	f.createAsset("a1")
	f.launchManual("a1", "t1")
	fid, frev := f.addFinding("t1", "订单接口越权读取", notifyContent(), evidenceAt("GET", "https://api.example.test/orders/1002"))
	v := f.verify("t1", fid, frev, "supported", notifyFlow("GET", "https://api.example.test/orders/1002", "", "{}"))
	vid := vulnOf(f.vulns("a1", ""), fid).ID
	ids := []string{}
	statuses := []string{NotifySent, NotifyFailed, NotifyDismissed, NotifyPendingConfirm, NotifySent, NotifyQueued}
	for index, status := range statuses {
		kind := []string{NotifyKindNew, NotifyKindManual, NotifyKindRegression}[index%3]
		model := notificationModel{ID: fmt.Sprintf("wn_fixture_%d", index), DedupeKey: vid, Kind: kind, TaskID: "t1", FindingID: fid, VerificationID: fmt.Sprintf("%s-%d", v.VerificationID, index),
			VulnID: vid, Status: status, CreatedAt: notifyTime(f.now.Add(time.Duration(index) * time.Minute))}
		if _, err := f.capability.store.orm.NewInsert().Model(&model).Exec(t.Context()); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, model.ID)
	}
	card := f.card(vid)
	if len(card.Notify.Items) != 5 {
		t.Fatalf("items = %d", len(card.Notify.Items))
	}
	for index, item := range card.Notify.Items {
		if item.ID != ids[len(ids)-1-index] {
			t.Fatalf("order[%d] = %s want %s", index, item.ID, ids[len(ids)-1-index])
		}
	}
}
