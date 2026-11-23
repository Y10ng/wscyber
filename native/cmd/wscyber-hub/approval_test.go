package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	agentsession "github.com/chainreactors/cyber/agent/session"
	"github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/exts/guardrail"
	"github.com/chainreactors/cyber/pkg/profile"
	"google.golang.org/protobuf/proto"

	"wscyber/native/internal/testdb"
	"wscyber/native/webapi"
)

// 真实模型节点 Harness：bash 高危命令经原生 tool.before 挂起，等待超过终端 5s 超时后批准仍能执行；
// 变量拼出、无法静态核对的命令同样挂起，拒绝后不执行。不调用模型。
func TestModelNodeApprovalThroughNativeBash(t *testing.T) {
	capability := newSchemaCapability(t)
	defer capability.Close(context.Background())
	handlers := map[string]http.Handler{}
	for _, route := range capability.Routes(nil) {
		handlers[route.Pattern] = route.Handler
	}
	request := httptest.NewRequest("PUT", "/api/webapi/tasks/approval-task", strings.NewReader(`{"known_info":"演示接口 example.test","objective":"审批演示","authorization":{"confirmed":true,"origins":["http://example.test"]}}`))
	request.SetPathValue("id", "approval-task")
	recorder := httptest.NewRecorder()
	handlers["PUT /api/webapi/tasks/{id}"].ServeHTTP(recorder, request)
	var saved struct {
		Task webapi.Task `json:"task"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &saved); err != nil || saved.Task.Policy == nil || !saved.Task.Policy.IsDefault {
		t.Fatalf("save task: %s", recorder.Body.String())
	}
	session := saved.Task.InitialSessionID

	p, err := newModelNodeProfile(profile.Request{ProviderMode: provider.StartupDisabled, Session: &agentsession.Config{}}, t.TempDir(), "http://127.0.0.1:18765/v1", capability)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(context.Background())
	if err := p.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	tools, err := p.Tools()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, definition := range tools.ToolDefinitions() {
		names = append(names, definition.GetName())
	}
	sort.Strings(names)
	t.Logf("模型节点工具：%s", strings.Join(names, ", "))

	stream, err := p.Events()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var states []guardrail.ReviewState
	stream.Observe(func(event *aop.Event) {
		var review guardrail.Review
		if event.Emitter == "guardrail" && event.GetExtension().UnmarshalTo(&review) == nil {
			mu.Lock()
			states = append(states, review.State)
			mu.Unlock()
		}
	})
	binding := capability.ApprovalBinding()
	protocol := func(message *guardrail.ProtocolMessage) proto.Message {
		envelope, _ := aop.Wrap("approval", "", message)
		var answer proto.Message
		if err := binding.Open()(context.Background(), envelope, message, func(reply *aop.Envelope) error {
			answer, err = aop.Unwrap(reply)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return answer
	}
	workdir := t.TempDir()
	run := func(callID, command string) (*aop.ToolResult, error) {
		args, _ := json.Marshal(map[string]any{"command": command})
		ctx := operation.ContextWithInvocation(t.Context(), operation.Invocation{SessionID: session, TurnID: "turn-approval", CallID: callID, WorkDir: workdir})
		return tools.ExecuteTool(ctx, "bash", string(args))
	}

	type outcome struct {
		result *aop.ToolResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := run("rm-approved", "rm -rf ./wscyber-approval-not-exist && echo APPROVED_DONE")
		done <- outcome{result, err}
	}()
	var pending *guardrail.Review
	for deadline := time.Now().Add(5 * time.Second); pending == nil && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if answer, ok := protocol(&guardrail.ProtocolMessage{Message: &guardrail.ProtocolMessage_Pending{Pending: &guardrail.PendingRequest{SessionId: session}}}).(*guardrail.ProtocolMessage); ok && len(answer.GetPendingResult().GetReviews()) > 0 {
			pending = answer.GetPendingResult().GetReviews()[0]
		}
	}
	if pending == nil || pending.GetCall().GetName() != "bash" {
		t.Fatal("bash review not pending")
	}
	time.Sleep(6 * time.Second) // 超过终端 5s 超时：审批等待在工具执行前，不计入。
	if answer, _ := protocol(&guardrail.ProtocolMessage{Message: &guardrail.ProtocolMessage_Resolve{Resolve: &guardrail.ResolveRequest{SessionId: session, OperationId: pending.GetOperation().GetOperationId(), Approve: true}}}).(*guardrail.ProtocolMessage); answer.GetResolved() == nil {
		t.Fatal("resolve failed")
	}
	got := <-done
	if got.err != nil || got.result.GetIsError() || !strings.Contains(coretool.ResultText(got.result), "APPROVED_DONE") {
		t.Fatalf("approved bash = %q err=%v", coretool.ResultText(got.result), got.err)
	}

	// 变量拼出的命令无法静态核对：同样挂起；拒绝后不执行。
	go func() {
		result, err := run("rm-dynamic", "x=rm; $x -rf ./wscyber-approval-not-exist; echo DYNAMIC_RAN")
		done <- outcome{result, err}
	}()
	pending = nil
	for deadline := time.Now().Add(5 * time.Second); pending == nil && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if answer, ok := protocol(&guardrail.ProtocolMessage{Message: &guardrail.ProtocolMessage_Pending{Pending: &guardrail.PendingRequest{SessionId: session}}}).(*guardrail.ProtocolMessage); ok && len(answer.GetPendingResult().GetReviews()) > 0 {
			pending = answer.GetPendingResult().GetReviews()[0]
		}
	}
	if pending == nil || !strings.Contains(pending.GetDecision().GetReason(), "动态展开") {
		t.Fatalf("dynamic command not pending: %v", pending)
	}
	protocol(&guardrail.ProtocolMessage{Message: &guardrail.ProtocolMessage_Resolve{Resolve: &guardrail.ResolveRequest{SessionId: session, OperationId: pending.GetOperation().GetOperationId(), Approve: false}}})
	got = <-done
	if text := coretool.ResultText(got.result); strings.Contains(text, "DYNAMIC_RAN") || !strings.Contains(text, "已拒绝") {
		t.Fatalf("rejected dynamic command ran: %q err=%v", text, got.err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(states) != 4 || states[1] != guardrail.ReviewState_REVIEW_STATE_APPROVED || states[3] != guardrail.ReviewState_REVIEW_STATE_REJECTED {
		t.Fatalf("review events = %v", states)
	}
}

// approvalOffSession 用鉴权内路由保存一个合成任务并显式关闭审批，返回其首会话；
// 供只验证原生命令边界（不经审批挂起）的测试使用。
func approvalOffSession(t *testing.T, capability *webapi.Capability) string {
	t.Helper()
	handlers := map[string]http.Handler{}
	for _, route := range capability.Routes(nil) {
		handlers[route.Pattern] = route.Handler
	}
	serve := func(pattern, path, body string) []byte {
		request := httptest.NewRequest("PUT", path, strings.NewReader(body))
		request.SetPathValue("id", "approval-off-task")
		recorder := httptest.NewRecorder()
		handlers[pattern].ServeHTTP(recorder, request)
		if recorder.Code >= 300 {
			t.Fatalf("%s: %d %s", pattern, recorder.Code, recorder.Body.String())
		}
		return recorder.Body.Bytes()
	}
	var saved struct {
		Task webapi.Task `json:"task"`
	}
	_ = json.Unmarshal(serve("PUT /api/webapi/tasks/{id}", "/api/webapi/tasks/approval-off-task", `{"known_info":"合成","objective":"命令边界","authorization":{"confirmed":true,"origins":["http://example.test"]}}`), &saved)
	serve("PUT /api/webapi/tasks/{id}/policy", "/api/webapi/tasks/approval-off-task/policy", `{"scope":"origins","approval":"off"}`)
	return saved.Task.InitialSessionID
}

// newSchemaCapability 用独立测试库（已迁移的模板克隆）与专属 state-dir 建 webapi 能力。
func newSchemaCapability(t *testing.T) *webapi.Capability {
	t.Helper()
	capability, err := webapi.NewCapability(testdb.New(t).URL, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return capability
}
