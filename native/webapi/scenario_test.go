package webapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	agenthooks "github.com/chainreactors/cyber/agent/hooks"
	agentprompt "github.com/chainreactors/cyber/agent/prompt"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	coretool "github.com/chainreactors/cyber/core/tool"
	promptext "github.com/chainreactors/cyber/exts/prompt"
	"github.com/uptrace/bun"
)

// 全部为合成数据：临时库、example.test 演示目标。

const scenarioTaskBody = `{"known_info":"演示商城 API，文档 /docs","objective":"检查越权与输入校验","authorization":{"confirmed":true,"origins":["https://api.example.test"]}`

func scenarioTaskJSON(extra string) string {
	if extra == "" {
		return scenarioTaskBody + "}"
	}
	return scenarioTaskBody + "," + extra + "}"
}

func scenarioOf(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	scenario, ok := value["scenario"].(map[string]any)
	if !ok {
		t.Fatalf("missing scenario: %v", value)
	}
	return scenario
}

func scenarioAudits(t *testing.T, c *Capability, action, taskID string) []auditEventModel {
	t.Helper()
	var events []auditEventModel
	if err := c.store.orm.NewSelect().Model(&events).Where("action = ? AND task_id = ?", action, taskID).Order("at ASC", "id ASC").Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	return events
}

