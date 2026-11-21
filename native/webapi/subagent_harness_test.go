package webapi

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chainreactors/cyber/agent/provider"
	agentsession "github.com/chainreactors/cyber/agent/session"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	"github.com/chainreactors/cyber/pkg/harness"
)

// 端到端（不调用真实模型）：真实上游 Harness/Session/subagent 工具 + 本项目三个扩展 + 脚本化 fake provider。
// 研究会话以 sync 派出 oracle_explorer；子会话只见白名单工具与子 Agent 提示词，越界请求被拒、授权内请求存证到子会话，
// 结案 refuted；父会话拿到 <subagent_result>；子会话用量计入任务。

type oracleExplorerScript struct {
	t     *testing.T
	f     *subagentFixture
	mu    sync.Mutex
	child []*provider.ChatCompletionRequest
	// parentResults 是父会话收到的 subagent 工具结果文本。
	parentResults []string
	childSystem   string
	childTools    []string
	mode          string
	// completions 记录父会话后续请求中出现的 <subagent_completion> 通知（async）。
	completions []string
}

func (p *oracleExplorerScript) Name() string { return "oracle-explorer-fixture" }

func toolCallMessage(calls ...*aop.ToolCall) *provider.ChatCompletionResponse {
	message := &aop.Message{Role: "assistant"}
	for _, call := range calls {
		message.Content = append(message.Content, &aop.Content{Value: &aop.Content_ToolCall{ToolCall: call}})
	}
	return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: message, FinishReason: "tool_calls"}}, Usage: &aop.TokenUsage{Model: "fixture-model", InputTokens: 10, OutputTokens: 2}}
}

func fixtureCall(id, name string, arguments any) *aop.ToolCall {
	value, _ := aop.JSONValue(arguments)
	return &aop.ToolCall{Id: id, Name: name, Arguments: value}
}

func toolResults(messages []*aop.Message) []string {
	var out []string
	for _, message := range messages {
		for _, content := range message.GetContent() {
			if result := content.GetToolResult(); result != nil {
				out = append(out, coretoolText(result))
			}
		}
	}
	return out
}

func coretoolText(result *aop.ToolResult) string {
	raw, _ := json.Marshal(result)
	return string(raw)
}

func (p *oracleExplorerScript) ChatCompletion(_ context.Context, request *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	system := ""
	if len(request.Messages) > 0 && request.Messages[0].GetRole() == "system" {
		system = provider.MessageText(request.Messages[0])
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.Contains(system, "## 子 Agent：oracle_explorer") {
		p.child = append(p.child, request)
		switch len(p.child) {
		case 1:
			p.childSystem = system
			for _, tool := range request.Tools {
				p.childTools = append(p.childTools, tool.GetName())
			}
			return toolCallMessage(
				fixtureCall("c-bash", "bash", map[string]any{"command": "true"}),
				fixtureCall("c-out", "webapi_http", map[string]any{"method": "GET", "url": "https://outside.example.test/orders/1"}),
				fixtureCall("c-in", "webapi_http", map[string]any{"method": "GET", "url": p.f.target.URL + "/orders/1"}),
			), nil
		case 2:
			var evidenceID string
			if err := p.f.cap.store.db.QueryRow(`SELECT id FROM webapi_requests WHERE status = 'completed' ORDER BY created_at DESC LIMIT 1`).Scan(&evidenceID); err != nil {
				return nil, err
			}
			return toolCallMessage(fixtureCall("c-close", "webapi_board_oracle", map[string]any{"action": "close", "number": "O-001", "status": "refuted",
				"summary": "未登录请求返回的是自己的订单（合成）", "evidence_ids": []string{evidenceID}})), nil
		default:
			return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: provider.TextMessage("assistant", "O-001 不成立：只返回本人订单。"), FinishReason: "stop"}},
				Usage: &aop.TokenUsage{Model: "fixture-model", InputTokens: 10, OutputTokens: 2}}, nil
		}
	}
	for _, message := range request.Messages {
		if text := provider.MessageText(message); strings.Contains(text, "<subagent_completion") {
			p.completions = append(p.completions, text)
		}
	}
	results := toolResults(request.Messages)
	if len(results) == 0 {
		return toolCallMessage(fixtureCall("p-sub", "subagent", map[string]any{"name": OracleExplorerName, "mode": p.mode, "prompt": "O-001 只看订单详情接口"})), nil
	}
	p.parentResults = results
	return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: provider.TextMessage("assistant", "已收到子 Agent 结论。"), FinishReason: "stop"}}}, nil
}

