package webapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	agenthooks "github.com/chainreactors/cyber/agent/hooks"
	"github.com/uptrace/bun"
)

// 研究场景（M1）：任务的场景与完成标准；只由人经鉴权 API 设置，研究会话每次 Run 读库注入系统提示词。
// 契约见 native/SCENARIO.md。

const (
	ScenarioWebAPIPentest = "webapi_pentest" // 默认
	ScenarioAutonomous    = "autonomous"

	maxDoneCriteriaRunes = 2000
)

var errScenarioKey = errors.New("scenario 只能是 webapi_pentest 或 autonomous")

// taskScenarioModel 是任务当前场景；无记录＝默认场景与默认完成标准。
type taskScenarioModel struct {
	bun.BaseModel `bun:"table:webapi_task_scenarios,alias:webapi_task_scenario"`

	TaskID       string `bun:"task_id,pk"`
	Scenario     string `bun:"scenario,notnull"`
	DoneCriteria string `bun:"done_criteria,type:text,notnull"`
	UpdatedAt    string `bun:"updated_at,notnull"`
}

// assetScenarioModel 是资产默认场景（发起任务与计划发起时拷贝到任务）；无记录＝默认。
// 资产表不加列：本项目 schema 升级只补表不 ALTER。
type assetScenarioModel struct {
	bun.BaseModel `bun:"table:webapi_asset_scenarios,alias:webapi_asset_scenario"`

	AssetID      string `bun:"asset_id,pk"`
	Scenario     string `bun:"scenario,notnull"`
	DoneCriteria string `bun:"done_criteria,type:text,notnull"`
	UpdatedAt    string `bun:"updated_at,notnull"`
}

// TaskScenario 是任务/资产响应中的场景视图。DoneCriteria 为保存的自定义文本（空＝场景默认），
// DoneCriteriaEffective 为实际生效文本；IsDefault=无保存记录。
type TaskScenario struct {
	Key                   string `json:"key"`
	Name                  string `json:"name"`
	DoneCriteria          string `json:"done_criteria"`
	DoneCriteriaEffective string `json:"done_criteria_effective"`
	IsDefault             bool   `json:"is_default"`
}

func newTaskScenario(key, criteria string, isDefault bool) TaskScenario {
	effective := criteria
	if strings.TrimSpace(effective) == "" {
		effective = scenarioDoneCriteria(key)
	}
	return TaskScenario{Key: key, Name: scenarioName(key), DoneCriteria: criteria, DoneCriteriaEffective: effective, IsDefault: isDefault}
}

func defaultTaskScenario() TaskScenario { return newTaskScenario(ScenarioWebAPIPentest, "", true) }

// parseScenarioKey：去首尾空白；空＝默认；未知值报错。
func parseScenarioKey(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return ScenarioWebAPIPentest, nil
	}
	if _, ok := scenarioDefOf(key); !ok {
		return "", errScenarioKey
	}
	return key, nil
}

// parseDoneCriteria：去首尾空白，≤2000 字；空＝场景默认。
func parseDoneCriteria(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	if utf8.RuneCountInString(text) > maxDoneCriteriaRunes {
		return "", fmt.Errorf("done_criteria 不能超过 %d 字", maxDoneCriteriaRunes)
	}
	return text, nil
}

// scenarioInput 是请求体中的可选场景字段（nil＝未提供）。
type scenarioInput struct {
	Scenario     *string `json:"scenario"`
	DoneCriteria *string `json:"done_criteria"`
}

func (in scenarioInput) provided() bool { return in.Scenario != nil || in.DoneCriteria != nil }

// resolve 在 base 之上应用提供的字段并校验；未提供的字段沿用 base。
func (in scenarioInput) resolve(baseKey, baseCriteria string) (key, criteria string, err error) {
	key, criteria = baseKey, baseCriteria
	if in.Scenario != nil {
		if key, err = parseScenarioKey(*in.Scenario); err != nil {
			return "", "", err
		}
	}
	if in.DoneCriteria != nil {
		if criteria, err = parseDoneCriteria(*in.DoneCriteria); err != nil {
			return "", "", err
		}
	}
	return key, criteria, nil
}