func TestScenarioTaskDefaultsPutValidationAndAudit(t *testing.T) {
	f := newAssetFixture(t)
	// 默认：无记录 → webapi_pentest + 默认完成标准，is_default=true；task.create 记 scenario。
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/t1", scenarioTaskJSON("")); status != 201 {
		t.Fatalf("create = %d %s", status, raw)
	}
	_, value, _ := f.do("GET", "/api/webapi/tasks/t1", "")
	scenario := scenarioOf(t, value)
	if scenario["key"] != ScenarioWebAPIPentest || scenario["done_criteria"] != "" || scenario["done_criteria_effective"] != scenarioPentestDoneCriteria || scenario["is_default"] != true || scenario["name"] != "Web/API 渗透测试" {
		t.Fatalf("default scenario = %v", scenario)
	}
	if events := scenarioAudits(t, f.capability, "task.create", "t1"); len(events) != 1 || auditDetail(t, events[0])["scenario"] != ScenarioWebAPIPentest {
		t.Fatalf("task.create audit = %+v", events)
	}
	if n, err := f.capability.store.orm.NewSelect().Model((*taskScenarioModel)(nil)).Where("task_id = ?", "t1").Count(t.Context()); err != nil || n != 0 {
		t.Fatalf("default task stored a scenario row: %d %v", n, err)
	}

	// 新建时校验：未知场景、超长完成标准 → 400 且不建任务。
	long := strings.Repeat("标", maxDoneCriteriaRunes+1)
	for id, extra := range map[string]string{"bad1": `"scenario":"pentest"`, "bad2": `"done_criteria":"` + long + `"`, "bad3": `"scenario":"WEBAPI_PENTEST"`} {
		if status, _, raw := f.do("PUT", "/api/webapi/tasks/"+id, scenarioTaskJSON(extra)); status != 400 {
			t.Fatalf("%s accepted: %d %s", id, status, raw)
		}
		if task, _ := f.capability.store.Get(t.Context(), id); task != nil {
			t.Fatalf("%s created despite 400", id)
		}
	}
	// 2000 字整好允许。
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/edge", scenarioTaskJSON(`"done_criteria":"`+strings.Repeat("标", maxDoneCriteriaRunes)+`"`)); status != 201 {
		t.Fatalf("2000 runes rejected: %d %s", status, raw)
	}

	// 新建时带场景与完成标准：随任务保存、读回。
	status, value, raw := f.do("PUT", "/api/webapi/tasks/t2", scenarioTaskJSON(`"scenario":"autonomous","done_criteria":"  深挖下单流程直到无新方向  "`))
	scenario = scenarioOf(t, value["task"].(map[string]any))
	if status != 201 || scenario["key"] != ScenarioAutonomous || scenario["done_criteria"] != "深挖下单流程直到无新方向" || scenario["done_criteria_effective"] != "深挖下单流程直到无新方向" || scenario["is_default"] != false {
		t.Fatalf("create with scenario = %d %s", status, raw)
	}
	if events := scenarioAudits(t, f.capability, "task.create", "t2"); len(events) != 1 || auditDetail(t, events[0])["scenario"] != ScenarioAutonomous {
		t.Fatalf("task.create audit t2 = %+v", events)
	}
	// 幂等重放即使带不同场景也不改已保存值。
	if status, value, raw := f.do("PUT", "/api/webapi/tasks/t2", scenarioTaskJSON(`"scenario":"webapi_pentest"`)); status != 200 || value["idempotent"] != true || scenarioOf(t, value["task"].(map[string]any))["key"] != ScenarioAutonomous {
		t.Fatalf("idempotent replay changed scenario: %d %s", status, raw)
	}

	// PUT /tasks/{id}/scenario 校验。
	for _, body := range []string{`{}`, `{"scenario":"bogus"}`, `{"scenario":"autonomous","extra":1}`, `not json`, `{"done_criteria":"` + long + `"}`} {
		if status, _, raw := f.do("PUT", "/api/webapi/tasks/t1/scenario", body); status != 400 {
			t.Fatalf("invalid scenario body %q accepted: %d %s", body, status, raw)
		}
	}
	if status, _, _ := f.do("PUT", "/api/webapi/tasks/missing/scenario", `{"scenario":"autonomous"}`); status != 404 {
		t.Fatalf("missing task = %d", status)
	}
	if len(scenarioAudits(t, f.capability, "task.scenario", "t1")) != 0 {
		t.Fatal("rejected requests were audited")
	}

	// 只改场景：完成标准沿用（空＝新场景默认）。
	status, value, raw = f.do("PUT", "/api/webapi/tasks/t1/scenario", `{"scenario":"autonomous"}`)
	scenario = scenarioOf(t, value)
	if status != 200 || value["task_id"] != "t1" || scenario["key"] != ScenarioAutonomous || scenario["done_criteria_effective"] != scenarioAutonomousDoneCriteria || scenario["is_default"] != false {
		t.Fatalf("put scenario = %d %s", status, raw)
	}
	// 只改完成标准：场景沿用。
	status, value, raw = f.do("PUT", "/api/webapi/tasks/t1/scenario", `{"done_criteria":"覆盖全部订单接口"}`)
	scenario = scenarioOf(t, value)
	if status != 200 || scenario["key"] != ScenarioAutonomous || scenario["done_criteria"] != "覆盖全部订单接口" {
		t.Fatalf("put criteria = %d %s", status, raw)
	}
	// 相同值再保存：不重复登记。
	if status, _, _ := f.do("PUT", "/api/webapi/tasks/t1/scenario", `{"scenario":"autonomous","done_criteria":"覆盖全部订单接口"}`); status != 200 {
		t.Fatal("same-value put failed")
	}
	events := scenarioAudits(t, f.capability, "task.scenario", "t1")
	if len(events) != 2 {
		t.Fatalf("task.scenario audits = %+v", events)
	}
	first, second := auditDetail(t, events[0]), auditDetail(t, events[1])
	if first["from"] != ScenarioWebAPIPentest || first["to"] != ScenarioAutonomous || first["done_criteria_changed"] != false || events[0].Actor != auditActorUser {
		t.Fatalf("first audit = %v", first)
	}
	if second["from"] != ScenarioAutonomous || second["to"] != ScenarioAutonomous || second["done_criteria_changed"] != true || strings.Contains(events[1].DetailJSON, "覆盖全部订单接口") || strings.Contains(events[1].Summary, "覆盖全部订单接口") {
		t.Fatalf("second audit leaks or wrong = %s / %s", events[1].DetailJSON, events[1].Summary)
	}

	// GET 与会话查询、任务列表都带场景。
	_, value, _ = f.do("GET", "/api/webapi/tasks/t1", "")
	if scenarioOf(t, value)["done_criteria_effective"] != "覆盖全部订单接口" {
		t.Fatalf("get after put = %v", value)
	}
	sessionID := value["initial_session_id"].(string)
	if _, value, _ = f.do("GET", "/api/webapi/tasks?session_id="+sessionID, ""); scenarioOf(t, value)["key"] != ScenarioAutonomous {
		t.Fatalf("by session = %v", value)
	}
	_, value, raw = f.do("GET", "/api/webapi/task-list?limit=200", "")
	items, _ := value["items"].([]any)
	if items == nil {
		items, _ = value["tasks"].([]any)
	}
	found := map[string]map[string]any{}
	for _, item := range items {
		row := item.(map[string]any)
		found[row["task_id"].(string)] = scenarioOf(t, row)
	}
	if found["t1"]["key"] != ScenarioAutonomous || found["t1"]["done_criteria"] != "覆盖全部订单接口" || found["t1"]["is_default"] != false ||
		found["edge"]["key"] != ScenarioWebAPIPentest || found["t2"]["key"] != ScenarioAutonomous {
		t.Fatalf("task-list scenarios = %v (%s)", found, raw)
	}
	// 一个从未设置的任务在列表中为默认。
	if status, _, _ := f.do("PUT", "/api/webapi/tasks/t3", scenarioTaskJSON("")); status != 201 {
		t.Fatal("create t3")
	}
	_, value, _ = f.do("GET", "/api/webapi/task-list?q=", "")
	if !strings.Contains(mustJSON(t, value), `"done_criteria_effective":"`+scenarioPentestDoneCriteria+`","is_default":true`) {
		t.Fatalf("task-list lacks default scenario: %v", value)
	}

	// 场景选项列表。
	_, value, _ = f.do("GET", "/api/webapi/scenarios", "")
	options, _ := value["scenarios"].([]any)
	if len(options) != 2 || options[0].(map[string]any)["key"] != ScenarioWebAPIPentest || options[0].(map[string]any)["is_default"] != true ||
		options[1].(map[string]any)["default_done_criteria"] != scenarioAutonomousDoneCriteria {
		t.Fatalf("scenario options = %v", value)
	}
	if scenarioDoneCriteria(ScenarioWebAPIPentest) != scenarioPentestDoneCriteria || scenarioDoneCriteria("x") != "" {
		t.Fatal("scenarioDoneCriteria")
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

const scenarioAssetExtra = `,"scenario":"autonomous","done_criteria":"穷尽库存与订单相关方向"}`

func TestScenarioAssetDefaultsCopiedToTasks(t *testing.T) {
	f := newScheduleFixture(t, "2026-11-18T09:59:00Z")
	// 未知场景 400，不建资产。
	if status, _, _ := f.do("PUT", "/api/webapi/assets/bad", strings.TrimSuffix(assetBody, "}")+`,"scenario":"nope"}`); status != 400 {
		t.Fatalf("bad asset scenario = %d", status)
	}
	if asset, _ := f.capability.store.assetModel(t.Context(), "bad"); asset != nil {
		t.Fatal("bad asset created")
	}
	// 默认资产：scenario 为默认视图。
	f.createAsset("plain")
	_, value, _ := f.do("GET", "/api/webapi/assets/plain", "")
	if s := scenarioOf(t, value); s["key"] != ScenarioWebAPIPentest || s["is_default"] != true {
		t.Fatalf("plain asset scenario = %v", s)
	}
	// 资产设置默认场景。
	status, value, raw := f.do("PUT", "/api/webapi/assets/a1", strings.TrimSuffix(assetBody, "}")+scenarioAssetExtra)
	if s := scenarioOf(t, value); status != 201 || s["key"] != ScenarioAutonomous || s["done_criteria"] != "穷尽库存与订单相关方向" || s["is_default"] != false {
		t.Fatalf("asset with scenario = %d %s", status, raw)
	}
	// 不带场景字段再保存：保留；审计无场景变化。
	if status, value, raw = f.do("PUT", "/api/webapi/assets/a1", strings.Replace(assetBody, "演示库存系统", "演示库存系统二", 1)); status != 200 || scenarioOf(t, value)["key"] != ScenarioAutonomous {
		t.Fatalf("asset save without scenario reset it: %d %s", status, raw)
	}
	var assetEvents []auditEventModel
	if err := f.capability.store.orm.NewSelect().Model(&assetEvents).Where("asset_id = ? AND action LIKE 'asset.%'", "a1").Order("at ASC", "id ASC").Scan(t.Context()); err != nil || len(assetEvents) != 2 {
		t.Fatalf("asset audits = %+v %v", assetEvents, err)
	}
	if detail := auditDetail(t, assetEvents[1]); strings.Contains(mustJSON(t, detail["changed"]), "scenario") {
		t.Fatalf("unchanged scenario audited: %v", detail)
	}
	// 资产列表也带场景。
	_, value, _ = f.do("GET", "/api/webapi/assets", "")
	if !strings.Contains(mustJSON(t, value), `"key":"autonomous"`) {
		t.Fatalf("asset list lacks scenario: %v", value)
	}

	// 手动从资产发起：拷贝到任务；task.create 记 scenario。
	status, value, raw = f.do("PUT", "/api/webapi/assets/a1/tasks/manual-1", "")
	if status != 201 {
		t.Fatalf("launch = %d %s", status, raw)
	}
	if s := scenarioOf(t, value["task"].(map[string]any)); s["key"] != ScenarioAutonomous || s["done_criteria"] != "穷尽库存与订单相关方向" || s["is_default"] != false {
		t.Fatalf("launched task scenario = %v", s)
	}
	if events := scenarioAudits(t, f.capability, "task.create", "manual-1"); len(events) != 1 || auditDetail(t, events[0])["scenario"] != ScenarioAutonomous {
		t.Fatalf("launch task.create audit = %+v", events)
	}
	// 从默认资产发起：任务也是默认（不写记录）。
	if status, value, raw = f.do("PUT", "/api/webapi/assets/plain/tasks/manual-2", ""); status != 201 || scenarioOf(t, value["task"].(map[string]any))["is_default"] != true {
		t.Fatalf("plain launch = %d %s", status, raw)
	}

	// 资产改场景后：已建任务不变，之后发起的任务按新值。
	if status, _, raw = f.do("PUT", "/api/webapi/assets/a1", strings.TrimSuffix(assetBody, "}")+`,"scenario":"webapi_pentest","done_criteria":""}`); status != 200 {
		t.Fatalf("asset scenario change = %d %s", status, raw)
	}
	if err := f.capability.store.orm.NewSelect().Model(&assetEvents).Where("asset_id = ? AND action = 'asset.update'", "a1").Order("at ASC", "id ASC").Scan(t.Context()); err != nil {
		t.Fatal(err)
	}
	last := auditDetail(t, assetEvents[len(assetEvents)-1])
	if changed := mustJSON(t, last["changed"]); !strings.Contains(changed, `"scenario"`) || !strings.Contains(changed, `"done_criteria"`) || last["values"].(map[string]any)["scenario"] != ScenarioWebAPIPentest ||
		strings.Contains(assetEvents[len(assetEvents)-1].DetailJSON, "穷尽库存") {
		t.Fatalf("asset scenario audit = %v", last)
	}
	if scenario, _ := f.capability.store.scenarioForTask(t.Context(), "manual-1"); scenario.Key != ScenarioAutonomous {
		t.Fatalf("existing task changed with asset: %+v", scenario)
	}
	// 同 task_id 重放发起不改任务场景。
	if status, _, raw = f.do("PUT", "/api/webapi/assets/a1/tasks/manual-1", ""); status != 200 {
		t.Fatalf("relaunch = %d %s", status, raw)
	}
	if scenario, _ := f.capability.store.scenarioForTask(t.Context(), "manual-1"); scenario.Key != ScenarioAutonomous {
		t.Fatalf("relaunch overwrote scenario: %+v", scenario)
	}

	// 计划发起：沿用资产当前默认场景。
	if status, _, raw = f.do("PUT", "/api/webapi/assets/a1", strings.TrimSuffix(assetBody, "}")+scenarioAssetExtra); status != 200 {
		t.Fatalf("asset back to autonomous = %d %s", status, raw)
	}
	f.putSchedule("a1", `{"cron":"0 * * * *"}`)
	f.setNow("2026-11-18T10:00:20Z")
	f.tick()
	scheduled := scheduleTaskID("a1", "2026-11-18T10:00:00Z")
	scenario, err := f.capability.store.scenarioForTask(t.Context(), scheduled)
	if err != nil || scenario.Key != ScenarioAutonomous || scenario.DoneCriteria != "穷尽库存与订单相关方向" || scenario.IsDefault {
		t.Fatalf("scheduled task scenario = %+v %v", scenario, err)
	}
	if events := scenarioAudits(t, f.capability, "task.create", scheduled); len(events) != 1 || events[0].Actor != auditActorSystem || auditDetail(t, events[0])["scenario"] != ScenarioAutonomous {
		t.Fatalf("scheduled task.create audit = %+v", events)
	}
}

// scenarioHookFixture 用真实 hooks.Registry 装载 Cyber 默认提示词扩展与本能力 ToolExtension。
func scenarioHookFixture(t *testing.T, f *assetFixture) (*corehooks.Registry, agentprompt.Resolver) {
	t.Helper()
	registry := corehooks.New()
	var resolver agentprompt.Resolver
	capture := extension.Func{LoadFunc: func(scope *extension.Scope) error {
		value, err := extension.Use[agentprompt.Resolver](scope)
		resolver = value
		return err
	}}
	set, err := extension.New(extension.Provided[*corehooks.Registry](registry), coretool.NewToolRegistry(), promptext.New(), f.capability.ToolExtension(), capture)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close(context.Background()) })
	return registry, resolver
}