func TestOracleExplorerThroughNativeHarness(t *testing.T) {
	for _, mode := range []string{"sync", "async"} {
		t.Run(mode, func(t *testing.T) { oracleExplorerThroughNativeHarness(t, mode) })
	}
}

func oracleExplorerThroughNativeHarness(t *testing.T, mode string) {
	f := newSubagentFixture(t, 1)
	llm := &oracleExplorerScript{t: t, f: f, mode: mode}
	h, err := harness.New(harness.Config{
		Base:    harness.BaseConfig{Directory: t.TempDir(), Provider: provider.StartupConfig{Mode: provider.StartupDisabled}},
		Session: &agentsession.Config{SkipBaseSkills: true},
		Extensions: []extension.Extension{extension.Func{LoadFunc: func(scope *extension.Scope) error {
			state, err := extension.Use[*provider.State](scope)
			if err == nil {
				state.Set(llm, provider.ProviderConfig{Model: "fixture-model"})
			}
			return err
		}}, f.cap.ToolExtension(), f.cap.ApprovalExtension(), f.cap.SubagentExtension()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	if err := h.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtime, err := h.Runtime()
	if err != nil {
		t.Fatal(err)
	}
	stream, err := h.Events()
	if err != nil {
		t.Fatal(err)
	}
	events := &exploreEvents{sessions: map[string]*turnEvents{}, counts: map[string]map[string]int{}, ended: make(chan string, 16)}
	subscription := stream.Observe(events.observe)
	t.Cleanup(func() { _ = subscription.Close(context.Background()) })

	runExploreTurn(t, runtime, events, f.task.InitialSessionID, "开始研究订单接口。", nil)
	if mode == "async" {
		// 父会话先拿到“已启动”，子会话结束后完成通知推入父收件箱并触发父会话下一次运行。
		deadline := time.Now().Add(10 * time.Second)
		for {
			llm.mu.Lock()
			done := len(llm.completions) > 0
			llm.mu.Unlock()
			if done {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("parent never received subagent completion")
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	llm.mu.Lock()
	parentResults, childSystem, childTools, childCalls := llm.parentResults, llm.childSystem, llm.childTools, len(llm.child)
	llm.mu.Unlock()
	if childCalls != 3 {
		t.Fatalf("child model calls = %d", childCalls)
	}
	llm.mu.Lock()
	completions := append([]string(nil), llm.completions...)
	llm.mu.Unlock()
	switch mode {
	case "sync":
		if len(parentResults) != 1 || !strings.Contains(parentResults[0], "subagent_result") || !strings.Contains(parentResults[0], "O-001 不成立") {
			t.Fatalf("parent result = %v", parentResults)
		}
	default:
		if len(parentResults) == 0 || !strings.Contains(parentResults[0], "Started subagent") ||
			!strings.Contains(completions[0], `status=\"completed\"`) && !strings.Contains(completions[0], `status="completed"`) || !strings.Contains(completions[0], "O-001 不成立") {
			t.Fatalf("async parent result=%v completions=%v", parentResults, completions)
		}
	}
	if strings.Join(childTools, ",") == "" || strings.Contains(strings.Join(childTools, ","), "bash") || strings.Contains(strings.Join(childTools, ","), "subagent") {
		t.Fatalf("child tools = %v", childTools)
	}
	for _, name := range childTools {
		found := false
		for _, allowed := range oracleExplorerTools {
			found = found || name == allowed
		}
		if !found {
			t.Fatalf("child saw non-whitelisted tool %s (%v)", name, childTools)
		}
	}
	for _, want := range []string{scenarioIdentityPrompt, "## 授权与边界", "## 黑板纪律", "只负责 Oracle O-001"} {
		if !strings.Contains(childSystem, want) {
			t.Fatalf("child system prompt missing %q", want)
		}
	}
	var subs []taskSubsessionModel
	if err := f.cap.store.orm.NewSelect().Model(&subs).Scan(t.Context()); err != nil || len(subs) != 1 {
		t.Fatalf("subsessions = %+v %v", subs, err)
	}
	sub := subs[0]
	deadline := time.Now().Add(5 * time.Second)
	for sub.Status != subsessionEnded && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		current, _ := f.cap.store.subsessionFor(t.Context(), sub.SessionID)
		sub = *current
	}
	if sub.Status != subsessionEnded || sub.StopReason != "completed" || sub.ParentSessionID != f.task.InitialSessionID || sub.ParentCallID != "p-sub" {
		t.Fatalf("subsession = %+v", sub)
	}
	if o := f.oracle(1); o.Status != oracleRefuted || o.ClaimedSessionID != sub.SessionID {
		t.Fatalf("oracle = %+v", o)
	}
	if n := f.count(`SELECT COUNT(*) FROM webapi_requests WHERE session_id = ? AND task_id = ?`, sub.SessionID, f.task.ID); n != 2 {
		t.Fatalf("child requests = %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM webapi_requests WHERE session_id = ? AND status = 'denied' AND url LIKE 'https://outside.example.test%'`, sub.SessionID); n != 1 {
		t.Fatalf("out-of-scope not denied: %d", n)
	}
	report, err := f.cap.store.Usage(t.Context(), f.task.ID, time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range *report.BySession {
		if item.SessionID == sub.SessionID && item.Role == "subagent" && item.Calls == 3 && item.TotalTokens == 36 {
			found = true
		}
	}
	if !found {
		t.Fatalf("child usage not counted: %+v", *report.BySession)
	}
}

// 纠正 1：会话轮换（/clear）以 ParentSessionID 开续接会话但 ParentToolCallID 为空，不得被子 Agent 的
// SessionStart FailClosed 拦截，也不落子会话表、不改 Oracle；带 ParentToolCallID 而无预占的委派会话仍拒绝启动。
func TestSubagentSessionRotationNotBlocked(t *testing.T) {
	f := newSubagentFixture(t, 1)
	llm := &oracleExplorerScript{t: t, f: f, mode: "sync"}
	h, err := harness.New(harness.Config{
		Base:    harness.BaseConfig{Directory: t.TempDir(), Provider: provider.StartupConfig{Mode: provider.StartupDisabled}},
		Session: &agentsession.Config{SkipBaseSkills: true},
		Extensions: []extension.Extension{extension.Func{LoadFunc: func(scope *extension.Scope) error {
			state, err := extension.Use[*provider.State](scope)
			if err == nil {
				state.Set(llm, provider.ProviderConfig{Model: "fixture-model"})
			}
			return err
		}}, f.cap.ToolExtension(), f.cap.ApprovalExtension(), f.cap.SubagentExtension()},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	if err := h.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtime, err := h.Runtime()
	if err != nil {
		t.Fatal(err)
	}
	stream, err := h.Events()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	continuations := []string{}
	subscription := stream.Observe(func(event *aop.Event) {
		if started := event.GetSessionStarted(); started != nil && started.GetParentSessionId() == f.task.InitialSessionID {
			mu.Lock()
			continuations = append(continuations, event.GetSessionId())
			mu.Unlock()
		}
	})
	t.Cleanup(func() { _ = subscription.Close(context.Background()) })
	research := f.task.InitialSessionID
	if response := runtime.OpenAOPSession(&aop.OpenSessionRequest{SessionId: research, NodeId: "node-a"}); response.GetAccepted() == nil {
		t.Fatalf("open = %+v", response)
	}
	before := f.oracle(1)
	if _, err := runtime.CommandSession(t.Context(), research, "/clear"); err != nil {
		t.Fatalf("/clear on research session blocked: %v", err)
	}
	mu.Lock()
	started := append([]string(nil), continuations...)
	mu.Unlock()
	if len(started) != 1 || started[0] == research {
		t.Fatalf("continuation sessions = %v", started)
	}
	if n := f.count(`SELECT COUNT(*) FROM webapi_task_subsessions`); n != 0 {
		t.Fatalf("rotation registered %d subsessions", n)
	}
	if after := f.oracle(1); after.Status != before.Status || after.ClaimedSessionID != before.ClaimedSessionID || len(after.Events) != len(before.Events) {
		t.Fatalf("rotation changed oracle: before=%+v after=%+v", before, after)
	}
	// 研究会话轮换后仍是同一逻辑会话，可再次 /clear（续接会话结束也不触碰子会话逻辑）。
	if _, err := runtime.CommandSession(t.Context(), research, "/clear"); err != nil {
		t.Fatalf("second /clear blocked: %v", err)
	}
	if n := f.count(`SELECT COUNT(*) FROM webapi_board_oracle_events`); n != len(before.Events) {
		t.Fatalf("second rotation wrote oracle events: %d", n)
	}
	// 带 ParentToolCallID 的委派会话无预占：真实 Session 仍 FailClosed。
	response := runtime.OpenAOPSession(&aop.OpenSessionRequest{SessionId: "rogue-child", NodeId: "node-a", ParentSessionId: research, ParentToolCallId: "no-reservation"})
	if response.GetRejected() == nil || !strings.Contains(response.GetRejected().GetMessage(), "无平台预占") {
		t.Fatalf("unreserved delegated child = %+v", response)
	}
	if n := f.count(`SELECT COUNT(*) FROM webapi_task_subsessions`); n != 0 {
		t.Fatalf("rejected child registered %d subsessions", n)
	}
}
