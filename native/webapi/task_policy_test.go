package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
	"github.com/chainreactors/cyber/exts/guardrail"
	"google.golang.org/protobuf/proto"
)

func policyRequest(t *testing.T, c *Capability, method, id, body string, handler func(http.ResponseWriter, *http.Request)) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(method, "/api/webapi/tasks/"+id, strings.NewReader(body))
	req.SetPathValue("id", id)
	recorder := httptest.NewRecorder()
	handler(recorder, req)
	var value map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &value)
	return recorder.Code, value
}

func TestWebAPITaskPolicyPutGetDefaults(t *testing.T) {
	capability, _, task, _, _, _ := newHTTPToolFixture(t, http.NotFoundHandler())
	status, value := policyRequest(t, capability, "GET", task.ID, "", capability.handleGetTask)
	policy, _ := value["policy"].(map[string]any)
	if status != 200 || policy["scope"] != "origins" || policy["approval"] != "high_risk" || policy["is_default"] != true || policy["updated_at"] != nil {
		t.Fatalf("default policy = %d %v", status, value)
	}
	for _, body := range []string{`{"scope":"any","approval":"off"}`, `{"scope":"origins","approval":"maybe"}`, `{"scope":"origins"}`, `{"scope":"origins","approval":"off","extra":1}`, `not json`} {
		if status, _ := policyRequest(t, capability, "PUT", task.ID, body, capability.handlePutPolicy); status != 400 {
			t.Fatalf("invalid body %s accepted: %d", body, status)
		}
	}
	if status, _ := policyRequest(t, capability, "PUT", "missing-task", `{"scope":"origins","approval":"off"}`, capability.handlePutPolicy); status != 404 {
		t.Fatalf("unknown task policy saved: %d", status)
	}
	status, value = policyRequest(t, capability, "PUT", task.ID, `{"scope":"unrestricted","approval":"all"}`, capability.handlePutPolicy)
	policy, _ = value["policy"].(map[string]any)
	if status != 200 || policy["scope"] != "unrestricted" || policy["approval"] != "all" || policy["is_default"] != false || policy["updated_at"] == nil {
		t.Fatalf("put policy = %d %v", status, value)
	}
	if status, _ = policyRequest(t, capability, "PUT", task.ID, `{"scope":"origins","approval":"off"}`, capability.handlePutPolicy); status != 200 {
		t.Fatal("second put failed")
	}
	_, value = policyRequest(t, capability, "GET", task.ID, "", capability.handleGetTask)
	policy, _ = value["policy"].(map[string]any)
	if policy["scope"] != "origins" || policy["approval"] != "off" || policy["is_default"] != false {
		t.Fatalf("get after put = %v", value)
	}
	// 会话查询（列表入口）同样带策略。
	req := httptest.NewRequest("GET", "/api/webapi/tasks?session_id="+task.InitialSessionID, nil)
	recorder := httptest.NewRecorder()
	capability.handleGetTaskBySession(recorder, req)
	if !strings.Contains(recorder.Body.String(), `"approval":"off"`) {
		t.Fatalf("session lookup lacks policy: %s", recorder.Body.String())
	}
	var events []taskPolicyEventModel
	if err := capability.store.orm.NewSelect().Model(&events).Where("task_id = ?", task.ID).Order("created_at ASC").Scan(t.Context()); err != nil || len(events) != 2 || events[0].Approval != "all" || events[1].Approval != "off" {
		t.Fatalf("policy events = %+v %v", events, err)
	}
	// 映射不到任务的会话按默认值。
	if policy, task, err := capability.store.policyForSession(t.Context(), "unmapped"); err != nil || task != nil || !policy.IsDefault || policy.Approval != ApprovalHighRisk {
		t.Fatalf("unmapped session policy = %+v %v", policy, err)
	}
}

