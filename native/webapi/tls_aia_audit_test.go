package webapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// 补链审计与预算测试：合成 PKI + 本地 HTTPS/AIA 替身，不访问外网。

type aiaRecorder struct {
	mu      sync.Mutex
	entries []auditEntry
	got     chan struct{}
}

func newAIARecorder() *aiaRecorder { return &aiaRecorder{got: make(chan struct{}, 16)} }

func (r *aiaRecorder) record(entry auditEntry, _ time.Time) {
	r.mu.Lock()
	r.entries = append(r.entries, entry)
	r.mu.Unlock()
	r.got <- struct{}{}
}

func (r *aiaRecorder) list() []auditEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]auditEntry(nil), r.entries...)
}

func (r *aiaRecorder) trace(parent context.Context) *tlsSendTrace {
	return &tlsSendTrace{taskID: "task-aia", sessionID: "sess-aia", parent: parent, record: r.record}
}

func getWithContext(ctx context.Context, client *http.Client, rawURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	return nil
}

// assertAIAEntry 校验单条 tls.aia_fetch 的固定字段，且 summary/detail 不含 URL 路径。
func assertAIAEntry(t *testing.T, entry auditEntry, aiaHost, targetHost, result string, paths ...string) {
	t.Helper()
	if entry.Actor != auditActorSystem || entry.Action != "tls.aia_fetch" || entry.TargetKind != "host" || entry.TargetID != aiaHost {
		t.Fatalf("entry = %+v", entry)
	}
	d := entry.Detail
	if d["aia_host"] != aiaHost || d["target_host"] != targetHost || d["result"] != result {
		t.Fatalf("detail = %+v, want aia_host=%s target_host=%s result=%s", d, aiaHost, targetHost, result)
	}
	if _, ok := d["redirects"]; !ok {
		t.Fatalf("detail missing redirects: %+v", d)
	}
	if _, ok := d["bytes"]; ok != (result == aiaResultOK) {
		t.Fatalf("bytes presence wrong for %s: %+v", result, d)
	}
	text := entry.Summary + " " + strings.Join(func() []string {
		var values []string
		for _, v := range d {
			if s, ok := v.(string); ok {
				values = append(values, s)
			}
		}
		return values
	}(), " ")
	for _, path := range append(paths, "http://", ".der") {
		if strings.Contains(text, path) {
			t.Fatalf("audit leaked %q: %s", path, text)
		}
	}
}

func TestAIAAuditSuccessAndCache(t *testing.T) {
	c := newSynthChain(t)
	leaf := c.leaf(t, leafOptions{dns: []string{"example.test"}})
	addr := startLeafOnlyServer(t, leaf.key, leaf.cert)
	v := newTestVerifier(c, c.stub())
	rec := newAIARecorder()
	ctx := withTLSTrace(t.Context(), rec.trace(t.Context()))
	if err := getWithContext(ctx, clientFor(v, "example.test", addr), "https://example.test/"); err != nil {
		t.Fatal(err)
	}
	entries := rec.list()
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	for _, entry := range entries {
		assertAIAEntry(t, entry, "aia.example.test", "example.test", aiaResultOK, "/inter2", "/inter1")
		if entry.TaskID != "task-aia" || entry.Detail["session_id"] != "sess-aia" || entry.Detail["bytes"].(int) <= 0 {
			t.Fatalf("entry = %+v", entry)
		}
		if !strings.Contains(entry.Summary, "证书补链：获取 aia.example.test 的中间证书（成功）") {
			t.Fatalf("summary = %q", entry.Summary)
		}
	}
	// 第二次走缓存：没有出网，不登记。
	if err := getWithContext(ctx, clientFor(v, "example.test", addr), "https://example.test/"); err != nil || len(rec.list()) != 2 {
		t.Fatalf("cache hit recorded: err=%v entries=%d", err, len(rec.list()))
	}
}

func TestAIAAuditNoTraceNoRecord(t *testing.T) {
	c := newSynthChain(t)
	leaf := c.leaf(t, leafOptions{dns: []string{"example.test"}})
	addr := startLeafOnlyServer(t, leaf.key, leaf.cert)
	// 无 trace：与生产校验路径相同，正常补链。
	if err := getVia(clientFor(newTestVerifier(c, c.stub()), "example.test", addr), "https://example.test/"); err != nil {
		t.Fatal(err)
	}
	// trace 无登记函数（其它调用方）：同样不 panic。
	ctx := withTLSTrace(t.Context(), &tlsSendTrace{taskID: "x"})
	if err := getWithContext(ctx, clientFor(newTestVerifier(c, c.stub()), "example.test", addr), "https://example.test/"); err != nil {
		t.Fatal(err)
	}
}

