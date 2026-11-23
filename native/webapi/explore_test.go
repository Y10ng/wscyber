package webapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	agenthooks "github.com/chainreactors/cyber/agent/hooks"
	agentprompt "github.com/chainreactors/cyber/agent/prompt"
	provider "github.com/chainreactors/cyber/agent/provider"
	agentsession "github.com/chainreactors/cyber/agent/session"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	types "github.com/chainreactors/cyber/core/types"
	"github.com/chainreactors/cyber/pkg/harness"
	"github.com/uptrace/bun"
)

// 全部为合成数据：临时库、example.test 演示目标、替身 launcher 与替身 Provider。

func exploreScenario(t *testing.T, f *assetFixture, taskID string) map[string]any {
	t.Helper()
	_, value, raw := f.do("GET", "/api/webapi/tasks/"+taskID, "")
	scenario, ok := value["scenario"].(map[string]any)
	if !ok {
		t.Fatalf("task %s lacks scenario: %s", taskID, raw)
	}
	return scenario
}

func runOptionsOf(t *testing.T, run fakeRun) *types.AgentRunOptions {
	t.Helper()
	extensions := run.request.GetExtensions()
	if len(extensions) == 0 {
		return nil
	}
	if len(extensions) != 1 {
		t.Fatalf("extensions = %d", len(extensions))
	}
	options := &types.AgentRunOptions{}
	if err := extensions[0].UnmarshalTo(options); err != nil {
		t.Fatal(err)
	}
	return options
}