func TestWebAPIHTTPReviewDecisionMatrix(t *testing.T) {
	cases := []struct {
		method, path, body string
		want               map[string]bool // approval → 需审批
	}{
		{"GET", "/api/items/1", "", map[string]bool{ApprovalOff: false, ApprovalHighRisk: false, ApprovalAll: false}},
		{"HEAD", "/api/items", "", map[string]bool{ApprovalOff: false, ApprovalHighRisk: false, ApprovalAll: false}},
		{"POST", "/api/items", `{"name":"新物品"}`, map[string]bool{ApprovalOff: false, ApprovalHighRisk: false, ApprovalAll: true}},
		{"DELETE", "/api/items/1", "", map[string]bool{ApprovalOff: false, ApprovalHighRisk: true, ApprovalAll: true}},
		{"GET", "/api/items/1/remove", "", map[string]bool{ApprovalOff: false, ApprovalHighRisk: true, ApprovalAll: true}},
		{"POST", "/api/batch", `{"action":"purgeAll"}`, map[string]bool{ApprovalOff: false, ApprovalHighRisk: true, ApprovalAll: true}},
	}
	for _, c := range cases {
		for approval, want := range c.want {
			if got := httpReviewReason(approval, c.method, "http://example.test"+c.path, nil, []byte(c.body)) != ""; got != want {
				t.Errorf("%s %s approval=%s review=%v want %v", c.method, c.path, approval, got, want)
			}
		}
	}
}

// 三档审批 × 两种 scope 经真实工具执行（无审批扩展＝无人批准）。
func TestWebAPIHTTPPolicyScopeAndApprovalExecution(t *testing.T) {
	capability, tool, task, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	var outside int
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { outside++; _, _ = w.Write([]byte("outside")) }))
	defer other.Close()
	call := 0
	run := func(method, url string) toolEvidenceResult {
		call++
		inv.CallID = "matrix-" + string(rune('a'+call))
		_, value := executeHTTP(t, tool, inv, method, url, "", "")
		return value
	}
	for _, scope := range []string{ScopeOrigins, ScopeUnrestricted} {
		for _, approval := range []string{ApprovalOff, ApprovalHighRisk, ApprovalAll} {
			if _, err := capability.store.setPolicy(t.Context(), task.ID, scope, approval); err != nil {
				t.Fatal(err)
			}
			before, beforeOutside := count.Load(), outside
			if got := run("GET", other.URL+"/x"); scope == ScopeOrigins && (got.Status != "denied" || outside != beforeOutside) ||
				scope == ScopeUnrestricted && (got.Status != requestCompleted || outside != beforeOutside+1) {
				t.Fatalf("scope=%s approval=%s outside GET = %+v", scope, approval, got)
			}
			got := run("DELETE", task.Origins[0]+"/api/items/1")
			if approval == ApprovalOff && got.Status != requestCompleted || approval != ApprovalOff && (got.Status != "review_required" || got.Sent == nil || *got.Sent) {
				t.Fatalf("scope=%s approval=%s DELETE = %+v", scope, approval, got)
			}
			got = run("POST", task.Origins[0]+"/api/items")
			if approval == ApprovalAll && got.Status != "review_required" || approval != ApprovalAll && got.Status != requestCompleted {
				t.Fatalf("scope=%s approval=%s POST = %+v", scope, approval, got)
			}
			_ = before
		}
	}
}

func TestWebAPICommandRuleTable(t *testing.T) {
	risky := []string{
		"rm -rf /tmp/demo-not-exist", "rm -r build", "rm -f a.txt", "sudo rm --recursive x", "cd /tmp && rm -fr x",
		"mkfs.ext4 /dev/sdb1", "wipefs -a /dev/sdb", "dd if=/dev/zero of=/dev/sda bs=1M", "echo x > /dev/sda",
		"shutdown -h now", "reboot", "systemctl poweroff", "init 0",
		`mysql -e "DROP TABLE users"`, `psql -c 'truncate table logs'`, `sqlite3 a.db "delete from items"`,
		"curl -X DELETE http://example.test/api/items/1", "curl -XDELETE http://example.test/a", "curl --request=DELETE http://example.test/a",
		"wget --method=DELETE http://example.test/a", "curl http://example.test/api/items/1/remove",
		`bash -c "rm -rf /tmp/x"`, `sh -c 'mkfs /dev/sdb'`, "find . -name '*.log' -delete", "find . -exec rm -f {} ;",
		"xargs rm -rf", "env FOO=1 rm -rf x", "timeout 5 rm -rf x", `eval "rm -rf x"`,
		"x=rm; $x -rf a", "rm $FLAGS a", `sh -c "$CMD"`, "$(echo rm) -rf a", `sudo "$TOOL" a`,
	}
	for _, command := range risky {
		if commandReviewReason(command) == "" {
			t.Errorf("risky command not matched: %s", command)
		}
	}
	safe := []string{
		"ls -la", "rm a.txt", "cat /dev/null", "echo rm -rf", "grep -r delete .", "curl http://example.test/api/items",
		"curl -X GET http://example.test/api/items", "dd if=/dev/zero of=./disk.img bs=1M count=1", "find . -name x",
		`echo "DROP is a word"`, "systemctl status nginx", "printf 'reboot\\n'", "echo $HOME", "ls $DIR", `cat "$FILE"`,
	}
	for _, command := range safe {
		if reason := commandReviewReason(command); reason != "" {
			t.Errorf("safe command matched %q: %s", reason, command)
		}
	}
}