// ---- 存储 ----

func scenarioNow() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// scenarioForTask 每次读库：设置后下一次 Run 生效。
func (s *taskStore) scenarioForTask(ctx context.Context, taskID string) (TaskScenario, error) {
	var model taskScenarioModel
	err := s.orm.NewSelect().Model(&model).Where("task_id = ?", taskID).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultTaskScenario(), nil
	}
	if err != nil {
		return TaskScenario{}, err
	}
	if _, ok := scenarioDefOf(model.Scenario); !ok {
		return TaskScenario{}, fmt.Errorf("已保存研究场景非法")
	}
	return newTaskScenario(model.Scenario, model.DoneCriteria, false), nil
}

// putTaskScenarioTx 写任务当前场景（upsert）；可在建任务事务内调用。
func putTaskScenarioTx(ctx context.Context, db bun.IDB, taskID, key, criteria string) error {
	model := &taskScenarioModel{TaskID: taskID, Scenario: key, DoneCriteria: criteria, UpdatedAt: scenarioNow()}
	_, err := db.NewInsert().Model(model).
		On("CONFLICT (task_id) DO UPDATE").Set("scenario = EXCLUDED.scenario").Set("done_criteria = EXCLUDED.done_criteria").Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

// assetScenarioModelOf 读资产默认场景记录；无记录返回 nil。
func assetScenarioModelOf(ctx context.Context, db bun.IDB, assetID string) (*assetScenarioModel, error) {
	var model assetScenarioModel
	err := db.NewSelect().Model(&model).Where("asset_id = ?", assetID).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (m *assetScenarioModel) view() TaskScenario {
	if m == nil {
		return defaultTaskScenario()
	}
	return newTaskScenario(m.Scenario, m.DoneCriteria, false)
}

// assetScenarios 批量读资产默认场景（无记录的资产不在结果中）。
func (s *taskStore) assetScenarios(ctx context.Context, assetIDs ...string) (map[string]*assetScenarioModel, error) {
	out := map[string]*assetScenarioModel{}
	if len(assetIDs) == 0 {
		return out, nil
	}
	var models []assetScenarioModel
	if err := s.orm.NewSelect().Model(&models).Where("asset_id IN (?)", bun.In(assetIDs)).Scan(ctx); err != nil {
		return nil, err
	}
	for index := range models {
		out[models[index].AssetID] = &models[index]
	}
	return out, nil
}

func (s *taskStore) putAssetScenario(ctx context.Context, assetID, key, criteria string) error {
	model := &assetScenarioModel{AssetID: assetID, Scenario: key, DoneCriteria: criteria, UpdatedAt: scenarioNow()}
	_, err := s.orm.NewInsert().Model(model).
		On("CONFLICT (asset_id) DO UPDATE").Set("scenario = EXCLUDED.scenario").Set("done_criteria = EXCLUDED.done_criteria").Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	return err
}

// ---- HTTP ----

// handlePutTaskScenario：PUT /tasks/{id}/scenario {scenario?, done_criteria?}；未提供的字段沿用当前值，
// 至少提供一个；随时可改，下一次 Run 生效。
func (c *Capability) handlePutTaskScenario(w http.ResponseWriter, r *http.Request) {
	id, err := taskIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var body scenarioInput
	if err := decodeJSONBody(r, &body, true, false); err != nil {
		writeError(w, http.StatusBadRequest, "请求体须为只含 scenario、done_criteria 的 JSON 对象")
		return
	}
	if !body.provided() {
		writeError(w, http.StatusBadRequest, "scenario 与 done_criteria 至少提供一个")
		return
	}
	task, err := c.store.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务失败")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	before, err := c.store.scenarioForTask(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取研究场景失败")
		return
	}
	key, criteria, err := body.resolve(before.Key, before.DoneCriteria)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := putTaskScenarioTx(r.Context(), c.store.orm, id, key, criteria); err != nil {
		writeError(w, http.StatusInternalServerError, "保存研究场景失败")
		return
	}
	after := newTaskScenario(key, criteria, false)
	c.auditTaskScenario(r.Context(), id, before, after)
	writeJSON(w, http.StatusOK, map[string]any{"task_id": id, "scenario": after})
}