func TestExploreScenarioMaxRoundsDefaultsValidationAudit(t *testing.T) {
	f := newAssetFixture(t)
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/t1", scenarioTaskJSON("")); status != 201 {
		t.Fatalf("create = %d %s", status, raw)
	}
	if s := exploreScenario(t, f, "t1"); s["max_rounds"] != float64(8) || s["is_default"] != true {
		t.Fatalf("default pentest rounds = %v", s)
	}
	// 场景选项带默认轮数。
	_, value, _ := f.do("GET", "/api/webapi/scenarios", "")
	options := value["scenarios"].([]any)
	if options[0].(map[string]any)["default_max_rounds"] != float64(8) || options[1].(map[string]any)["default_max_rounds"] != float64(12) {
		t.Fatalf("scenario options = %v", options)
	}
	// 只改场景：默认轮数跟随场景（12），审计记最大轮数变化。
	if status, value, raw := f.do("PUT", "/api/webapi/tasks/t1/scenario", `{"scenario":"autonomous"}`); status != 200 || scenarioOf(t, value)["max_rounds"] != float64(12) {
		t.Fatalf("switch scenario = %d %s", status, raw)
	}
	// 非法值 400，不改。
	for _, body := range []string{`{"max_rounds":0}`, `{"max_rounds":21}`, `{"max_rounds":3.5}`, `{"max_rounds":"8"}`, `{"max_rounds":-1}`} {
		if status, _, raw := f.do("PUT", "/api/webapi/tasks/t1/scenario", body); status != 400 {
			t.Fatalf("%s accepted: %d %s", body, status, raw)
		}
	}
	if s := exploreScenario(t, f, "t1"); s["max_rounds"] != float64(12) {
		t.Fatalf("invalid changed rounds: %v", s)
	}
	// 边界 1、20 允许；只改轮数时场景与完成标准保留。
	for _, rounds := range []int{1, 20} {
		if status, value, raw := f.do("PUT", "/api/webapi/tasks/t1/scenario", fmt.Sprintf(`{"max_rounds":%d}`, rounds)); status != 200 ||
			scenarioOf(t, value)["max_rounds"] != float64(rounds) || scenarioOf(t, value)["key"] != ScenarioAutonomous {
			t.Fatalf("rounds %d = %d %s", rounds, status, raw)
		}
	}
	// 保存值后再换场景：保存值不随场景变。
	if status, value, raw := f.do("PUT", "/api/webapi/tasks/t1/scenario", `{"scenario":"webapi_pentest"}`); status != 200 || scenarioOf(t, value)["max_rounds"] != float64(20) {
		t.Fatalf("saved rounds after switch = %d %s", status, raw)
	}
	events := scenarioAudits(t, f.capability, "task.scenario", "t1")
	if len(events) != 4 {
		t.Fatalf("task.scenario audits = %d", len(events))
	}
	if detail := auditDetail(t, events[1]); mustJSON(t, detail["max_rounds"]) != `{"from":12,"to":1}` || !strings.Contains(events[1].Summary, "最大轮数 12 → 1") {
		t.Fatalf("rounds audit = %v %s", detail, events[1].Summary)
	}
	if detail := auditDetail(t, events[3]); detail["max_rounds"] != nil || detail["to"] != ScenarioWebAPIPentest {
		t.Fatalf("switch with saved rounds audit = %v", detail)
	}
	// 任务列表项同样带生效值。
	_, value, raw := f.do("GET", "/api/webapi/task-list", "")
	if !strings.Contains(raw, `"max_rounds":20`) {
		t.Fatalf("task list lacks max_rounds: %s", raw)
	}

	// 资产默认：非法 400 不保存；合法保存并拷贝到发起的任务。
	f.createAsset("a1")
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1", strings.TrimSuffix(assetBody, "}")+`,"max_rounds":30}`); status != 400 {
		t.Fatalf("asset bad rounds = %d %s", status, raw)
	}
	status, value, raw := f.do("PUT", "/api/webapi/assets/a1", strings.TrimSuffix(assetBody, "}")+`,"scenario":"autonomous","max_rounds":5}`)
	if s := scenarioOf(t, value); status != 200 || s["max_rounds"] != float64(5) || s["key"] != ScenarioAutonomous {
		t.Fatalf("asset rounds = %d %s", status, raw)
	}
	// 不带字段再保存：保留。
	if _, value, raw = f.do("PUT", "/api/webapi/assets/a1", strings.Replace(assetBody, "演示库存系统", "演示库存系统二", 1)); scenarioOf(t, value)["max_rounds"] != float64(5) {
		t.Fatalf("asset save without rounds reset it: %s", raw)
	}
	var assetEvents []auditEventModel
	if err := f.capability.store.orm.NewSelect().Model(&assetEvents).Where("asset_id = ? AND action = 'asset.update'", "a1").Order("at ASC", "id ASC").Scan(t.Context()); err != nil || len(assetEvents) != 2 {
		t.Fatalf("asset audits = %+v %v", assetEvents, err)
	}
	if detail := auditDetail(t, assetEvents[0]); !strings.Contains(mustJSON(t, detail["changed"]), `"max_rounds"`) || detail["values"].(map[string]any)["max_rounds"] != float64(5) {
		t.Fatalf("asset rounds audit = %v", detail)
	}
	if detail := auditDetail(t, assetEvents[1]); strings.Contains(mustJSON(t, detail["changed"]), "max_rounds") {
		t.Fatalf("unchanged rounds audited: %v", detail)
	}
	f.capability.launcher = newFakeLauncher(productAgent("node-a"))
	if status, value, raw = f.do("PUT", "/api/webapi/assets/a1/tasks/from-asset", ""); status != 201 || scenarioOf(t, value["task"].(map[string]any))["max_rounds"] != float64(5) {
		t.Fatalf("launched task rounds = %d %s", status, raw)
	}
	// 之后改资产不影响已建任务。
	f.do("PUT", "/api/webapi/assets/a1", strings.TrimSuffix(assetBody, "}")+`,"max_rounds":9}`)
	if s := exploreScenario(t, f, "from-asset"); s["max_rounds"] != float64(5) {
		t.Fatalf("existing task changed with asset: %v", s)
	}
}

