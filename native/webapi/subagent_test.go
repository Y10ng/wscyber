package webapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agenthooks "github.com/chainreactors/cyber/agent/hooks"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	types "github.com/chainreactors/cyber/core/types"
	"github.com/chainreactors/cyber/exts/guardrail"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

// 子 Agent（M5a）单元测试：全部用临时库、本机 httptest 目标与自编中性数据，直接走原生 tool.before / SessionStart / SessionEnd 钩子。

type subagentFixture struct {
	t      *testing.T
	cap    *Capability
	h      *approvalHarness
	task   *Task
	target *httptest.Server
	mux    *http.ServeMux
}

func newSubagentFixture(t *testing.T, oracles int) *subagentFixture {
	t.Helper()
	database := filepath.Join(t.TempDir(), "hub.sqlite")
	native, err := webservice.NewSQLiteStore(database, SchemaModule())
	if err != nil {
		t.Fatal(err)
	}
	_ = native.Close()
	capability, err := NewCapability(database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = capability.Close(context.Background()) })
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"order":"synthetic","path":"` + r.URL.Path + `"}`))
	}))
	t.Cleanup(target.Close)
	outcome, err := capability.store.Save(t.Context(), "task-sub", "演示订单 API（合成）", "检查越权", []string{originOf(t, target.URL)})
	if err != nil {
		t.Fatal(err)
	}
	var proposals []boardOracleProposal
	for index := 1; index <= oracles; index++ {
		proposal, err := validateOracleInput("items[0]", boardOracleInput{Question: fmt.Sprintf("普通用户能否读取他人订单 %d", index), Category: "authz",
			EndpointKeys: []string{fmt.Sprintf("GET %s/orders/%d", target.URL, index)}, Priority: "high"})
		if err != nil {
			t.Fatal(err)
		}
		proposals = append(proposals, proposal)
	}
	if len(proposals) > 0 {
		if _, err := capability.store.boardProposeOracles(t.Context(), outcome.Task.ID, "user", "", proposals, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	for _, route := range capability.Routes(nil) {
		mux.Handle(route.Pattern, route.Handler)
	}
	return &subagentFixture{t: t, cap: capability, h: newApprovalHarness(t, capability), task: outcome.Task, target: target, mux: mux}
}

func (f *subagentFixture) research(call string) operation.Invocation {
	return operation.Invocation{SessionID: f.task.InitialSessionID, TurnID: "turn-research", CallID: call}
}

// subagent 经原生 tool.before → 工具 → tool.completed；during 在“工具执行中”运行（模拟 sessionexec 打开子会话）。
func (f *subagentFixture) subagent(inv operation.Invocation, args string, during func()) (string, bool) {
	f.t.Helper()
	called := false
	result, err := f.h.execute(f.t.Context(), inv, "subagent", args, func(context.Context, string) (*aop.ToolResult, error) {
		called = true
		if during != nil {
			during()
		}
		return coretool.TextResult("started"), nil
	})
	return coretool.ResultText(result) + fmt.Sprint(err), called
}

func (f *subagentFixture) start(parent, call, child, agentType string) error {
	_, err := agenthooks.SessionStart.Emit(f.t.Context(), f.h.registry, agenthooks.SessionEvent{SessionID: child, ParentID: parent, ParentToolCallID: call,
		AgentName: "label", Delegation: &types.DelegationDetail{AgentType: agentType}})
	return err
}

func (f *subagentFixture) end(child string, stop agenthooks.StopReason, failure error) {
	f.t.Helper()
	if _, err := agenthooks.SessionEnd.Emit(f.t.Context(), f.h.registry, agenthooks.SessionEvent{SessionID: child, ParentID: f.task.InitialSessionID, ParentToolCallID: "call-" + child, Stop: stop, Err: failure}); err != nil {
		f.t.Fatal(err)
	}
}

// spawn 放行一个 oracle_explorer 并在工具执行中启动子会话。
func (f *subagentFixture) spawn(number, child string) {
	f.t.Helper()
	inv := f.research("call-" + child)
	var startErr error
	text, called := f.subagent(inv, fmt.Sprintf(`{"name":"oracle_explorer","prompt":"%s 只看订单接口"}`, number), func() {
		startErr = f.start(inv.SessionID, inv.CallID, child, OracleExplorerName)
	})
	if !called || startErr != nil {
		f.t.Fatalf("spawn %s: called=%v start=%v text=%s", number, called, startErr, text)
	}
}

func (f *subagentFixture) oracle(number int64) *BoardOracle {
	f.t.Helper()
	oracle, err := f.cap.store.boardOracle(f.t.Context(), f.task.ID, number)
	if err != nil || oracle == nil {
		f.t.Fatalf("oracle %d: %v", number, err)
	}
	return oracle
}

func (f *subagentFixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.cap.store.db.QueryRow(query, args...).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *subagentFixture) tool(tool coretool.Tool, inv operation.Invocation, arguments string) string {
	f.t.Helper()
	result, err := tool.Execute(operation.ContextWithInvocation(f.t.Context(), inv), arguments)
	if err != nil {
		f.t.Fatal(err)
	}
	return coretool.ResultText(result)
}

func TestSubagentAdmissionRejects(t *testing.T) {
	f := newSubagentFixture(t, 4)
	store := f.cap.store
	// O-002 终态、O-003 被其它会话认领。
	f.cap.store.db.Exec(`UPDATE webapi_board_oracles SET status = 'refuted' WHERE task_id = ? AND number = 2`, f.task.ID)
	f.cap.store.db.Exec(`UPDATE webapi_board_oracles SET status = 'in_progress', claimed_session_id = 'other-session' WHERE task_id = ? AND number = 3`, f.task.ID)
	cases := []struct {
		name, session, args, want string
	}{
		{"anonymous", f.task.InitialSessionID, `{"prompt":"O-001 看看"}`, "只开放命名子 Agent oracle_explorer"},
		{"other-name", f.task.InitialSessionID, `{"name":"verify","prompt":"O-001"}`, "只开放命名子 Agent oracle_explorer"},
		{"fork", f.task.InitialSessionID, `{"name":"oracle_explorer","mode":"fork","prompt":"O-001"}`, "fork 会带入父会话历史"},
		{"ordinary-chat", "ordinary-chat", `{"name":"oracle_explorer","prompt":"O-001"}`, "只有 Web/API 任务的研究会话可派出"},
		{"no-session", "", `{"name":"oracle_explorer","prompt":"O-001"}`, "缺少可信原生"},
		{"no-number", f.task.InitialSessionID, `{"name":"oracle_explorer","prompt":"看看订单"}`, "不是 Oracle 编号"},
		{"absent", f.task.InitialSessionID, `{"name":"oracle_explorer","prompt":"O-099 看看"}`, "本任务不存在 O-099"},
		{"terminal", f.task.InitialSessionID, `{"name":"oracle_explorer","prompt":"O-002"}`, "O-002 已是终态 refuted"},
		{"claimed", f.task.InitialSessionID, `{"name":"oracle_explorer","prompt":"O-003"}`, "O-003 已被会话 other-session 认领"},
		{"kill-foreign", f.task.InitialSessionID, `{"action":"kill","session_id":"not-a-subsession"}`, "不是本任务的子 Agent 会话"},
		{"kill-ordinary", "ordinary-chat", `{"action":"kill","session_id":"x"}`, "只有 Web/API 任务的研究会话可 kill"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			text, called := f.subagent(operation.Invocation{SessionID: c.session, TurnID: "turn", CallID: "call-" + c.name}, c.args, nil)
			if called || !strings.Contains(text, "已拒绝 subagent") || !strings.Contains(text, c.want) {
				t.Fatalf("called=%v text=%s want %q", called, text, c.want)
			}
		})
	}
	if n := f.count(`SELECT COUNT(*) FROM webapi_subagent_reservations`); n != 0 {
		t.Fatalf("rejection left %d reservations", n)
	}
	if o := f.oracle(1); o.Status != oracleOpen || len(o.Events) != 1 {
		t.Fatalf("rejection changed O-001: %+v", o)
	}
	for _, action := range []string{"catalog", "list"} {
		if text, called := f.subagent(f.research("call-"+action), `{"action":"`+action+`"}`, nil); !called {
			t.Fatalf("%s denied: %s", action, text)
		}
	}
	_ = store
}

func TestSubagentReserveStartEndAndLimit(t *testing.T) {
	f := newSubagentFixture(t, 5)
	inv := f.research("call-a")
	var during *BoardOracle
	var startErr error
	text, called := f.subagent(inv, `{"name":"oracle_explorer","mode":"async","label":"订单越权","prompt":"O-001 只看订单接口"}`, func() {
		during = f.oracle(1)
		// 同一 Oracle 预占中不可重复派出。
		if again, ok := f.subagent(f.research("call-dup"), `{"name":"oracle_explorer","prompt":"O-001"}`, nil); ok || !strings.Contains(again, "已有子 Agent 在探索") {
			t.Errorf("duplicate admitted: %s", again)
		}
		startErr = f.start(inv.SessionID, inv.CallID, "child-a", OracleExplorerName)
	})
	if !called || startErr != nil {
		t.Fatalf("allow: called=%v start=%v text=%s", called, startErr, text)
	}
	if during.Status != oracleInProgress || during.ClaimedSessionID != "" || during.Events[len(during.Events)-1].Summary != "子 Agent 认领" || during.Events[len(during.Events)-1].Actor != oracleActorAI {
		t.Fatalf("reservation oracle = %+v", during)
	}
	if o := f.oracle(1); o.ClaimedSessionID != "child-a" || o.Status != oracleInProgress {
		t.Fatalf("after start = %+v", o)
	}
	sub, err := f.cap.store.subsessionFor(t.Context(), "child-a")
	if err != nil || sub == nil || sub.Status != subsessionRunning || sub.TaskID != f.task.ID || sub.ParentCallID != "call-a" || sub.AgentName != OracleExplorerName {
		t.Fatalf("subsession = %+v %v", sub, err)
	}
	if n := f.count(`SELECT COUNT(*) FROM webapi_subagent_reservations`); n != 0 {
		t.Fatalf("reservation not consumed: %d", n)
	}
	// 子会话本身不能再派（不递归）。
	if text, called := f.subagent(operation.Invocation{SessionID: "child-a", TurnID: "t", CallID: "c"}, `{"name":"oracle_explorer","prompt":"O-002"}`, nil); called || !strings.Contains(text, "只有 Web/API 任务的研究会话可派出") {
		t.Fatalf("subsession spawned: %s", text)
	}
	// 研究会话可 kill 本任务子会话（真正取消由上游工具执行）。
	if text, called := f.subagent(f.research("call-kill"), `{"action":"kill","session_id":"child-a"}`, nil); !called {
		t.Fatalf("kill denied: %s", text)
	}
	f.spawn("O-002", "child-b")
	f.spawn("O-003", "child-c")
	if text, called := f.subagent(f.research("call-d"), `{"name":"oracle_explorer","prompt":"O-004"}`, nil); called || !strings.Contains(text, "已达上限 3 个") {
		t.Fatalf("limit not enforced: %s", text)
	}
	if o := f.oracle(4); o.Status != oracleOpen {
		t.Fatalf("limit rejection changed O-004: %+v", o)
	}
	// 结束未结论 → 退回 open。
	f.end("child-a", agenthooks.StopReasonCompleted, nil)
	o := f.oracle(1)
	if o.Status != oracleOpen || o.ClaimedSessionID != "" || o.Events[len(o.Events)-1].Summary != "子 Agent 结束未给结论（completed）" {
		t.Fatalf("release = %+v", o)
	}
	sub, _ = f.cap.store.subsessionFor(t.Context(), "child-a")
	if sub.Status != subsessionEnded || sub.StopReason != "completed" || !sub.EndedAt.Valid {
		t.Fatalf("ended row = %+v", sub)
	}
	// 出错结束记 error。
	f.end("child-b", agenthooks.StopReasonError, errors.New("synthetic provider failure"))
	sub, _ = f.cap.store.subsessionFor(t.Context(), "child-b")
	if sub.StopReason != "error" || sub.Error != "synthetic provider failure" || f.oracle(2).Status != oracleOpen {
		t.Fatalf("error end = %+v", sub)
	}
	// 释放后名额恢复；预占后未启动 → tool.completed 退回 open。
	text, called = f.subagent(f.research("call-e"), `{"name":"oracle_explorer","prompt":"O-004"}`, nil)
	if !called {
		t.Fatalf("after release denied: %s", text)
	}
	o = f.oracle(4)
	if o.Status != oracleOpen || o.Events[len(o.Events)-1].Summary != "子 Agent 未启动，退回 open" {
		t.Fatalf("unstarted reservation = %+v", o)
	}
	if n := f.count(`SELECT COUNT(*) FROM webapi_subagent_reservations`); n != 0 {
		t.Fatalf("unstarted reservation left: %d", n)
	}
	// 研究会话自己 claim 的 Oracle 仍可派出。
	board := &boardOracleTool{store: f.cap.store}
	if out := f.tool(board, f.research("claim-5"), `{"action":"claim","number":"O-005"}`); !strings.Contains(out, `"in_progress"`) {
		t.Fatalf("claim = %s", out)
	}
	f.spawn("O-005", "child-e")
	if o := f.oracle(5); o.ClaimedSessionID != "child-e" {
		t.Fatalf("research-claimed spawn = %+v", o)
	}
}

func TestSubagentSessionStartFailClosed(t *testing.T) {
	f := newSubagentFixture(t, 1)
	if err := f.start(f.task.InitialSessionID, "no-reservation", "rogue", OracleExplorerName); err == nil || !strings.Contains(err.Error(), "无平台预占") {
		t.Fatalf("unreserved child of task session started: %v", err)
	}
	if err := f.start("ordinary-chat", "call", "chat-child", ""); err != nil {
		t.Fatalf("ordinary chat child blocked: %v", err)
	}
	if err := f.start("", "", "primary", ""); err != nil {
		t.Fatalf("primary session blocked: %v", err)
	}
	inv := f.research("call-wrong")
	var startErr error
	f.subagent(inv, `{"name":"oracle_explorer","prompt":"O-001"}`, func() {
		startErr = f.start(inv.SessionID, inv.CallID, "wrong-type", "verify")
	})
	if startErr == nil || !strings.Contains(startErr.Error(), "不是 oracle_explorer") {
		t.Fatalf("wrong type started: %v", startErr)
	}
	if n := f.count(`SELECT COUNT(*) FROM webapi_task_subsessions`); n != 0 {
		t.Fatalf("fail-closed start wrote %d rows", n)
	}
	if o := f.oracle(1); o.Status != oracleOpen {
		t.Fatalf("failed start not released: %+v", o)
	}
}

func TestSubagentSubsessionAttribution(t *testing.T) {
	f := newSubagentFixture(t, 3)
	f.spawn("O-001", "child-a")
	f.spawn("O-003", "child-c")
	child := operation.Invocation{SessionID: "child-a", TurnID: "turn-child", CallID: "http-1"}
	if task, err := f.cap.store.taskForSession(t.Context(), "child-a"); err != nil || task == nil || task.ID != f.task.ID {
		t.Fatalf("taskForSession(child) = %+v %v", task, err)
	}
	// webapi_http：授权内发送、存证 session 为子会话；越界拒绝且 0 发送。
	webHTTP := &httpTool{store: f.cap.store, secrets: f.cap.secrets, approvals: f.cap.approvals}
	out := f.tool(webHTTP, child, fmt.Sprintf(`{"method":"GET","url":"%s/orders/1"}`, f.target.URL))
	var evidence map[string]any
	if err := json.Unmarshal([]byte(out), &evidence); err != nil || evidence["status"] != requestCompleted || evidence["session_id"] != "child-a" {
		t.Fatalf("in-scope = %s", out)
	}
	evidenceID := evidence["evidence_id"].(string)
	child.CallID = "http-2"
	if out := f.tool(webHTTP, child, `{"method":"GET","url":"https://outside.example.test/orders/1"}`); !strings.Contains(out, "不在持久授权内") {
		t.Fatalf("out-of-scope = %s", out)
	}
	if n := f.count(`SELECT COUNT(*) FROM webapi_requests WHERE session_id = 'child-a' AND task_id = ? AND status = 'denied' AND sent = 0`, f.task.ID); n != 1 {
		t.Fatalf("denied rows = %d", n)
	}
	// 出口代理：子会话按任务策略，越界 407。
	endpoint := &egressEndpoint{store: f.cap.store, now: time.Now, byToken: map[string]egressRoute{"tok-child": {sessionID: "child-a", turnID: "t", callID: "c", tool: egressToolName}}, byOperation: map[string]egressRoute{}}
	proxied := func(rawURL string) error {
		req := httptest.NewRequest(http.MethodGet, rawURL, nil)
		req.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("tok-child:x")))
		return endpoint.authorize(req)
	}
	if err := proxied(f.target.URL + "/orders/1"); err != nil {
		t.Fatalf("egress in-scope denied: %v", err)
	}
	if err := proxied("https://outside.example.test/x"); err == nil || !errors.Is(err, errEgressDenied) {
		t.Fatalf("egress out-of-scope allowed: %v", err)
	}
	if n := f.count(`SELECT COUNT(*) FROM webapi_egress_flows WHERE task_id = ? AND session_id = 'child-a' AND decision = 'denied'`, f.task.ID); n != 1 {
		t.Fatalf("egress denied rows = %d", n)
	}
	// 审批策略：子会话按任务。
	if policy, task, err := f.cap.store.policyForSession(t.Context(), "child-a"); err != nil || task == nil || task.ID != f.task.ID || policy.Approval != ApprovalHighRisk {
		t.Fatalf("policyForSession(child) = %+v %+v %v", policy, task, err)
	}
	// 候选：来源会话记子会话。
	child.CallID = "finding-1"
	out = f.tool(&findingTool{store: f.cap.store}, child, findingArguments(evidenceID, "订单越权（合成）"))
	var finding map[string]any
	if err := json.Unmarshal([]byte(out), &finding); err != nil || finding["source"].(map[string]any)["session_id"] != "child-a" {
		t.Fatalf("finding = %s", out)
	}
	// 黑板：事实可写、可提出新 Oracle、不能 claim 其它、只能结案自己的。
	fact := &boardFactTool{store: f.cap.store}
	child.CallID = "fact-1"
	if out := f.tool(fact, child, fmt.Sprintf(`{"add":[{"kind":"auth","subject":"订单接口","statement":"无需令牌即可访问（合成）","confidence":"confirmed","evidence_ids":[%q]}]}`, evidenceID)); !strings.Contains(out, `"added"`) {
		t.Fatalf("fact = %s", out)
	}
	board := &boardOracleTool{store: f.cap.store}
	child.CallID = "propose-1"
	if out := f.tool(board, child, `{"action":"propose","items":[{"question":"能否修改他人订单（合成）","category":"authz","endpoint_keys":[],"priority":"medium"}]}`); !strings.Contains(out, `"O-004"`) {
		t.Fatalf("propose = %s", out)
	}
	child.CallID = "claim-2"
	if out := f.tool(board, child, `{"action":"claim","number":"O-002"}`); !strings.Contains(out, "不能 claim O-002") {
		t.Fatalf("claim other = %s", out)
	}
	child.CallID = "drop-3"
	if out := f.tool(board, child, `{"action":"drop","number":"O-003","summary":"不是我的"}`); !strings.Contains(out, "只能 drop 自己认领的 Oracle") {
		t.Fatalf("drop other's = %s", out)
	}
	// 研究会话不能结案子 Agent 正在探索的 Oracle。
	if out := f.tool(board, f.research("close-3"), `{"action":"drop","number":"O-003","summary":"研究会话放弃"}`); !strings.Contains(out, "正由子 Agent 探索") {
		t.Fatalf("research closed subagent oracle: %s", out)
	}
	child.CallID = "close-1"
	if out := f.tool(board, child, fmt.Sprintf(`{"action":"close","number":"O-001","status":"holds","summary":"未登录可读他人订单（合成）","evidence_ids":[%q],"finding_id":%q}`, evidenceID, finding["finding_id"])); !strings.Contains(out, `"holds"`) {
		t.Fatalf("close own = %s", out)
	}
	// 结案后结束：不释放。
	f.end("child-a", agenthooks.StopReasonCompleted, nil)
	if o := f.oracle(1); o.Status != oracleHolds || o.ClaimedSessionID != "child-a" {
		t.Fatalf("concluded oracle changed on end: %+v", o)
	}
	// 验证会话仍不可写（不变）。
	verificationSession := "verify-session"
	f.cap.store.db.Exec(`INSERT INTO webapi_verifications (id, task_id, finding_id, source_revision_id, session_id, created_at) VALUES ('wv-1', ?, ?, 'r', ?, '2026-01-01T00:00:00Z')`, f.task.ID, finding["finding_id"], verificationSession)
	if out := f.tool(fact, operation.Invocation{SessionID: verificationSession, TurnID: "t", CallID: "c"}, `{"add":[{"kind":"other","subject":"x","statement":"y","confidence":"inferred"}]}`); !strings.Contains(out, "只有本任务研究会话可写黑板") {
		t.Fatalf("verification wrote board: %s", out)
	}
}

func TestSubagentApprovalVisibleThroughParent(t *testing.T) {
	f := newSubagentFixture(t, 1)
	if _, err := f.cap.store.setPolicy(t.Context(), f.task.ID, ScopeOrigins, ApprovalAll); err != nil {
		t.Fatal(err)
	}
	f.spawn("O-001", "child-a")
	child := operation.Invocation{SessionID: "child-a", TurnID: "turn-child", CallID: "post-1"}
	done := make(chan string, 1)
	go func() {
		webHTTP := &httpTool{store: f.cap.store, secrets: f.cap.secrets, approvals: f.cap.approvals}
		result, err := f.h.execute(context.Background(), child, "webapi_http", fmt.Sprintf(`{"method":"POST","url":"%s/orders","body":"{}"}`, f.target.URL), webHTTP.Execute)
		done <- coretool.ResultText(result) + fmt.Sprint(err)
	}()
	review := f.h.waitPending(t, f.task.InitialSessionID)
	if review.SessionId != "child-a" || !strings.Contains(review.GetDecision().GetReason(), "子 Agent oracle_explorer 会话 child-a") {
		t.Fatalf("review = %+v", review)
	}
	if f.h.resolve(t, "ordinary-chat", review.Operation.GetOperationId(), true) {
		t.Fatal("unrelated session resolved subagent review")
	}
	if !f.h.resolve(t, f.task.InitialSessionID, review.Operation.GetOperationId(), true) {
		t.Fatal("parent could not resolve subagent review")
	}
	select {
	case out := <-done:
		if !strings.Contains(out, `"status":"completed"`) {
			t.Fatalf("approved request = %s", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approved request did not finish")
	}
	if states := f.h.states(); len(states) != 2 || states[1] != guardrail.ReviewState_REVIEW_STATE_APPROVED {
		t.Fatalf("states = %v", states)
	}
}

func TestSubagentOrphanReleaseEndpointAndUsage(t *testing.T) {
	f := newSubagentFixture(t, 2)
	f.spawn("O-001", "child-a")
	f.spawn("O-002", "child-b")
	f.cap.store.db.Exec(`INSERT INTO webapi_requests (id, task_id, session_id, turn_id, call_id, fingerprint, method, url, status, reason, preview, preview_truncated, capture_truncated, flow_pb, created_at) VALUES ('we-1', ?, 'child-a', 't', 'c', 'fp', 'GET', 'x', 'completed', '', x'', 0, 0, x'', '2026-01-01T00:00:00Z')`, f.task.ID)
	// 节点侧用量观察：子会话 Usage 事件记入所属任务。
	f.h.stream.Publish(&aop.Event{SessionId: "child-a", TurnId: "turn-child", Payload: &aop.Event_Usage{Usage: &aop.TokenUsage{Model: "fixture-model", InputTokens: 100, OutputTokens: 20, Detail: map[string]uint64{"cache_read": 5}}}})
	f.h.stream.Publish(&aop.Event{SessionId: "unrelated", Payload: &aop.Event_Usage{Usage: &aop.TokenUsage{Model: "fixture-model", InputTokens: 7}}})
	report, err := f.cap.store.Usage(t.Context(), f.task.ID, time.Time{}, time.Time{})
	if err != nil || report.Totals.TotalTokens != 120 || len(*report.BySession) != 1 || (*report.BySession)[0].Role != "subagent" || (*report.BySession)[0].CacheReadTokens != 5 {
		t.Fatalf("usage = %+v %v", report, err)
	}
	global, err := f.cap.store.Usage(t.Context(), "", time.Time{}, time.Time{})
	if err != nil || len(*global.ByTask) != 1 || (*global.ByTask)[0].TaskID != f.task.ID || (*global.ByTask)[0].TotalTokens != 120 {
		t.Fatalf("global usage = %+v %v", global, err)
	}
	f.end("child-b", agenthooks.StopReasonCompleted, nil)
	if running, err := f.cap.store.taskHasRunningSubsession(t.Context(), f.task.ID); err != nil || !running {
		t.Fatalf("running = %v %v", running, err)
	}
	status, body, raw := f.do("GET", "/api/webapi/tasks/"+f.task.ID+"/subsessions")
	items, _ := body["items"].([]any)
	if status != 200 || len(items) != 2 {
		t.Fatalf("list = %d %s", status, raw)
	}
	byID := map[string]map[string]any{}
	for _, item := range items {
		value := item.(map[string]any)
		byID[value["session_id"].(string)] = value
	}
	if a := byID["child-a"]; a["status"] != subsessionRunning || a["request_count"] != float64(1) || a["finding_count"] != float64(0) || a["ended_at"] != nil ||
		a["oracle"].(map[string]any)["number"] != "O-001" || a["oracle"].(map[string]any)["status"] != oracleInProgress {
		t.Fatalf("child-a = %v", a)
	}
	if b := byID["child-b"]; b["status"] != subsessionEnded || b["stop_reason"] != "completed" || b["ended_at"] == nil {
		t.Fatalf("child-b = %v", b)
	}
	if status, _, _ := f.do("GET", "/api/webapi/tasks/missing/subsessions"); status != 404 {
		t.Fatalf("missing task = %d", status)
	}
	// Hub 重启：遗留 running 行与预占行按已中断释放。
	f.cap.store.db.Exec(`INSERT INTO webapi_subagent_reservations (parent_session_id, parent_call_id, task_id, oracle_id, created_at) SELECT ?, 'stale-call', task_id, id, '2026-01-01T00:00:00.000Z' FROM webapi_board_oracles WHERE task_id = ? AND number = 2`, f.task.InitialSessionID, f.task.ID)
	f.cap.store.db.Exec(`UPDATE webapi_board_oracles SET status = 'in_progress', claimed_session_id = '' WHERE task_id = ? AND number = 2`, f.task.ID)
	if err := f.cap.store.releaseOrphanSubsessions(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	sub, _ := f.cap.store.subsessionFor(t.Context(), "child-a")
	if sub.Status != subsessionEnded || sub.StopReason != subsessionInterrupted || !strings.Contains(sub.Error, "Hub 重启") {
		t.Fatalf("orphan = %+v", sub)
	}
	if o := f.oracle(1); o.Status != oracleOpen || o.Events[len(o.Events)-1].Summary != "子 Agent 结束未给结论（interrupted）：Hub 重启时子会话仍在运行，按已中断结束" {
		t.Fatalf("orphan oracle = %+v", o)
	}
	if o := f.oracle(2); o.Status != oracleOpen || f.count(`SELECT COUNT(*) FROM webapi_subagent_reservations`) != 0 {
		t.Fatalf("stale reservation = %+v", o)
	}
	if running, _ := f.cap.store.taskHasRunningSubsession(t.Context(), f.task.ID); running {
		t.Fatal("still running after orphan release")
	}
}

func (f *subagentFixture) do(method, path string) (int, map[string]any, string) {
	f.t.Helper()
	recorder := httptest.NewRecorder()
	f.mux.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
	var value map[string]any
	_ = json.Unmarshal(recorder.Body.Bytes(), &value)
	return recorder.Code, value, recorder.Body.String()
}

func TestSubagentPromptAndTask(t *testing.T) {
	oracle := boardOracleModel{Number: 7, Question: "普通用户能否读取他人订单", Category: "authz", Priority: "high", EndpointKeysJSON: `["GET https://api.example.test/orders/{id}"]`}
	task := oracleExplorerTask(oracle, "O-007   只用测试账号 A")
	for _, want := range []string{"O-007", "普通用户能否读取他人订单", "authz", "GET https://api.example.test/orders/{id}", "只用测试账号 A"} {
		if !strings.Contains(task, want) {
			t.Fatalf("task missing %q:\n%s", want, task)
		}
	}
	if strings.Contains(task, "O-007   只用") {
		t.Fatalf("number not stripped from rest: %s", task)
	}
	prompt := oracleExplorerSystemPrompt("O-007")
	for _, want := range []string{"只负责 Oracle O-007", "webapi_finding", "close 或 drop", "一句结论"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
	if !strings.Contains(scenarioBoardPrompt, "name=oracle_explorer") || !strings.Contains(scenarioBoardPrompt, "最多 3 个") {
		t.Fatal("research board prompt lacks subagent line")
	}
}

// 旧库（含一键重放三十八表）单事务补子 Agent 三表到完整结构；旧行保留，完整库再次 Prepare 不变。
func TestSubagentSchemaUpgradeFromReplayStage(t *testing.T) {
	database := filepath.Join(t.TempDir(), "hub.sqlite")
	stage, err := webservice.NewSQLiteStore(database, webapiSchemaStage(38, 35))
	if err != nil {
		t.Fatal(err)
	}
	_ = stage.Close()
	store, err := openTaskStore(database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Save(t.Context(), "replay-stage-task", "旧任务", "保留重放阶段行", []string{"https://api.example.test"}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	if err := PrepareWebAPISchema(database); err != nil {
		t.Fatalf("prepare replay stage: %v", err)
	}
	complete, err := webservice.NewSQLiteStore(database, SchemaModule())
	if err != nil {
		t.Fatalf("full native validation after prepare: %v", err)
	}
	_ = complete.Close()
	reopened, err := openTaskStore(database)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if task, err := reopened.Get(t.Context(), "replay-stage-task"); err != nil || task == nil {
		t.Fatalf("old task lost: %v", err)
	}
	if items, err := reopened.listSubsessions(t.Context(), "replay-stage-task"); err != nil || len(items) != 0 {
		t.Fatalf("new tables unusable: %v %v", items, err)
	}
	if err := PrepareWebAPISchema(database); err != nil {
		t.Fatalf("complete schema not accepted: %v", err)
	}
}
