package webapi

import (
	"crypto/x509"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/chainreactors/cyber/core/operation"
)

// swapDirectTLS 让生产 directTransport 的 verifier 临时信任合成根、使用本地 AIA 替身；清空并恢复缓存。
func swapDirectTLS(t *testing.T, roots *x509.CertPool, fetch aiaFetchFunc, aiaBudget, fetchTimeout time.Duration) {
	t.Helper()
	savedRoots, savedFetch, savedBudget, savedFetchTimeout := directTLS.roots, directTLS.fetch, directTLS.aiaBudget, directTLS.fetchTimeout
	clearCache := func() {
		directTLS.cacheMu.Lock()
		directTLS.cache = nil
		directTLS.cacheMu.Unlock()
	}
	clearCache()
	directTLS.roots, directTLS.fetch, directTLS.aiaBudget, directTLS.fetchTimeout = roots, fetch, aiaBudget, fetchTimeout
	t.Cleanup(func() {
		directTLS.roots, directTLS.fetch, directTLS.aiaBudget, directTLS.fetchTimeout = savedRoots, savedFetch, savedBudget, savedFetchTimeout
		clearCache()
	})
}

// newLeafOnlyHTTPTask：leaf-only HTTPS 替身（127.0.0.1 IP SAN）+ 以其为 origin 的任务。
func newLeafOnlyHTTPTask(t *testing.T, c synthChain) (*Capability, *httpTool, *Task, operation.Invocation, string) {
	t.Helper()
	leaf := c.leaf(t, leafOptions{ip: []net.IP{net.ParseIP("127.0.0.1")}})
	addr := startLeafOnlyServer(t, leaf.key, leaf.cert)
	database := newTestHub(t)
	capability, err := NewCapability(database.URL, database.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = capability.Close(t.Context()) })
	outcome, err := capability.store.Save(t.Context(), "task-tls", "本机 leaf-only fixture", "验证补链审计", []string{"https://" + addr})
	if err != nil {
		t.Fatal(err)
	}
	inv := operation.Invocation{SessionID: outcome.Task.InitialSessionID, TurnID: "turn-1", CallID: "call-1"}
	return capability, &httpTool{store: capability.store, secrets: capability.secrets}, outcome.Task, inv, addr
}

func TestWebAPIHTTPAIAAuditEndToEnd(t *testing.T) {
	c := newSynthChain(t)
	swapDirectTLS(t, c.roots, c.stub().fetch, 0, 0)
	capability, tool, task, inv, addr := newLeafOnlyHTTPTask(t, c)
	result, item := executeHTTP(t, tool, inv, "GET", "https://"+addr+"/orders/1", "", "")
	if result.IsError || item.Status != requestCompleted || item.HTTPStatus != 200 {
		t.Fatalf("leaf-only target not completed via AIA: %+v", item)
	}
	rows := auditByAction(auditRows(t, capability), "tls.aia_fetch")
	if len(rows) != 2 {
		t.Fatalf("tls.aia_fetch rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		detail := auditDetail(t, row)
		if row.TaskID != task.ID || row.Actor != auditActorSystem || row.TargetKind != "host" || row.TargetID != "aia.example.test" ||
			detail["target_host"] != "127.0.0.1" || detail["result"] != aiaResultOK || detail["session_id"] != inv.SessionID {
			t.Fatalf("row = %+v detail = %+v", row, detail)
		}
		if strings.Contains(row.DetailJSON+row.Summary, ".der") || strings.Contains(row.DetailJSON+row.Summary, "/orders") {
			t.Fatalf("audit leaked path: %+v", row)
		}
	}
}

func TestWebAPIHTTPAIAIncompleteEndToEnd(t *testing.T) {
	c := newSynthChain(t)
	swapDirectTLS(t, c.roots, (&aiaStub{hang: true}).fetch, 300*time.Millisecond, 200*time.Millisecond)
	capability, tool, task, inv, addr := newLeafOnlyHTTPTask(t, c)
	start := time.Now()
	result, item := executeHTTP(t, tool, inv, "GET", "https://"+addr+"/orders/1", "", "")
	if elapsed := time.Since(start); elapsed >= httpTimeout || elapsed > 5*time.Second {
		t.Fatalf("send took %s", elapsed)
	}
	if !result.IsError || item.Status != requestUnknown || item.Sent != nil {
		t.Fatalf("unknown semantics changed: %+v", item)
	}
	model, err := capability.store.evidenceByID(t.Context(), task.ID, item.EvidenceID)
	if err != nil || model == nil {
		t.Fatalf("missing evidence: %v", err)
	}
	want := "发送或接收响应头失败（tls_chain_incomplete：证书链补全未完成），目标结果未知；不自动重试"
	if model.Reason != want {
		t.Fatalf("reason = %q, want %q", model.Reason, want)
	}
	rows := auditByAction(auditRows(t, capability), "tls.aia_fetch")
	if len(rows) == 0 {
		t.Fatal("no tls.aia_fetch rows")
	}
	for _, row := range rows {
		if detail := auditDetail(t, row); row.TaskID != task.ID || detail["result"] != aiaResultTimeout {
			t.Fatalf("row = %+v", row)
		}
	}
}

// 一键重放（人触发）同样经 directClient：补链登记带重放的任务 id 与合成会话 id。
func TestVulnCardReplayAIAAudit(t *testing.T) {
	c := newSynthChain(t)
	swapDirectTLS(t, c.roots, c.stub().fetch, 0, 0)
	leaf := c.leaf(t, leafOptions{ip: []net.IP{net.ParseIP("127.0.0.1")}})
	addr := startLeafOnlyServer(t, leaf.key, leaf.cert)
	origin := "https://" + addr
	f := replayFixture(t, origin)
	key, _ := f.replayVuln("订单接口越权读取", replayStep{flow: replayOriginalFlow("GET", origin+"/orders/1002", addr, "", 200, "ok")})
	before := len(auditByAction(auditRows(t, f.capability), "tls.aia_fetch"))
	if status, _, raw := f.replay(key, `{"step":1}`); status != 200 {
		t.Fatalf("replay = %d %s", status, raw)
	}
	rows := auditByAction(auditRows(t, f.capability), "tls.aia_fetch")[before:]
	if len(rows) != 2 {
		t.Fatalf("replay tls.aia_fetch rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		if detail := auditDetail(t, row); row.TaskID != "t1" || row.AssetID != "a1" || detail["session_id"] != replaySessionID || detail["result"] != aiaResultOK {
			t.Fatalf("row = %+v detail = %+v", row, detail)
		}
	}
}
