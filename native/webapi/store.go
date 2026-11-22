package webapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"

	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

// taskModel 是 webapi_tasks 的关系列投影，只操作此业务表。
type taskModel struct {
	bun.BaseModel `bun:"table:webapi_tasks,alias:webapi_task"`

	ID               string `bun:"id,pk"`
	KnownInfo        string `bun:"known_info,type:text,notnull"`
	Objective        string `bun:"objective,type:text,notnull"`
	OriginsJSON      string `bun:"origins_json,type:text,notnull"`
	InitialSessionID string `bun:"initial_session_id,notnull"`
	CreatedAt        string `bun:"created_at,notnull"`
}

// requestModel 的 Flow 只进本库受保护业务行，不进入全局 Artifact archive。
type requestModel struct {
	bun.BaseModel `bun:"table:webapi_requests,alias:webapi_request"`

	ID               string         `bun:"id,pk"`
	TaskID           string         `bun:"task_id,notnull"`
	SessionID        string         `bun:"session_id,notnull"`
	TurnID           string         `bun:"turn_id,notnull"`
	CallID           string         `bun:"call_id,notnull"`
	Fingerprint      string         `bun:"fingerprint,notnull"`
	Method           string         `bun:"method,notnull"`
	URL              string         `bun:"url,notnull"`
	Status           string         `bun:"status,notnull"`
	Reason           string         `bun:"reason,type:text,notnull"`
	Sent             sql.NullBool   `bun:"sent"`
	StatusCode       sql.NullInt64  `bun:"status_code"`
	Preview          []byte         `bun:"preview,type:blob,notnull"`
	PreviewTruncated bool           `bun:"preview_truncated,notnull"`
	CaptureTruncated bool           `bun:"capture_truncated,notnull"`
	FlowPB           []byte         `bun:"flow_pb,type:blob,notnull"`
	CreatedAt        string         `bun:"created_at,notnull"`
	CompletedAt      sql.NullString `bun:"completed_at"`
}