// ScenarioOption 是 GET /api/webapi/scenarios 的项（新建任务/资产表单用）。
type ScenarioOption struct {
	Key                 string `json:"key"`
	Name                string `json:"name"`
	DefaultDoneCriteria string `json:"default_done_criteria"`
	IsDefault           bool   `json:"is_default"`
}

func (c *Capability) handleScenarioList(w http.ResponseWriter, _ *http.Request) {
	items := make([]ScenarioOption, 0, len(scenarioDefs))
	for _, def := range scenarioDefs {
		items = append(items, ScenarioOption{Key: def.Key, Name: def.Name, DefaultDoneCriteria: def.DoneCriteria, IsDefault: def.Key == ScenarioWebAPIPentest})
	}
	writeJSON(w, http.StatusOK, map[string]any{"scenarios": items})
}

// ---- 审计 ----

func (c *Capability) auditTaskScenario(ctx context.Context, taskID string, from, to TaskScenario) {
	criteriaChanged := from.DoneCriteria != to.DoneCriteria
	if from.Key == to.Key && !criteriaChanged {
		return
	}
	parts := []string{}
	if from.Key != to.Key {
		parts = append(parts, "研究场景 "+from.Name+" → "+to.Name)
	}
	if criteriaChanged {
		parts = append(parts, "完成标准已修改")
	}
	assetID, _ := c.store.taskAssetID(ctx, taskID)
	c.audit(ctx, auditEntry{Actor: auditActorOf(ctx), Action: "task.scenario", TargetKind: "task", TargetID: taskID, TaskID: taskID, AssetID: assetID,
		Summary: "修改任务" + strings.Join(parts, "；"),
		Detail:  map[string]any{"from": from.Key, "to": to.Key, "done_criteria_changed": criteriaChanged}})
}

// ---- 研究会话系统提示词注入 ----

// researchTaskForSession 只认任务研究会话（initial_session_id）；验证/复测/报告/普通会话返回 nil。
func (s *taskStore) researchTaskForSession(ctx context.Context, sessionID string) (*Task, error) {
	if sessionID == "" {
		return nil, nil
	}
	var model taskModel
	err := s.orm.NewSelect().Model(&model).Where("initial_session_id = ?", sessionID).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model.toTask()
}

// scenarioBeforeRun 是 BeforeRun 处理：研究会话按任务场景与测试策略改写系统提示词；
// 其他会话或读库失败时保持原样（失败只记日志，不阻断运行）。
func (c *Capability) scenarioBeforeRun(ctx context.Context, event agenthooks.RunStartEvent) (agenthooks.RunStartResult, error) {
	task, err := c.store.researchTaskForSession(ctx, event.SessionID)
	if err != nil {
		log.Printf("webapi 场景注入：读取会话任务失败：%v", err)
		return agenthooks.RunStartResult{}, nil
	}
	if task == nil {
		return agenthooks.RunStartResult{}, nil
	}
	scenario, err := c.store.scenarioForTask(ctx, task.ID)
	if err != nil {
		log.Printf("webapi 场景注入：读取任务 %s 研究场景失败：%v", task.ID, err)
		return agenthooks.RunStartResult{}, nil
	}
	policy, err := c.store.policyForTask(ctx, task.ID)
	if err != nil {
		log.Printf("webapi 场景注入：读取任务 %s 测试策略失败：%v", task.ID, err)
		return agenthooks.RunStartResult{}, nil
	}
	system := injectScenarioPrompt(event.SystemPrompt, scenario, policy.Scope)
	if system == event.SystemPrompt {
		return agenthooks.RunStartResult{}, nil
	}
	return agenthooks.RunStartResult{SystemPrompt: &system}, nil
}