func TestExploreServerLaunchRunOptions(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-20T09:30:00Z")
	f.createAsset("a1")
	f.do("PUT", "/api/webapi/assets/a1", strings.TrimSuffix(assetBody, "}")+`,"done_criteria":"订单与库存接口都已结论","max_rounds":6}`)

	// 研究会话（手动发起）：带 AgentRunOptions。
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/tasks/manual-1", ""); status != 201 {
		t.Fatalf("launch = %d %s", status, raw)
	}
	runs := f.runsTo(f.sessionOf("manual-1"))
	if len(runs) != 1 {
		t.Fatalf("runs = %+v", runs)
	}
	if options := runOptionsOf(t, runs[0]); options == nil || options.EvalCriteria != "订单与库存接口都已结论" || options.EvalRounds != "6" || runs[0].request.MaxTurns != 0 {
		t.Fatalf("research run options = %+v", options)
	}

	// 计划发起的研究同样带（默认完成标准、场景默认轮数）。
	f.do("PUT", "/api/webapi/assets/a1", strings.TrimSuffix(assetBody, "}")+`,"done_criteria":""}`)
	f.putSchedule("a1", `{"cron":"0 * * * *"}`)
	f.setNow("2026-11-20T10:00:10Z")
	f.tick()
	run := f.runFor("a1", "2026-11-20T10:00:00Z")
	scheduled := f.runsTo(f.sessionOf(run.TaskID))
	if len(scheduled) != 1 {
		t.Fatalf("scheduled runs = %+v", scheduled)
	}
	if options := runOptionsOf(t, scheduled[0]); options == nil || options.EvalCriteria != scenarioPentestDoneCriteria || options.EvalRounds != "6" {
		t.Fatalf("scheduled options = %+v", options)
	}

	// 计划后续的验证会话：不带。
	findingID, _ := f.addFinding(run.TaskID, "订单接口越权读取", simpleContent(), nil)
	f.endResearch(run.TaskID)
	f.setNow("2026-11-20T10:01:00Z")
	f.tick()
	items := f.followups(run.ID)
	if len(items) != 1 || items[0].FindingID != findingID || items[0].Status != FollowupLaunched {
		t.Fatalf("followups = %+v", items)
	}
	verification, err := f.capability.store.verificationByID(t.Context(), run.TaskID, findingID, items[0].TargetID)
	if err != nil || verification == nil {
		t.Fatalf("verification = %v %v", verification, err)
	}
	verifyRuns := f.runsTo(verification.SessionID)
	if len(verifyRuns) != 1 || len(verifyRuns[0].request.GetExtensions()) != 0 {
		t.Fatalf("verification run carried options: %+v", verifyRuns)
	}

	// 本机受控模型节点：不开 Goal 模式，保持 maxTurns=3。
	f.launcher.mu.Lock()
	f.launcher.agents = []*types.AgentView{{Hello: &aop.AgentHello{NodeId: "local", Capabilities: []string{localModelCapability, webapiHTTPCapability}}, Status: &aop.AgentStatus{Provider: "openai", Model: "demo"}}}
	f.launcher.mu.Unlock()
	if status, _, raw := f.do("PUT", "/api/webapi/assets/a1/tasks/local-1", ""); status != 201 {
		t.Fatalf("local launch = %d %s", status, raw)
	}
	local := f.runsTo(f.sessionOf("local-1"))
	if len(local) != 1 || len(local[0].request.GetExtensions()) != 0 || local[0].request.MaxTurns != localModelMaxTurns {
		t.Fatalf("local run = %+v", local)
	}
}