func upstreamSystemPrompt(t *testing.T, resolver agentprompt.Resolver) string {
	t.Helper()
	result := resolver.Build(t.Context(), agentprompt.Context{Target: agentprompt.MainSystem, Agent: agentprompt.AgentContext{
		OS: "linux", Arch: "amd64", NodeName: "node-demo",
		Tools: []agentprompt.Tool{{Name: "webapi_http", Description: "Send one HTTP request within the task authorization."}},
	}})
	if len(result.Diagnostics) != 0 || result.Prompt == "" {
		t.Fatalf("upstream prompt build = %+v", result)
	}
	return result.Prompt
}

func emitRunStart(t *testing.T, registry *corehooks.Registry, sessionID, system string) string {
	t.Helper()
	result, err := agenthooks.BeforeRun.Emit(t.Context(), registry, agenthooks.RunStartEvent{SessionID: sessionID, TurnID: "turn-1", SystemPrompt: system})
	if err != nil {
		t.Fatal(err)
	}
	if result.SystemPrompt == nil {
		return system
	}
	return *result.SystemPrompt
}

func TestScenarioBeforeRunInjectsResearchSessionOnly(t *testing.T) {
	f := newAssetFixture(t)
	registry, resolver := scenarioHookFixture(t, f)
	base := upstreamSystemPrompt(t, resolver)
	if !strings.HasPrefix(base, upstreamDefaultIdentity) {
		t.Fatalf("upstream identity changed; update upstreamDefaultIdentity:\n%s", base)
	}
	if status, _, raw := f.do("PUT", "/api/webapi/tasks/t1", scenarioTaskJSON("")); status != 201 {
		t.Fatalf("create = %d %s", status, raw)
	}
	task, _ := f.capability.store.Get(t.Context(), "t1")

	// 默认场景（webapi_pentest、scope=origins）。
	got := emitRunStart(t, registry, task.InitialSessionID, base)
	if os.Getenv("WEBAPI_SCENARIO_RENDER_OUT") != "" {
		if err := os.WriteFile(os.Getenv("WEBAPI_SCENARIO_RENDER_OUT"), []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{
		scenarioIdentityPrompt, "## Environment", "### webapi_http",
		"## 授权与边界\n\n- " + scenarioScopeOriginsRule, "资产登记中的“禁止行为”必须遵守",
		"## 黑板纪律", "`webapi_board_read summary`", "`webapi_board_map`", "`webapi_board_fact`", "`webapi_board_oracle propose`", "`webapi_finding`",
		"## 证据标准", "## 研究场景：Web/API 渗透测试", "6. 收尾",
		"## 完成标准\n\n完成标准：" + scenarioPentestDoneCriteria,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("injected prompt lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, upstreamDefaultIdentity) || !strings.HasPrefix(got, scenarioIdentityPrompt) || strings.Contains(got, scenarioScopeUnrestrictedRule) || strings.Contains(got, "## 研究场景：自主研究") {
		t.Fatalf("identity not replaced or wrong variant:\n%s", got)
	}
	if strings.Index(got, "## Environment") > strings.Index(got, "## 授权与边界") {
		t.Fatal("scenario sections must be appended after upstream sections")
	}
	// 已注入的提示词再经过一次：不重复。
	if again := emitRunStart(t, registry, task.InitialSessionID, got); again != got {
		t.Fatal("double injection")
	}

	// 改场景 + 完成标准 + scope=unrestricted：下一次 Run 生效。
	if status, _, _ := f.do("PUT", "/api/webapi/tasks/t1/scenario", `{"scenario":"autonomous","done_criteria":"订单与库存方向都已结论"}`); status != 200 {
		t.Fatal("put scenario")
	}
	if status, _, _ := f.do("PUT", "/api/webapi/tasks/t1/policy", `{"scope":"unrestricted","approval":"high_risk"}`); status != 200 {
		t.Fatal("put policy")
	}
	got = emitRunStart(t, registry, task.InitialSessionID, base)
	if !strings.Contains(got, "## 授权与边界\n\n- "+scenarioScopeUnrestrictedRule) || strings.Contains(got, scenarioScopeOriginsRule) ||
		!strings.Contains(got, "## 研究场景：自主研究") || strings.Contains(got, "## 研究场景：Web/API 渗透测试") ||
		!strings.Contains(got, "完成标准：订单与库存方向都已结论") || strings.Contains(got, scenarioAutonomousDoneCriteria) ||
		!strings.Contains(got, "## 黑板纪律") || !strings.Contains(got, "危险操作（删除、批量修改、影响可用性的请求）会被平台挂起等人审批") {
		t.Fatalf("unrestricted/autonomous variant:\n%s", got)
	}

	// 身份句找不到时前置。
	custom := "Custom identity.\n\n## Environment\n\nOperating System: linux"
	if got := emitRunStart(t, registry, task.InitialSessionID, custom); !strings.HasPrefix(got, scenarioIdentityPrompt+"\n\nCustom identity.") || !strings.Contains(got, "## 黑板纪律") {
		t.Fatalf("prepend identity:\n%s", got)
	}

	// 验证会话（映射到同任务）、未知会话、空会话：原样。
	if err := f.capability.store.write(t.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&verificationModel{ID: "v1", TaskID: "t1", FindingID: "wf_demo", SourceRevisionID: "wr_demo", SessionID: "verify-session", CreatedAt: "2026-11-20T00:00:00Z"}).Exec(ctx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if mapped, err := f.capability.store.taskForSession(t.Context(), "verify-session"); err != nil || mapped == nil || mapped.ID != "t1" {
		t.Fatalf("verification session fixture not mapped: %v %v", mapped, err)
	}
	for _, sessionID := range []string{"verify-session", "unknown-session", ""} {
		if got := emitRunStart(t, registry, sessionID, base); got != base {
			t.Fatalf("session %q modified:\n%s", sessionID, got)
		}
	}
}