// SchemaModule 是本能力新增的表声明；Hub 建库时并入唯一 schema。
// 既有相邻测试重开同一 hub.sqlite 时可传该模块匹配新 schema。
func SchemaModule() webservice.SchemaModule {
	return webservice.SchemaModule{
		Name:   "webapi",
		Models: []any{(*taskModel)(nil), (*requestModel)(nil), (*findingModel)(nil), (*findingRevisionModel)(nil), (*verificationModel)(nil), (*verificationResultModel)(nil), (*reportModel)(nil), (*reportDraftModel)(nil), (*retestModel)(nil), (*retestResultModel)(nil), (*taskPolicyModel)(nil), (*taskPolicyEventModel)(nil), (*assetModel)(nil), (*assetCredentialModel)(nil), (*assetTaskModel)(nil), (*assetScheduleModel)(nil), (*scheduleRunModel)(nil), (*scheduleOptionsModel)(nil), (*scheduleRunFollowupModel)(nil), (*scheduleFollowupModel)(nil), (*assetVulnModel)(nil), (*assetVulnMemberModel)(nil), (*egressFlowModel)(nil), (*notifySettingsModel)(nil), (*notificationModel)(nil), (*auditEventModel)(nil), (*vulnTriageModel)(nil), (*vulnSLAModel)(nil), (*boardEndpointModel)(nil), (*boardIngestModel)(nil), (*boardFactModel)(nil), (*boardOracleModel)(nil), (*boardOracleEventModel)(nil), (*taskScenarioModel)(nil), (*assetScenarioModel)(nil), (*taskExploreModel)(nil), (*assetExploreModel)(nil), (*replayModel)(nil), (*subagentReservationModel)(nil), (*taskSubsessionModel)(nil), (*subsessionUsageModel)(nil), (*authzCompareModel)(nil), (*notifyOpsSettingsModel)(nil), (*notificationOpsModel)(nil)},
		Tables: map[string][]string{
			"webapi_tasks":                  {"id", "known_info", "objective", "origins_json", "initial_session_id", "created_at"},
			"webapi_requests":               {"id", "task_id", "session_id", "turn_id", "call_id", "fingerprint", "method", "url", "status", "reason", "sent", "status_code", "preview", "preview_truncated", "capture_truncated", "flow_pb", "created_at", "completed_at"},
			"webapi_findings":               {"id", "task_id", "number", "status", "current_revision", "current_revision_id", "created_at", "updated_at"},
			"webapi_finding_revisions":      {"id", "finding_id", "task_id", "number", "revision", "session_id", "turn_id", "call_id", "payload_json", "title", "created_at"},
			"webapi_verifications":          {"id", "task_id", "finding_id", "source_revision_id", "session_id", "created_at"},
			"webapi_verification_results":   {"verification_id", "session_id", "turn_id", "call_id", "payload_json", "created_at"},
			"webapi_reports":                {"id", "task_id", "session_id", "items_json", "created_at"},
			"webapi_report_drafts":          {"report_id", "session_id", "turn_id", "call_id", "payload_json", "created_at"},
			"webapi_retests":                {"id", "task_id", "finding_id", "source_revision_id", "session_id", "created_at"},
			"webapi_retest_results":         {"retest_id", "session_id", "turn_id", "call_id", "payload_json", "created_at"},
			"webapi_task_policies":          {"task_id", "scope", "approval", "updated_at"},
			"webapi_task_policy_events":     {"id", "task_id", "scope", "approval", "created_at"},
			"webapi_assets":                 {"id", "name", "owner", "environment", "known_info", "objective", "origins_json", "forbidden", "scope", "approval", "archived", "created_at", "updated_at"},
			"webapi_asset_credentials":      {"id", "asset_id", "label", "username", "secret_cipher", "note", "created_at", "updated_at"},
			"webapi_asset_tasks":            {"task_id", "asset_id", "created_at"},
			"webapi_asset_schedules":        {"asset_id", "cron", "enabled", "next_run_at", "updated_at"},
			"webapi_schedule_runs":          {"id", "asset_id", "scheduled_for", "task_id", "status", "error", "created_at"},
			"webapi_schedule_options":       {"asset_id", "auto_verify", "auto_retest", "updated_at"},
			"webapi_schedule_run_followups": {"run_id", "asset_id", "task_id", "auto_verify", "auto_retest", "planned_at", "completed_at"},
			"webapi_schedule_followups":     {"id", "run_id", "kind", "task_id", "finding_id", "revision_id", "target_id", "status", "error", "created_at", "updated_at"},
			"webapi_asset_vulns":            {"id", "asset_id", "endpoint_key", "title_override", "created_at", "updated_at"},
			"webapi_asset_vuln_members":     {"finding_id", "vuln_id", "asset_id", "task_id", "link", "created_at", "updated_at"},
			"webapi_egress_flows":           {"id", "task_id", "session_id", "turn_id", "call_id", "tool", "method", "url", "host", "decision", "sent", "http_status", "error", "flow_pb", "request_bytes", "response_bytes", "preview_truncated", "created_at", "completed_at"},
			"webapi_notify_settings":        {"id", "webhook_cipher", "enabled", "require_confirm", "base_url", "updated_at"},
			"webapi_notifications":          {"id", "dedupe_key", "kind", "task_id", "finding_id", "verification_id", "vuln_id", "status", "attempts", "next_attempt_at", "last_error", "created_at", "sent_at"},
			"webapi_audit_events":           {"id", "at", "actor", "action", "target_kind", "target_id", "task_id", "asset_id", "summary", "detail_json"},
			"webapi_vuln_triage":            {"vuln_key", "asset_id", "severity", "disposition", "owner", "due_date", "note", "created_at", "updated_at"},
			"webapi_vuln_sla":               {"id", "critical_days", "high_days", "medium_days", "low_days", "updated_at"},
			"webapi_board_endpoints":        {"id", "task_id", "endpoint_key", "method", "origin", "path", "params_json", "status_codes_json", "auth_seen", "request_count", "first_seen_at", "last_seen_at", "source", "note"},
			"webapi_board_ingest":           {"task_id", "last_created_at", "last_id"},
			"webapi_board_facts":            {"id", "task_id", "kind", "subject", "statement", "confidence", "evidence_ids_json", "status", "session_id", "turn_id", "call_id", "created_at", "updated_at"},
			"webapi_board_oracles":          {"id", "task_id", "number", "question", "category", "endpoint_keys_json", "priority", "status", "origin", "claimed_session_id", "result_summary", "evidence_ids_json", "finding_id", "dedupe_key", "created_at", "updated_at"},
			"webapi_board_oracle_events":    {"id", "oracle_id", "task_id", "from_status", "to_status", "actor", "session_id", "at", "summary"},
			"webapi_task_scenarios":         {"task_id", "scenario", "done_criteria", "updated_at"},
			"webapi_asset_scenarios":        {"asset_id", "scenario", "done_criteria", "updated_at"},
			"webapi_task_explore":           {"task_id", "max_rounds", "updated_at"},
			"webapi_asset_explore":          {"asset_id", "max_rounds", "updated_at"},
			"webapi_replays":                {"id", "vuln_key", "step", "task_id", "evidence_id", "original_evidence_id", "method", "url", "status_code", "verdict_hint", "created_at"},
			"webapi_subagent_reservations":  {"parent_session_id", "parent_call_id", "task_id", "oracle_id", "created_at"},
			"webapi_task_subsessions":       {"session_id", "task_id", "parent_session_id", "parent_call_id", "oracle_id", "agent_name", "status", "stop_reason", "error", "created_at", "ended_at"},
			"webapi_subsession_usage":       {"id", "session_id", "task_id", "model", "input_tokens", "output_tokens", "total_tokens", "cache_read_tokens", "cache_write_tokens", "created_at"},
			"webapi_authz_compares":         {"id", "task_id", "session_id", "original_evidence_id", "label", "variant_evidence_id", "status", "hint", "http_status", "created_at"},
			"webapi_notify_ops_settings":    {"id", "notify_approval", "notify_schedule_failed", "notify_weekly_digest", "weekly_digest_since", "updated_at"},
			"webapi_notification_ops":       {"notification_id", "title", "asset_id", "session_id", "payload_json"},
		},
		Indexes: []webservice.SchemaIndex{
			{Model: (*requestModel)(nil), Name: "webapi_requests_call", Columns: []string{"task_id", "turn_id", "call_id"}, Unique: true},
			{Model: (*requestModel)(nil), Name: "webapi_requests_task_created", Expr: "task_id, created_at DESC"},
			{Model: (*requestModel)(nil), Name: "webapi_requests_unknown", Columns: []string{"task_id", "fingerprint", "status"}},
			{Model: (*findingModel)(nil), Name: "webapi_findings_number", Columns: []string{"number"}, Unique: true},
			{Model: (*findingRevisionModel)(nil), Name: "webapi_finding_revisions_call", Columns: []string{"task_id", "session_id", "turn_id", "call_id"}, Unique: true},
			{Model: (*findingRevisionModel)(nil), Name: "webapi_finding_revisions_revision", Columns: []string{"finding_id", "revision"}, Unique: true},
			{Model: (*verificationModel)(nil), Name: "webapi_verifications_session", Columns: []string{"session_id"}, Unique: true},
			{Model: (*verificationModel)(nil), Name: "webapi_verifications_finding_created", Expr: "finding_id, created_at DESC, id DESC"},
			{Model: (*reportModel)(nil), Name: "webapi_reports_session", Columns: []string{"session_id"}, Unique: true},
			{Model: (*reportModel)(nil), Name: "webapi_reports_task_created", Expr: "task_id, created_at DESC, id DESC"},
			{Model: (*retestModel)(nil), Name: "webapi_retests_session", Columns: []string{"session_id"}, Unique: true},
			{Model: (*retestModel)(nil), Name: "webapi_retests_finding_created", Expr: "finding_id, created_at DESC, id DESC"},
			{Model: (*taskPolicyEventModel)(nil), Name: "webapi_task_policy_events_task_created", Expr: "task_id, created_at DESC, id DESC"},
			{Model: (*assetCredentialModel)(nil), Name: "webapi_asset_credentials_asset", Columns: []string{"asset_id", "created_at"}},
			{Model: (*assetTaskModel)(nil), Name: "webapi_asset_tasks_asset_created", Expr: "asset_id, created_at DESC"},
			{Model: (*scheduleRunModel)(nil), Name: "webapi_schedule_runs_asset_scheduled", Columns: []string{"asset_id", "scheduled_for"}, Unique: true},
			{Model: (*scheduleFollowupModel)(nil), Name: "webapi_schedule_followups_item", Columns: []string{"run_id", "kind", "finding_id"}, Unique: true},
			{Model: (*assetVulnModel)(nil), Name: "webapi_asset_vulns_asset", Columns: []string{"asset_id"}},
			{Model: (*assetVulnMemberModel)(nil), Name: "webapi_asset_vuln_members_vuln", Columns: []string{"vuln_id"}},
			{Model: (*assetVulnMemberModel)(nil), Name: "webapi_asset_vuln_members_asset", Columns: []string{"asset_id"}},
			{Model: (*egressFlowModel)(nil), Name: "webapi_egress_flows_task_created", Columns: []string{"task_id", "created_at"}},
			{Model: (*egressFlowModel)(nil), Name: "webapi_egress_flows_session", Columns: []string{"session_id"}},
			{Model: (*notificationModel)(nil), Name: "webapi_notifications_dedupe", Columns: []string{"dedupe_key", "kind", "verification_id"}, Unique: true},
			{Model: (*notificationModel)(nil), Name: "webapi_notifications_due", Columns: []string{"status", "next_attempt_at"}},
			{Model: (*notificationModel)(nil), Name: "webapi_notifications_created", Expr: "created_at DESC, id DESC"},
			{Model: (*auditEventModel)(nil), Name: "webapi_audit_events_at", Columns: []string{"at", "id"}},
			{Model: (*auditEventModel)(nil), Name: "webapi_audit_events_action", Columns: []string{"action"}},
			{Model: (*auditEventModel)(nil), Name: "webapi_audit_events_task", Columns: []string{"task_id"}},
			{Model: (*auditEventModel)(nil), Name: "webapi_audit_events_asset", Columns: []string{"asset_id"}},
			{Model: (*boardEndpointModel)(nil), Name: "webapi_board_endpoints_task_key", Columns: []string{"task_id", "endpoint_key"}, Unique: true},
			{Model: (*boardFactModel)(nil), Name: "webapi_board_facts_task_created", Expr: "task_id, created_at DESC, id DESC"},
			{Model: (*boardOracleModel)(nil), Name: "webapi_board_oracles_number", Columns: []string{"task_id", "number"}, Unique: true},
			{Model: (*boardOracleModel)(nil), Name: "webapi_board_oracles_dedupe", Columns: []string{"task_id", "dedupe_key"}, Unique: true},
			{Model: (*boardOracleEventModel)(nil), Name: "webapi_board_oracle_events_oracle", Columns: []string{"oracle_id", "at"}},
			{Model: (*replayModel)(nil), Name: "webapi_replays_vuln_created", Expr: "vuln_key, created_at DESC, id DESC"},
			{Model: (*taskSubsessionModel)(nil), Name: "webapi_task_subsessions_task_created", Expr: "task_id, created_at DESC"},
			{Model: (*subsessionUsageModel)(nil), Name: "webapi_subsession_usage_task_created", Columns: []string{"task_id", "created_at"}},
			{Model: (*authzCompareModel)(nil), Name: "webapi_authz_compares_task_created", Expr: "task_id, created_at DESC"},
			{Model: (*authzCompareModel)(nil), Name: "webapi_authz_compares_original", Columns: []string{"original_evidence_id"}},
		},
	}
}

