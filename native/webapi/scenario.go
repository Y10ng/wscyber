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

	// MaxRounds 存 webapi_task_explore（M3），不是本表列；0＝未保存（用场景默认）。
	MaxRounds int `bun:"-"`
}

// assetScenarioModel 是资产默认场景（发起任务与计划发起时拷贝到任务）；无记录＝默认。
// 资产表不加列：本项目 schema 升级只补表不 ALTER。
type assetScenarioModel struct {
	bun.BaseModel `bun:"table:webapi_asset_scenarios,alias:webapi_asset_scenario"`

	AssetID      string `bun:"asset_id,pk"`
	Scenario     string `bun:"scenario,notnull"`
	DoneCriteria string `bun:"done_criteria,type:text,notnull"`
	UpdatedAt    string `bun:"updated_at,notnull"`

	// MaxRounds 存 webapi_asset_explore（M3），不是本表列；0＝未保存（用场景默认）。
	MaxRounds int `bun:"-"`
}

// TaskScenario 是任务/资产响应中的场景视图。DoneCriteria 为保存的自定义文本（空＝场景默认），
// DoneCriteriaEffective 为实际生效文本；IsDefault=无保存记录。
type TaskScenario struct {
	Key                   string `json:"key"`
	Name                  string `json:"name"`
	DoneCriteria          string `json:"done_criteria"`
	DoneCriteriaEffective string `json:"done_criteria_effective"`
	IsDefault             bool   `json:"is_default"`
	// MaxRounds 是持续探索（Goal 评估循环）最大轮数的生效值：保存值或场景默认（M3，见 explore.go）。
	MaxRounds int `json:"max_rounds"`
}

func newTaskScenario(key, criteria string, isDefault bool) TaskScenario {
	effective := criteria
	if strings.TrimSpace(effective) == "" {
		effective = scenarioDoneCriteria(key)
	}
	return TaskScenario{Key: key, Name: scenarioName(key), DoneCriteria: criteria, DoneCriteriaEffective: effective, IsDefault: isDefault, MaxRounds: scenarioMaxRounds(key)}
}

// withMaxRounds 用保存的最大轮数覆盖场景默认；saved 不在 1..20（含 0＝未保存）时保持默认。
func (s TaskScenario) withMaxRounds(saved int) TaskScenario {
	if validMaxRounds(saved) {
		s.MaxRounds = saved
	}
	return s
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
	MaxRounds    *int    `json:"max_rounds"`
}

func (in scenarioInput) provided() bool {
	return in.Scenario != nil || in.DoneCriteria != nil || in.MaxRounds != nil
}

// rounds 校验提供的 max_rounds（1..20）；未提供返回 0（沿用当前值）。
func (in scenarioInput) rounds() (int, error) {
	if in.MaxRounds == nil {
		return 0, nil
	}
	if !validMaxRounds(*in.MaxRounds) {
		return 0, errMaxRounds
	}
	return *in.MaxRounds, nil
}

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
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&model).Where("task_id = ?", taskID).Limit(1).Scan(ctx)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return defaultTaskScenario(), nil
	}
	if err != nil {
		return TaskScenario{}, err
	}
	if _, ok := scenarioDefOf(model.Scenario); !ok {
		return TaskScenario{}, fmt.Errorf("已保存研究场景非法")
	}
	rounds, err := readValue(ctx, s, func(ctx context.Context, db bun.IDB) (int, error) { return taskMaxRounds(ctx, db, taskID) })
	if err != nil {
		return TaskScenario{}, err
	}
	return newTaskScenario(model.Scenario, model.DoneCriteria, false).withMaxRounds(rounds), nil
}

// putTaskScenarioTx 写任务当前场景（upsert）；maxRounds>0 时同时写最大轮数，0＝不动已保存值。
// 可在建任务事务内调用。
func putTaskScenarioTx(ctx context.Context, db bun.IDB, taskID, key, criteria string, maxRounds int) error {
	model := &taskScenarioModel{TaskID: taskID, Scenario: key, DoneCriteria: criteria, UpdatedAt: scenarioNow()}
	_, err := db.NewInsert().Model(model).
		On("CONFLICT (task_id) DO UPDATE").Set("scenario = EXCLUDED.scenario").Set("done_criteria = EXCLUDED.done_criteria").Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil || maxRounds <= 0 {
		return err
	}
	return putTaskMaxRounds(ctx, db, taskID, maxRounds)
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
	rounds, err := assetMaxRounds(ctx, db, assetID)
	if err != nil {
		return nil, err
	}
	model.MaxRounds = rounds[assetID]
	return &model, nil
}

func (m *assetScenarioModel) view() TaskScenario {
	if m == nil {
		return defaultTaskScenario()
	}
	return newTaskScenario(m.Scenario, m.DoneCriteria, false).withMaxRounds(m.MaxRounds)
}

// assetScenarios 批量读资产默认场景（无记录的资产不在结果中）。
func (s *taskStore) assetScenarios(ctx context.Context, assetIDs ...string) (map[string]*assetScenarioModel, error) {
	out := map[string]*assetScenarioModel{}
	if len(assetIDs) == 0 {
		return out, nil
	}
	var models []assetScenarioModel
	if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&models).Where("asset_id IN (?)", bun.In(assetIDs)).Scan(ctx)
	}); err != nil {
		return nil, err
	}
	rounds, err := readValue(ctx, s, func(ctx context.Context, db bun.IDB) (map[string]int, error) {
		return assetMaxRounds(ctx, db, assetIDs...)
	})
	if err != nil {
		return nil, err
	}
	for index := range models {
		models[index].MaxRounds = rounds[models[index].AssetID]
		out[models[index].AssetID] = &models[index]
	}
	return out, nil
}