// 本地 AIA 发布点替身（明文 HTTP）：路由到固定响应；只给测试专用客户端（无私网拦截）使用。
func startAIAPublisher(t *testing.T, c synthChain) (*httptest.Server, *atomicCounter) {
	t.Helper()
	hits := &atomicCounter{}
	mux := http.NewServeMux()
	mux.HandleFunc("/inter2.der", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(c.inter2.cert.Raw) })
	mux.HandleFunc("/junk.der", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "junk") })
	mux.HandleFunc("/large.der", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(make([]byte, aiaMaxBytes+10)) })
	mux.HandleFunc("/r1", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/r2", http.StatusFound) })
	mux.HandleFunc("/r2", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/inter2.der", http.StatusFound) })
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/loop", http.StatusFound) })
	mux.HandleFunc("/to-https", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://aia.example.test/x", http.StatusFound)
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.add()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server, hits
}

type atomicCounter struct {
	mu sync.Mutex
	n  int
}

func (c *atomicCounter) add() { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *atomicCounter) get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

func TestAIAFetchResultsAndRedirects(t *testing.T) {
	c := newSynthChain(t)
	publisher, _ := startAIAPublisher(t, c)
	fetch := aiaFetcher(newAIAClient(nil))
	if data, redirects, err := fetch(t.Context(), publisher.URL+"/r1"); err != nil || redirects != 2 || parseAIACertificate(data) == nil {
		t.Fatalf("redirected fetch: redirects=%d err=%v", redirects, err)
	}
	cases := map[string]string{
		"/missing.der": aiaResultHTTPStatus, "/large.der": aiaResultTooLarge, "/loop": aiaResultHTTPStatus, "/to-https": aiaResultUnsupportedURL,
	}
	for path, want := range cases {
		_, redirects, err := fetch(t.Context(), publisher.URL+path)
		if err == nil || aiaFetchResult(t.Context(), err) != want {
			t.Errorf("%s: err=%v result=%s, want %s", path, err, aiaFetchResult(t.Context(), err), want)
		}
		if path == "/loop" && redirects != 2 { // len(via) >= 3 拒绝：最多跟随 2 次
			t.Errorf("loop redirects = %d, want 2", redirects)
		}
	}
}

func TestAIAAuditFailures(t *testing.T) {
	c := newSynthChain(t)
	publisher, hits := startAIAPublisher(t, c)
	local := strings.TrimPrefix(publisher.URL, "http://")
	testFetch, productionFetch := aiaFetcher(newAIAClient(nil)), aiaFetcher(aiaClient)
	cases := []struct {
		name, aia, aiaHost, want string
		fetch                    aiaFetchFunc
	}{
		{"http_404", publisher.URL + "/missing.der", local, aiaResultHTTPStatus, testFetch},
		{"parse_failed", publisher.URL + "/junk.der", local, aiaResultParseFailed, testFetch},
		{"too_large", publisher.URL + "/large.der", local, aiaResultTooLarge, testFetch},
		// 生产客户端：环回发布点在连接时被拒，不会到达替身。
		{"refused_address", publisher.URL + "/inter2.der", local, aiaResultRefusedAddress, productionFetch},
		{"unsupported_url", "ldap://aia.example.test/ca?cn=x", "aia.example.test", aiaResultUnsupportedURL, productionFetch},
		{"credentials_url", "http://user:pw@aia.example.test/ca.der", "aia.example.test", aiaResultUnsupportedURL, productionFetch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := hits.get()
			leaf := c.leaf(t, leafOptions{dns: []string{"example.test"}, aia: []string{tc.aia}})
			addr := startLeafOnlyServer(t, leaf.key, leaf.cert)
			v := &tlsChainVerifier{roots: c.roots, fetch: tc.fetch, now: time.Now, dialer: directDialer}
			rec := newAIARecorder()
			trace := rec.trace(t.Context())
			err := getWithContext(withTLSTrace(t.Context(), trace), clientFor(v, "example.test", addr), "https://example.test/")
			if err == nil {
				t.Fatal("verification unexpectedly passed")
			}
			if reason := httpFailureReason(trace.classify(err), false); !strings.Contains(reason, "tls_certificate：TLS证书校验失败") {
				t.Fatalf("classification = %q", reason)
			}
			entries := rec.list()
			if len(entries) != 1 {
				t.Fatalf("entries = %+v", entries)
			}
			assertAIAEntry(t, entries[0], tc.aiaHost, "example.test", tc.want, "/missing", "/junk", "/large", "?cn", "user:pw", "pw@")
			if !strings.Contains(entries[0].Summary, "失败：") {
				t.Fatalf("summary = %q", entries[0].Summary)
			}
			if tc.name == "refused_address" && hits.get() != before {
				t.Fatal("production AIA client reached loopback publisher")
			}
		})
	}
}

func TestTLSChainBudgetConstants(t *testing.T) {
	if aiaFetchTimeout > aiaTotalBudget || directDialTimeout+aiaTotalBudget > tlsDialBudget || tlsDialBudget+10*time.Second > httpTimeout ||
		directDialer.Timeout != directDialTimeout || directClient.Timeout != httpTimeout {
		t.Fatal("dial + handshake + AIA budget must leave response headroom within httpTimeout")
	}
}

func hangingAIALeaf(t *testing.T, c synthChain) string {
	leaf := c.leaf(t, leafOptions{dns: []string{"example.test"}, aia: []string{
		"http://aia.example.test/a.der", "http://aia.example.test/b.der", "http://aia.example.test/c.der"}})
	return startLeafOnlyServer(t, leaf.key, leaf.cert)
}

// 补链预算耗尽：握手在预算内结束，verifier 自身返回 tls_chain_incomplete（有无 trace 均如此）。
func TestTLSChainAIABudgetExhausted(t *testing.T) {
	c := newSynthChain(t)
	addr := hangingAIALeaf(t, c)
	for _, traced := range []bool{true, false} {
		stub := &aiaStub{hang: true}
		v := newTestVerifier(c, stub)
		v.aiaBudget, v.fetchTimeout = 300*time.Millisecond, 200*time.Millisecond
		rec := newAIARecorder()
		ctx := t.Context()
		var trace *tlsSendTrace
		if traced {
			trace = rec.trace(ctx)
			ctx = withTLSTrace(ctx, trace)
		}
		start := time.Now()
		err := getWithContext(ctx, clientFor(v, "example.test", addr), "https://example.test/")
		elapsed := time.Since(start)
		if err == nil || elapsed > 2*time.Second || elapsed >= httpTimeout {
			t.Fatalf("traced=%v err=%v elapsed=%s", traced, err, elapsed)
		}
		if reason := httpFailureReason(trace.classify(err), false); !strings.Contains(reason, "tls_chain_incomplete：证书链补全未完成") || strings.Contains(reason, "timeout：") {
			t.Fatalf("traced=%v classification = %q", traced, reason)
		}
		if got := stub.fetches.Load(); got < 1 || got > 2 {
			t.Fatalf("fetches = %d, want 1..2 within budget", got)
		}
		if traced {
			entries := rec.list()
			if len(entries) != int(stub.fetches.Load()) {
				t.Fatalf("entries = %d, fetches = %d", len(entries), stub.fetches.Load())
			}
			for _, entry := range entries {
				assertAIAEntry(t, entry, "aia.example.test", "example.test", aiaResultTimeout, "/a.der", "/b.der")
			}
		}
	}
}

// DialTLS 自身预算比补链预算紧：verifier 留余量先返回 tls_chain_incomplete，而不是握手上下文的超时。
func TestTLSChainAIADialBudgetMargin(t *testing.T) {
	c := newSynthChain(t)
	addr := hangingAIALeaf(t, c)
	v := newTestVerifier(c, &aiaStub{hang: true})
	v.dialBudget = 1500 * time.Millisecond
	err := getVia(clientFor(v, "example.test", addr), "https://example.test/")
	if reason := httpFailureReason(err, false); !strings.Contains(reason, "tls_chain_incomplete") {
		t.Fatalf("classification = %q", reason)
	}
}

// 外层（本次发送）截止发生在补链期间：http.Client 只返回自身超时，靠 trace 关联为 tls_chain_incomplete；
// 拨号上下文接回截止，补取随之中止（不在请求放弃后继续出网）。
func TestTLSChainOuterDeadlineDuringAIA(t *testing.T) {
	c := newSynthChain(t)
	addr := hangingAIALeaf(t, c)
	v := newTestVerifier(c, &aiaStub{hang: true}) // 默认 8s 补链预算
	parent, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	rec := newAIARecorder()
	trace := rec.trace(parent)
	start := time.Now()
	err := getWithContext(withTLSTrace(parent, trace), clientFor(v, "example.test", addr), "https://example.test/")
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v elapsed=%s", err, time.Since(start))
	}
	if reason := httpFailureReason(err, false); !strings.Contains(reason, "timeout：超时") {
		t.Fatalf("raw client error should be a plain timeout, got %q", reason)
	}
	if reason := httpFailureReason(trace.classify(err), false); !strings.Contains(reason, "tls_chain_incomplete：证书链补全未完成") {
		t.Fatalf("classification = %q", reason)
	}
	select {
	case <-rec.got:
	case <-time.After(2 * time.Second):
		t.Fatal("AIA fetch kept running after the send deadline")
	}
	if entries := rec.list(); len(entries) != 1 || entries[0].Detail["result"] != aiaResultTimeout {
		t.Fatalf("entries = %+v", entries)
	}
	// 人为取消（context.Canceled）不改写为补链未完成。
	canceledTrace := &tlsSendTrace{phase: aiaRunning}
	if reason := httpFailureReason(canceledTrace.classify(context.Canceled), false); !strings.Contains(reason, "context_canceled") {
		t.Fatalf("canceled = %q", reason)
	}
	// 未进入补链时的超时仍是 timeout。
	if reason := httpFailureReason((&tlsSendTrace{}).classify(context.DeadlineExceeded), false); !strings.Contains(reason, "timeout：超时") {
		t.Fatalf("idle timeout = %q", reason)
	}
}