// taskStore 持有自有可关闭 *sql.DB，仅操作本能力任务、HTTP证据和候选业务表。
type taskStore struct {
	db  *sql.DB
	orm *bun.DB
}

// openTaskStore 打开同一 hub.sqlite，仅建/用本表；数据库其余部分由原生
// SchemaModule 机制校验，这里不做迁移或重建。
func openTaskStore(database string) (*taskStore, error) {
	if strings.TrimSpace(database) == "" {
		return nil, fmt.Errorf("webapi 数据库路径不能为空")
	}
	// Begin 即取写锁并按 busy_timeout 等待，避免 deferred 先读后写升级立即 BUSY。
	db, err := sql.Open("sqlite", database+"?_journal_mode=WAL&_busy_timeout=5000&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("打开 sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	orm := bun.NewDB(db, sqlitedialect.New())
	return &taskStore{db: db, orm: orm}, nil
}

func (s *taskStore) Close() error {
	if s == nil || s.orm == nil {
		return nil
	}
	return s.orm.Close()
}

// SaveOutcome 描述保存结果：Conflict=同 ID 不同内容（不覆盖原授权），
// Idempotent=同 ID 同规范内容返回已保存任务，否则为新建。
type SaveOutcome struct {
	Task       *Task
	Created    bool
	Conflict   bool
	Idempotent bool
}

// canonicalKey 是规范比较视图（两输入 + 规范化授权）的相等 JSON，
// 不含服务端生成字段，避免切片不可比。
func canonicalKey(knownInfo, objective string, origins []string) string {
	copied := append([]string(nil), origins...)
	raw, _ := json.Marshal(struct {
		KnownInfo string   `json:"known_info"`
		Objective string   `json:"objective"`
		Origins   []string `json:"origins"`
	}{KnownInfo: knownInfo, Objective: objective, Origins: copied})
	return string(raw)
}

func decodeOrigins(raw string) ([]string, error) {
	var origins []string
	if err := json.Unmarshal([]byte(raw), &origins); err != nil {
		return nil, err
	}
	return origins, nil
}

func (m *taskModel) toTask() (*Task, error) {
	origins, err := decodeOrigins(m.OriginsJSON)
	if err != nil {
		return nil, err
	}
	return &Task{
		ID:               m.ID,
		KnownInfo:        m.KnownInfo,
		Objective:        m.Objective,
		Origins:          origins,
		InitialSessionID: m.InitialSessionID,
		CreatedAt:        m.CreatedAt,
	}, nil
}

func marshalOrigins(origins []string) (string, error) {
	raw, err := json.Marshal(origins)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Get 按 id 回读真实保存字段；不存在返回 (nil, nil)。
func (s *taskStore) Get(ctx context.Context, id string) (*Task, error) {
	var model taskModel
	err := s.orm.NewSelect().Model(&model).Where("id = ?", id).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return model.toTask()
}

// Save 在同一事务内先 INSERT ... ON CONFLICT(id) DO NOTHING，再一次 SELECT
// 做规范比较：事务首条 SQL 即写，避免与原生 Hub 其它连接并发写时的
// SQLite 读事务升级 BUSY/SNAPSHOT 窗口；唯一约束保证并发同 ID 只有一条。
func (s *taskStore) Save(ctx context.Context, id, knownInfo, objective string, origins []string) (*SaveOutcome, error) {
	return s.SaveWithScenario(ctx, id, knownInfo, objective, origins, nil)
}

// SaveWithScenario 同 Save；scenario 非空且本次新建时在同一事务内写任务场景（M1）。
// 幂等重放与冲突都不改已保存场景（改场景走 PUT /tasks/{id}/scenario）。
func (s *taskStore) SaveWithScenario(ctx context.Context, id, knownInfo, objective string, origins []string, scenario *taskScenarioModel) (*SaveOutcome, error) {
	want := canonicalKey(knownInfo, objective, origins)
	originsJSON, err := marshalOrigins(origins)
	if err != nil {
		return nil, err
	}
	model := taskModel{
		ID:               id,
		KnownInfo:        knownInfo,
		Objective:        objective,
		OriginsJSON:      originsJSON,
		InitialSessionID: newSessionID(),
		CreatedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	}
	outcome := &SaveOutcome{}
	err = s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewInsert().Model(&model).On("CONFLICT (id) DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		var existing taskModel
		if err := tx.NewSelect().Model(&existing).Where("id = ?", id).Limit(1).Scan(ctx); err != nil {
			return err
		}
		existingOrigins, err := decodeOrigins(existing.OriginsJSON)
		if err != nil {
			return err
		}
		task, err := existing.toTask()
		if err != nil {
			return err
		}
		outcome.Task = task
		if affected > 0 {
			outcome.Created = true
			if scenario != nil {
				return putTaskScenarioTx(ctx, tx, id, scenario.Scenario, scenario.DoneCriteria, scenario.MaxRounds)
			}
			return nil
		}
		if canonicalKey(existing.KnownInfo, existing.Objective, existingOrigins) != want {
			outcome.Conflict = true
			return nil
		}
		outcome.Idempotent = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	return outcome, nil
}