// exploreHookFixture：真实 hooks.Registry + Cyber 默认提示词扩展 + 本能力 ToolExtension。
func exploreEvalPrompt(t *testing.T, resolver agentprompt.Resolver, criteria, trace, progress string) string {
	t.Helper()
	result := resolver.Build(t.Context(), agentprompt.Context{Target: agentprompt.EvaluatorRequest, Evaluation: agentprompt.EvaluationContext{
		Goal: "开始探索任务。", Criteria: criteria, Progress: progress, Trace: trace,
	}})
	if len(result.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
	return result.Prompt
}

func emitRunEnd(t *testing.T, registry *corehooks.Registry, sessionID string, messages int, stop agenthooks.StopReason) {
	t.Helper()
	event := agenthooks.RunEndEvent{SessionID: sessionID, TurnID: "turn-1", Stop: stop, Messages: make([]*aop.Message, messages)}
	if _, err := agenthooks.RunEnd.Emit(t.Context(), registry, event); err != nil {
		t.Fatal(err)
	}
}

func TestExploreEvaluatorContributionResearchOnly(t *testing.T) {
	f := newAssetFixture(t)
	registry, resolver := scenarioHookFixture(t, f)
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/t1", scenarioTaskJSON("")); status != 201 {
		t.Fatalf("create = %d %s", status, raw)
	}
	if status, _, raw := f.do("POST", "/api/webapi/tasks/t1/board/oracles", `{"question":"普通用户能否读取他人订单详情","category":"authz","endpoint_keys":["GET https://api.example.test/orders/{id}"],"priority":"high"}`); status != 201 {
		t.Fatalf("propose = %d %s", status, raw)
	}
	task, _ := f.capability.store.Get(t.Context(), "t1")
	trace := "Turns: 2 | Messages: 5 | Context tokens: 100/200000 (0%)\nTool calls: 0\n"
	progress := "Round 1 (hard ceiling 8, reached only if you never stop the loop)\n"

	// 研究会话：RunEnd 登记后评估追加黑板段与评估要求，位于上游请求之后。
	emitRunEnd(t, registry, task.InitialSessionID, 5, agenthooks.StopReasonCompleted)
	got := exploreEvalPrompt(t, resolver, scenarioPentestDoneCriteria, trace, progress)
	for _, want := range []string{"## Execution Trace", "## 平台黑板", "地图：1 个端点", "O-001 [high·open·authz]", "地图端点 1，未覆盖 0；Oracle open 1 / in_progress 0 / holds 0 / refuted 0 / blocked 0",
		"首轮评估", "## 平台评估要求", "feedback 必须点名下一轮要处理的 Oracle 编号", "连续两轮黑板无新事实、无新结论"} {
		if !strings.Contains(got, want) {
			t.Fatalf("evaluator prompt lacks %q:\n%s", want, got)
		}
	}
	if strings.Index(got, "## Execution Trace") > strings.Index(got, "## 平台黑板") {
		t.Fatal("board section must follow upstream request")
	}
	// 已认领：同一评估再构建一次不重复追加（没有新的 Run 结束）。
	if again := exploreEvalPrompt(t, resolver, scenarioPentestDoneCriteria, trace, progress); strings.Contains(again, "## 平台黑板") {
		t.Fatal("pending claimed twice")
	}
	// 第 2、3 轮无新增：连续无新增轮数累加。
	for round, idle := range map[int]int{2: 1} {
		emitRunEnd(t, registry, task.InitialSessionID, 7, agenthooks.StopReasonCompleted)
		got = exploreEvalPrompt(t, resolver, scenarioPentestDoneCriteria, strings.Replace(trace, "Messages: 5", "Messages: 7", 1), fmt.Sprintf("Round %d (hard ceiling 8)\n", round))
		if !strings.Contains(got, fmt.Sprintf("新增事实 0 条、新结论 0 个；连续无新增轮数 %d。", idle)) {
			t.Fatalf("round %d delta:\n%s", round, got)
		}
	}
	if _, err := f.capability.store.boardWriteFacts(t.Context(), "t1", task.InitialSessionID, "turn-1", "call-1",
		[]boardFactInput{{Kind: "auth", Subject: "登录", Statement: "使用 Bearer 令牌", Confidence: factInferred}}, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	emitRunEnd(t, registry, task.InitialSessionID, 9, agenthooks.StopReasonCompleted)
	got = exploreEvalPrompt(t, resolver, scenarioPentestDoneCriteria, strings.Replace(trace, "Messages: 5", "Messages: 9", 1), "Round 3 (hard ceiling 8)\n")
	if !strings.Contains(got, "新增事实 1 条、新结论 0 个；连续无新增轮数 0。") {
		t.Fatalf("round 3 delta:\n%s", got)
	}

	// 本 turn 有 unknown 请求：提示评估器停止。
	if err := f.capability.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&requestModel{ID: "req-unknown", TaskID: "t1", SessionID: task.InitialSessionID, TurnID: "turn-1", CallID: "call-9",
			Fingerprint: "fp", Method: "GET", URL: "https://api.example.test/orders/1", Status: requestUnknown, Preview: []byte{}, FlowPB: []byte{}, CreatedAt: "2026-11-20T00:00:00Z"}).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	emitRunEnd(t, registry, task.InitialSessionID, 10, agenthooks.StopReasonTerminated)
	if got := exploreEvalPrompt(t, resolver, scenarioPentestDoneCriteria, strings.Replace(trace, "Messages: 5", "Messages: 10", 1), "Round 4 (hard ceiling 8)\n"); !strings.Contains(got, exploreUnresolvedNotice) {
		t.Fatalf("unknown notice missing:\n%s", got)
	}
	if got := exploreEvalPrompt(t, resolver, scenarioPentestDoneCriteria, trace, progress); strings.Contains(got, exploreUnresolvedNotice) {
		t.Fatal("notice without claim")
	}

	// 消息数或完成标准不一致、出错的 Run：不追加。
	emitRunEnd(t, registry, task.InitialSessionID, 11, agenthooks.StopReasonCompleted)
	if got := exploreEvalPrompt(t, resolver, scenarioPentestDoneCriteria, trace, progress); strings.Contains(got, "## 平台黑板") {
		t.Fatal("message count mismatch appended board")
	}
	if got := exploreEvalPrompt(t, resolver, "别的标准", strings.Replace(trace, "Messages: 5", "Messages: 11", 1), progress); strings.Contains(got, "## 平台黑板") {
		t.Fatal("criteria mismatch appended board")
	}
	emitRunEnd(t, registry, task.InitialSessionID, 5, agenthooks.StopReasonError)
	if got := exploreEvalPrompt(t, resolver, scenarioPentestDoneCriteria, trace, progress); strings.Contains(got, "## 平台黑板") {
		t.Fatal("errored run appended board")
	}

	// 验证会话（同任务）、未知会话：不追加。
	if err := f.capability.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&verificationModel{ID: "v1", TaskID: "t1", FindingID: "wf_demo", SourceRevisionID: "wr_demo", SessionID: "verify-session", CreatedAt: "2026-11-20T00:00:00Z"}).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, sessionID := range []string{"verify-session", "unknown-session"} {
		emitRunEnd(t, registry, sessionID, 5, agenthooks.StopReasonCompleted)
		if got := exploreEvalPrompt(t, resolver, scenarioPentestDoneCriteria, trace, progress); strings.Contains(got, "## 平台黑板") || !strings.Contains(got, "## Acceptance Criteria") {
			t.Fatalf("session %s appended board:\n%s", sessionID, got)
		}
	}
}