// putAssetScenario 写资产默认场景；maxRounds>0 时同一事务写最大轮数，0＝不动已保存值。
func (s *taskStore) putAssetScenario(ctx context.Context, assetID, key, criteria string, maxRounds int) error {
	return s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		model := &assetScenarioModel{AssetID: assetID, Scenario: key, DoneCriteria: criteria, UpdatedAt: scenarioNow()}
		_, err := tx.NewInsert().Model(model).
			On("CONFLICT (asset_id) DO UPDATE").Set("scenario = EXCLUDED.scenario").Set("done_criteria = EXCLUDED.done_criteria").Set("updated_at = EXCLUDED.updated_at").
			Exec(ctx)
		if err != nil || maxRounds <= 0 {
			return err
		}
		return putAssetMaxRounds(ctx, tx, assetID, maxRounds)
	})
}

// ---- HTTP ----

// handlePutTaskScenario：PUT /tasks/{id}/scenario {scenario?, done_criteria?, max_rounds?}；未提供的字段沿用当前值，
// 至少提供一个；随时可改，下一次 Run 生效。
func (c *Capability) handlePutTaskScenario(w http.ResponseWriter, r *http.Request) {
	id, err := taskIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var body scenarioInput
	if err := decodeJSONBody(r, &body, true, false); err != nil {
		writeError(w, http.StatusBadRequest, "请求体须为只含 scenario、done_criteria、max_rounds 的 JSON 对象（max_rounds 为整数）")
		return
	}
	if !body.provided() {
		writeError(w, http.StatusBadRequest, "scenario、done_criteria、max_rounds 至少提供一个")
		return
	}
	rounds, err := body.rounds()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
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
	err = c.store.write(r.Context(), nil, func(ctx context.Context, tx bun.Tx) error {
		return putTaskScenarioTx(ctx, tx, id, key, criteria, rounds)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存研究场景失败")
		return
	}
	after, err := c.store.scenarioForTask(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取研究场景失败")
		return
	}
	c.auditTaskScenario(r.Context(), id, before, after)
	writeJSON(w, http.StatusOK, map[string]any{"task_id": id, "scenario": after})
}

// ScenarioOption 是 GET /api/webapi/scenarios 的项（新建任务/资产表单用）。
type ScenarioOption struct {
	Key                 string `json:"key"`
	Name                string `json:"name"`
	DefaultDoneCriteria string `json:"default_done_criteria"`
	DefaultMaxRounds    int    `json:"default_max_rounds"`
	IsDefault           bool   `json:"is_default"`
}

func (c *Capability) handleScenarioList(w http.ResponseWriter, _ *http.Request) {
	items := make([]ScenarioOption, 0, len(scenarioDefs))
	for _, def := range scenarioDefs {
		items = append(items, ScenarioOption{Key: def.Key, Name: def.Name, DefaultDoneCriteria: def.DoneCriteria, DefaultMaxRounds: def.MaxRounds, IsDefault: def.Key == ScenarioWebAPIPentest})
	}
	writeJSON(w, http.StatusOK, map[string]any{"scenarios": items})
}

// ---- 审计 ----

func (c *Capability) auditTaskScenario(ctx context.Context, taskID string, from, to TaskScenario) {
	criteriaChanged := from.DoneCriteria != to.DoneCriteria
	roundsChanged := from.MaxRounds != to.MaxRounds
	if from.Key == to.Key && !criteriaChanged && !roundsChanged {
		return
	}
	parts := []string{}
	if from.Key != to.Key {
		parts = append(parts, "研究场景 "+from.Name+" → "+to.Name)
	}
	if criteriaChanged {
		parts = append(parts, "完成标准已修改")
	}
	detail := map[string]any{"from": from.Key, "to": to.Key, "done_criteria_changed": criteriaChanged}
	if roundsChanged {
		parts = append(parts, fmt.Sprintf("最大轮数 %d → %d", from.MaxRounds, to.MaxRounds))
		detail["max_rounds"] = map[string]any{"from": from.MaxRounds, "to": to.MaxRounds}
	}
	assetID, _ := c.store.taskAssetID(ctx, taskID)
	c.audit(ctx, auditEntry{Actor: auditActorOf(ctx), Action: "task.scenario", TargetKind: "task", TargetID: taskID, TaskID: taskID, AssetID: assetID,
		Summary: "修改任务" + strings.Join(parts, "；"),
		Detail:  detail})
}

// ---- 研究会话系统提示词注入 ----

// researchTaskForSession 只认任务研究会话（initial_session_id）；验证/复测/报告/普通会话返回 nil。
func (s *taskStore) researchTaskForSession(ctx context.Context, sessionID string) (*Task, error) {
	if sessionID == "" {
		return nil, nil
	}
	var model taskModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&model).Where("initial_session_id = ?", sessionID).Limit(1).Scan(ctx)
	})
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