type approvalHarness struct {
	capability *Capability
	registry   *corehooks.Registry
	stream     *events.Stream
	mu         sync.Mutex
	reviews    []*guardrail.Review
}

func newApprovalHarness(t *testing.T, capability *Capability) *approvalHarness {
	t.Helper()
	h := &approvalHarness{capability: capability, registry: corehooks.New(), stream: events.New()}
	h.stream.Observe(func(event *aop.Event) {
		if event.Emitter != "guardrail" {
			return
		}
		var review guardrail.Review
		if event.GetExtension().UnmarshalTo(&review) == nil {
			h.mu.Lock()
			h.reviews = append(h.reviews, proto.Clone(&review).(*guardrail.Review))
			h.mu.Unlock()
		}
	})
	set, err := extension.New(extension.Provided[*corehooks.Registry](h.registry), extension.Provided[*events.Stream](h.stream), capability.ApprovalExtension())
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close(context.Background()) })
	return h
}

func (h *approvalHarness) states() []guardrail.ReviewState {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []guardrail.ReviewState
	for _, review := range h.reviews {
		out = append(out, review.State)
	}
	return out
}

// execute 走原生唯一工具边界 toolhooks.Execute（Before → 工具 → Completed）。
func (h *approvalHarness) execute(ctx context.Context, inv operation.Invocation, name, arguments string, run func(context.Context, string) (*aop.ToolResult, error)) (*aop.ToolResult, error) {
	return toolhooks.Execute(operation.ContextWithInvocation(ctx, inv), h.registry, name, arguments, run)
}