// ---- 上游 Goal 循环端到端：真实 harness 会话 + 替身 Provider ----

type exploreLoopProvider struct {
	t        *testing.T
	mu       sync.Mutex
	verdicts []map[string]any
	agent    [][]*aop.Message
	prompts  []string
	// onEvaluate 在每次评估调用时执行（读取当时的事件状态）。
	onEvaluate func(round int)
}

func (p *exploreLoopProvider) Name() string { return "explore-fixture" }

func (p *exploreLoopProvider) ChatCompletion(_ context.Context, request *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	evaluation := false
	for _, tool := range request.Tools {
		evaluation = evaluation || tool.GetName() == "verdict"
	}
	p.mu.Lock()
	if !evaluation {
		p.agent = append(p.agent, request.Messages)
		round := len(p.agent)
		p.mu.Unlock()
		return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: provider.TextMessage("assistant", fmt.Sprintf("第 %d 轮：已整理接口与结论。", round)), FinishReason: "stop"}}}, nil
	}
	p.prompts = append(p.prompts, provider.MessageText(request.Messages[len(request.Messages)-1]))
	round := len(p.prompts)
	if round > len(p.verdicts) {
		p.mu.Unlock()
		return nil, fmt.Errorf("unexpected evaluation round %d", round)
	}
	verdict := p.verdicts[round-1]
	hook := p.onEvaluate
	p.mu.Unlock()
	if hook != nil {
		hook(round)
	}
	arguments, _ := aop.JSONValue(verdict)
	message := &aop.Message{Role: "assistant", Content: []*aop.Content{{Value: &aop.Content_ToolCall{ToolCall: &aop.ToolCall{Id: fmt.Sprintf("verdict-%d", round), Name: "verdict", Arguments: arguments}}}}}
	return &provider.ChatCompletionResponse{Choices: []provider.Choice{{Message: message, FinishReason: "tool_calls"}}}, nil
}

type exploreEvents struct {
	mu       sync.Mutex
	sessions map[string]*turnEvents
	counts   map[string]map[string]int
	ended    chan string
}

func (e *exploreEvents) observe(event *aop.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	sessionID := event.GetSessionId()
	if e.sessions[sessionID] == nil {
		e.sessions[sessionID], e.counts[sessionID] = newTurnEvents(), map[string]int{}
	}
	e.sessions[sessionID].observe(event)
	counts := e.counts[sessionID]
	switch {
	case event.GetTurnStarted() != nil:
		counts["turn_started"]++
	case event.GetTurnEnded() != nil:
		counts["turn_ended"]++
		select {
		case e.ended <- sessionID:
		default:
		}
	case event.GetStatus() != nil:
		counts[event.GetStatus().GetState()]++
	case event.GetUsage() != nil:
		counts["usage"]++
	}
}