// waitPending 用 Pending/Resolve 协议处理器列出挂起审批。
func (h *approvalHarness) waitPending(t *testing.T, sessionID string) *guardrail.Review {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		answer := h.protocol(t, &guardrail.ProtocolMessage{Message: &guardrail.ProtocolMessage_Pending{Pending: &guardrail.PendingRequest{SessionId: sessionID}}})
		if reviews := answer.GetPendingResult().GetReviews(); len(reviews) > 0 {
			return reviews[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no pending review")
	return nil
}

func (h *approvalHarness) protocol(t *testing.T, message *guardrail.ProtocolMessage) *guardrail.ProtocolMessage {
	t.Helper()
	envelope, err := aop.Wrap("approval-test", "", message)
	if err != nil {
		t.Fatal(err)
	}
	var answer proto.Message
	if err := h.capability.handleApprovalProtocol(context.Background(), envelope, message, func(reply *aop.Envelope) error {
		answer, err = aop.Unwrap(reply)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if value, ok := answer.(*guardrail.ProtocolMessage); ok {
		return value
	}
	if failure, ok := answer.(*aop.ProtocolMessage); ok && failure.GetProtocolError() != nil {
		return &guardrail.ProtocolMessage{}
	}
	t.Fatalf("unexpected protocol answer %T", answer)
	return nil
}

func (h *approvalHarness) resolve(t *testing.T, sessionID, operationID string, approve bool) bool {
	answer := h.protocol(t, &guardrail.ProtocolMessage{Message: &guardrail.ProtocolMessage_Resolve{Resolve: &guardrail.ResolveRequest{SessionId: sessionID, OperationId: operationID, Approve: approve}}})
	return answer.GetResolved() != nil
}

func TestWebAPIApprovalHTTPApproveRejectExpireCancel(t *testing.T) {
	capability, tool, task, inv, count, _ := newHTTPToolFixture(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"deleted":true}`)) }))
	tool.approvals = capability.approvals
	h := newApprovalHarness(t, capability)
	args := `{"method":"DELETE","url":"` + task.Origins[0] + `/api/items/7"}`
	type outcome struct {
		result *aop.ToolResult
		err    error
	}
	start := func(ctx context.Context, callID string) chan outcome {
		inv.CallID = callID
		done := make(chan outcome, 1)
		local := inv
		go func() {
			result, err := h.execute(ctx, local, "webapi_http", args, tool.Execute)
			done <- outcome{result, err}
		}()
		return done
	}

	// 批准 → 请求发出，工具内不再二次拒绝。
	done := start(t.Context(), "approve-call")
	pending := h.waitPending(t, task.InitialSessionID)
	if pending.GetCall().GetName() != "webapi_http" || pending.GetDecision().GetAction() != guardrail.Action_ACTION_REVIEW || !strings.Contains(pending.GetDecision().GetReason(), "DELETE") || pending.GetExpiresAt() == nil {
		t.Fatalf("pending review = %v", pending)
	}
	if h.resolve(t, "other-session", pending.GetOperation().GetOperationId(), true) {
		t.Fatal("cross-session resolve accepted")
	}
	if !h.resolve(t, task.InitialSessionID, pending.GetOperation().GetOperationId(), true) {
		t.Fatal("resolve failed")
	}
	got := <-done
	if got.err != nil || !strings.Contains(coretool.ResultText(got.result), `"status":"completed"`) || count.Load() != 1 {
		t.Fatalf("approved request = %s err=%v count=%d", coretool.ResultText(got.result), got.err, count.Load())
	}
	if capability.approvals.isApproved(operation.Invocation{SessionID: inv.SessionID, TurnID: inv.TurnID, CallID: "approve-call"}) {
		t.Fatal("approval not released after tool completed")
	}

	// 拒绝 → 不执行，记 review_denied 未发送。
	done = start(t.Context(), "reject-call")
	pending = h.waitPending(t, task.InitialSessionID)
	h.resolve(t, task.InitialSessionID, pending.GetOperation().GetOperationId(), false)
	got = <-done
	if got.err == nil || !strings.Contains(got.err.Error(), "已拒绝") || count.Load() != 1 {
		t.Fatalf("rejected request = %v count=%d", got.err, count.Load())
	}

	// 超时（注入短时长）→ review_expired。
	capability.approvals.timeout = 50 * time.Millisecond
	done = start(t.Context(), "expire-call")
	got = <-done
	if got.err == nil || !strings.Contains(got.err.Error(), "超时") || count.Load() != 1 {
		t.Fatalf("expired request = %v", got.err)
	}
	capability.approvals.timeout = ReviewTimeout

	// 取消 → review_canceled。
	ctx, cancel := context.WithCancel(t.Context())
	done = start(ctx, "cancel-call")
	h.waitPending(t, task.InitialSessionID)
	cancel()
	got = <-done
	if got.err == nil || count.Load() != 1 {
		t.Fatalf("canceled request = %v", got.err)
	}

	items, err := capability.store.evidenceList(t.Context(), task.ID, 100, "", "")
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, item := range items {
		statuses[item.CallID] = item.Status
		if item.Status != requestCompleted && (!item.Sent.Valid || item.Sent.Bool || !strings.Contains(item.Reason, "人工审批")) {
			t.Fatalf("unsent record = %+v", item)
		}
	}
	if statuses["approve-call"] != requestCompleted || statuses["reject-call"] != requestReviewDenied || statuses["expire-call"] != requestReviewExpired || statuses["cancel-call"] != requestReviewCanceled {
		t.Fatalf("statuses = %v", statuses)
	}
	want := []guardrail.ReviewState{
		guardrail.ReviewState_REVIEW_STATE_PENDING, guardrail.ReviewState_REVIEW_STATE_APPROVED,
		guardrail.ReviewState_REVIEW_STATE_PENDING, guardrail.ReviewState_REVIEW_STATE_REJECTED,
		guardrail.ReviewState_REVIEW_STATE_PENDING, guardrail.ReviewState_REVIEW_STATE_EXPIRED,
		guardrail.ReviewState_REVIEW_STATE_PENDING, guardrail.ReviewState_REVIEW_STATE_CANCELED,
	}
	if got := h.states(); len(got) != len(want) {
		t.Fatalf("review events = %v", got)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("review events = %v", got)
			}
		}
	}
	h.mu.Lock()
	source := h.reviews[1].GetResolutionSource()
	h.mu.Unlock()
	if source != "control" {
		t.Fatalf("resolution source = %q", source)
	}
	// 被拒/超时的 review_* 不阻断后续模型调用（人已决定）。
	if blocked, err := capability.store.unresolvedTurn(t.Context(), inv.SessionID, inv.TurnID); err != nil || blocked {
		t.Fatalf("decided reviews block turn: %v %v", blocked, err)
	}
}

func TestWebAPIApprovalPolicyGatesBashAndWrite(t *testing.T) {
	capability, _, task, inv, _, _ := newHTTPToolFixture(t, http.NotFoundHandler())
	h := newApprovalHarness(t, capability)
	ran := 0
	run := func(context.Context, string) (*aop.ToolResult, error) { ran++; return coretool.TextResult("ok"), nil }
	exec := func(name, args, callID string) error {
		inv.CallID = callID
		_, err := h.execute(t.Context(), inv, name, args, run)
		return err
	}
	// 默认 high_risk：普通命令、写文件、只读工具直接放行。
	for i, call := range [][2]string{{"bash", `{"command":"ls -la"}`}, {"write", `{"path":"a.txt","content":"x"}`}, {"read", `{"path":"a.txt"}`}, {"webapi_finding", `{}`}} {
		if err := exec(call[0], call[1], "free-"+string(rune('a'+i))); err != nil {
			t.Fatalf("%s gated under high_risk: %v", call[0], err)
		}
	}
	// 高危命令挂起；拒绝 → 不执行。
	done := make(chan error, 1)
	go func() { done <- exec("bash", `{"command":"rm -rf /tmp/wscyber-demo-not-exist"}`, "rm-call") }()
	pending := h.waitPending(t, task.InitialSessionID)
	if !strings.Contains(pending.GetDecision().GetReason(), "递归/强制删除") {
		t.Fatalf("bash reason = %s", pending.GetDecision().GetReason())
	}
	h.resolve(t, task.InitialSessionID, pending.GetOperation().GetOperationId(), false)
	if err := <-done; err == nil || ran != 4 {
		t.Fatalf("rejected bash executed: %v ran=%d", err, ran)
	}
	// 命令边界兜底：动态拼出的高危命令未经批准直接拒绝，不等待。
	ctx := operation.ContextWithInvocation(t.Context(), operation.Invocation{SessionID: inv.SessionID, TurnID: inv.TurnID, CallID: "dynamic"})
	if admission, _ := capability.admitCommand(ctx, toolhooks.CommandEvent{Name: "rm", Args: []string{"-rf", "/tmp/x"}}); admission.Deny == nil {
		t.Fatal("dynamic rm -rf admitted without approval")
	}
	if admission, _ := capability.admitCommand(ctx, toolhooks.CommandEvent{Name: "ls", Args: []string{"-la"}}); admission.Deny != nil {
		t.Fatal("ls denied")
	}
	// approval=all：所有终端命令与写文件挂起；只读工具仍放行。
	if _, err := capability.store.setPolicy(t.Context(), task.ID, ScopeOrigins, ApprovalAll); err != nil {
		t.Fatal(err)
	}
	if err := exec("read", `{"path":"a.txt"}`, "all-read"); err != nil {
		t.Fatal(err)
	}
	for _, call := range [][2]string{{"bash", `{"command":"ls"}`}, {"write", `{"path":"a.txt","content":"x"}`}} {
		go func() { done <- exec(call[0], call[1], "all-"+call[0]) }()
		pending := h.waitPending(t, task.InitialSessionID)
		h.resolve(t, task.InitialSessionID, pending.GetOperation().GetOperationId(), true)
		if err := <-done; err != nil {
			t.Fatalf("approved %s failed: %v", call[0], err)
		}
	}
	// approval=off：高危命令直接执行（用户显式关闭）。
	if _, err := capability.store.setPolicy(t.Context(), task.ID, ScopeUnrestricted, ApprovalOff); err != nil {
		t.Fatal(err)
	}
	before := ran
	if err := exec("bash", `{"command":"rm -rf /tmp/wscyber-demo-not-exist"}`, "off-rm"); err != nil || ran != before+1 {
		t.Fatalf("approval=off still gated: %v", err)
	}
	if admission, _ := capability.admitCommand(ctx, toolhooks.CommandEvent{Name: "rm", Args: []string{"-rf", "/tmp/x"}}); admission.Deny != nil {
		t.Fatal("approval=off command boundary denied")
	}
}