// state 返回 apiLauncher 的判定材料：仍有未结束轮次（SessionRunning 条件 3）、是否出现过 TurnEnded。
func (e *exploreEvents) state(sessionID string) (open bool, ended bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	turns := e.sessions[sessionID]
	if turns == nil {
		return false, false
	}
	return turns.openTurns() > 0, turns.ended
}

func (e *exploreEvents) count(sessionID, key string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.counts[sessionID][key]
}

func exploreHarness(t *testing.T, f *assetFixture, llm provider.Provider) (*agentsession.Runtime, *exploreEvents) {
	t.Helper()
	h, err := harness.New(harness.Config{
		Base:    harness.BaseConfig{Directory: t.TempDir(), Provider: provider.StartupConfig{Mode: provider.StartupDisabled}},
		Session: &agentsession.Config{SkipBaseSkills: true},
		Extensions: []extension.Extension{extension.Func{LoadFunc: func(scope *extension.Scope) error {
			state, err := extension.Use[*provider.State](scope)
			if err == nil {
				state.Set(llm, provider.ProviderConfig{Model: "explore-fixture"})
			}
			return err
		}}, f.capability.ToolExtension()},
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
	events := &exploreEvents{sessions: map[string]*turnEvents{}, counts: map[string]map[string]int{}, ended: make(chan string, 8)}
	subscription := stream.Observe(events.observe)
	t.Cleanup(func() { _ = subscription.Close(context.Background()) })
	return runtime, events
}

func runExploreTurn(t *testing.T, runtime *agentsession.Runtime, events *exploreEvents, sessionID, text string, options *types.AgentRunOptions) {
	t.Helper()
	if response := runtime.OpenAOPSession(&aop.OpenSessionRequest{SessionId: sessionID, NodeId: "node-a"}); response.GetAccepted() == nil {
		t.Fatalf("open = %+v", response)
	}
	extensions, err := packRunOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	response := runtime.RunAOPTurn(t.Context(), &aop.RunTurnRequest{SessionId: sessionID, TurnId: "turn-" + sessionID, Extensions: extensions,
		Input: &aop.Message{Id: "msg-" + sessionID, Role: "user", Content: []*aop.Content{{Value: &aop.Content_Text{Text: &aop.TextContent{Text: text}}}}}})
	if response.GetAccepted() == nil {
		t.Fatalf("run = %+v", response)
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ended := <-events.ended:
			if ended == sessionID {
				return
			}
		case <-deadline:
			t.Fatal("turn did not end")
		}
	}
}

func TestExploreGoalLoopRoundsFeedbackAndTurnEnd(t *testing.T) {
	f := newAssetFixture(t)
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/t1", scenarioTaskJSON("")); status != 201 {
		t.Fatalf("create = %d %s", status, raw)
	}
	f.do("PUT", "/api/webapi/tasks/t1/scenario", `{"max_rounds":3}`)
	f.do("POST", "/api/webapi/tasks/t1/board/oracles", `{"question":"普通用户能否读取他人订单详情","category":"authz","endpoint_keys":["GET https://api.example.test/orders/{id}"],"priority":"high"}`)
	task, _ := f.capability.store.Get(t.Context(), "t1")
	feedback := "未达标：下一轮处理 O-001，并覆盖 GET https://api.example.test/orders/{id}。"
	llm := &exploreLoopProvider{t: t, verdicts: []map[string]any{
		{"pass": false, "continue": true, "reason": "O-001 仍 open", "feedback": feedback, "inherit_context": true},
		{"pass": true, "continue": false, "reason": "完成标准已满足", "feedback": "", "inherit_context": true},
	}}
	runtime, events := exploreHarness(t, f, llm)
	type snapshot struct{ open, ended bool }
	during := map[int]snapshot{}
	llm.onEvaluate = func(round int) {
		open, ended := events.state(task.InitialSessionID)
		during[round] = snapshot{open, ended}
		if round == 1 {
			// 两轮之间写入一条事实：第 2 轮评估应看到增量。
			if _, err := f.capability.store.boardWriteFacts(context.Background(), "t1", task.InitialSessionID, "turn-x", "call-1",
				[]boardFactInput{{Kind: "auth", Subject: "登录", Statement: "使用 Bearer 令牌", Confidence: factInferred}}, nil, time.Now()); err != nil {
				t.Error(err)
			}
		}
	}

	options, err := f.capability.store.researchRunOptions(t.Context(), "t1")
	if err != nil || options.EvalRounds != "3" || options.EvalCriteria != scenarioPentestDoneCriteria {
		t.Fatalf("options = %+v %v", options, err)
	}
	runExploreTurn(t, runtime, events, task.InitialSessionID, strings.Join(webAPIFirstMessage(task), "\n"), options)

	llm.mu.Lock()
	defer llm.mu.Unlock()
	if len(llm.agent) != 2 || len(llm.prompts) != 2 {
		t.Fatalf("agent calls = %d, evaluations = %d", len(llm.agent), len(llm.prompts))
	}
	// 反馈作为第 2 轮输入（最后一条用户消息）。
	round2 := llm.agent[1]
	if last := round2[len(round2)-1]; last.GetRole() != "user" || provider.MessageText(last) != feedback {
		t.Fatalf("round 2 input = %s %q", last.GetRole(), provider.MessageText(last))
	}
	// 每轮系统提示词仍是研究场景注入（BeforeRun 每轮触发）。
	if system := provider.MessageText(round2[0]); round2[0].GetRole() != "system" || !strings.Contains(system, "## 黑板纪律") {
		t.Fatalf("round 2 system prompt not injected: %s", system)
	}
	for index, prompt := range llm.prompts {
		for _, want := range []string{"## Acceptance Criteria\n" + scenarioPentestDoneCriteria, "## 平台黑板", "O-001", "地图端点 1，未覆盖 0", "## 平台评估要求"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("evaluation %d lacks %q:\n%s", index+1, want, prompt)
			}
		}
	}
	if !strings.Contains(llm.prompts[0], "首轮评估") || !strings.Contains(llm.prompts[1], "新增事实 1 条、新结论 0 个；连续无新增轮数 0。") ||
		!strings.Contains(llm.prompts[1], "you asked for: "+feedback) || !strings.Contains(llm.prompts[1], "hard ceiling 3") {
		t.Fatalf("evaluation progress:\n%s\n---\n%s", llm.prompts[0], llm.prompts[1])
	}
	// 整个 Goal 循环只有一对 TurnStarted/TurnEnded；评估期间轮次仍未结束（running、sessionEnded=false），结束后 ended。
	if started, ended := events.count(task.InitialSessionID, "turn_started"), events.count(task.InitialSessionID, "turn_ended"); started != 1 || ended != 1 {
		t.Fatalf("turn events started=%d ended=%d", started, ended)
	}
	if events.count(task.InitialSessionID, types.EvalStateStart) != 2 || events.count(task.InitialSessionID, types.EvalStateEnd) != 2 {
		t.Fatalf("eval statuses = %v", events.counts[task.InitialSessionID])
	}
	if usage := events.count(task.InitialSessionID, "usage"); usage > 2 {
		t.Fatalf("usage events = %d（评估调用不应产生 Usage 事件）", usage)
	}
	for round := 1; round <= 2; round++ {
		if s := during[round]; !s.open || s.ended {
			t.Fatalf("during evaluation %d: open=%v ended=%v", round, s.open, s.ended)
		}
	}
	if open, ended := events.state(task.InitialSessionID); open || !ended {
		t.Fatalf("after loop: open=%v ended=%v", open, ended)
	}

	// 非研究会话开 Goal 模式：评估不追加黑板。
	llm.verdicts = append(llm.verdicts, map[string]any{"pass": true, "continue": false, "reason": "ok", "feedback": "", "inherit_context": true})
	llm.mu.Unlock()
	runExploreTurn(t, runtime, events, "plain-session", "整理一份接口清单。", &types.AgentRunOptions{EvalCriteria: scenarioPentestDoneCriteria, EvalRounds: "2"})
	llm.mu.Lock()
	if len(llm.prompts) != 3 || strings.Contains(llm.prompts[2], "## 平台黑板") {
		t.Fatalf("plain session evaluation = %d\n%s", len(llm.prompts), llm.prompts[len(llm.prompts)-1])
	}
}
