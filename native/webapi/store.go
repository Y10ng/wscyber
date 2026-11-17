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
		Models: []any{(*taskModel)(nil), (*requestModel)(nil), (*findingModel)(nil), (*findingRevisionModel)(nil), (*verificationModel)(nil), (*verificationResultModel)(nil), (*reportModel)(nil), (*reportDraftModel)(nil), (*retestModel)(nil), (*retestResultModel)(nil), (*taskPolicyModel)(nil), (*taskPolicyEventModel)(nil)},
		Tables: map[string][]string{
			"webapi_tasks":                {"id", "known_info", "objective", "origins_json", "initial_session_id", "created_at"},
			"webapi_requests":             {"id", "task_id", "session_id", "turn_id", "call_id", "fingerprint", "method", "url", "status", "reason", "sent", "status_code", "preview", "preview_truncated", "capture_truncated", "flow_pb", "created_at", "completed_at"},
			"webapi_findings":             {"id", "task_id", "number", "status", "current_revision", "current_revision_id", "created_at", "updated_at"},
			"webapi_finding_revisions":    {"id", "finding_id", "task_id", "number", "revision", "session_id", "turn_id", "call_id", "payload_json", "title", "created_at"},
			"webapi_verifications":        {"id", "task_id", "finding_id", "source_revision_id", "session_id", "created_at"},
			"webapi_verification_results": {"verification_id", "session_id", "turn_id", "call_id", "payload_json", "created_at"},
			"webapi_reports":              {"id", "task_id", "session_id", "items_json", "created_at"},
			"webapi_report_drafts":        {"report_id", "session_id", "turn_id", "call_id", "payload_json", "created_at"},
			"webapi_retests":              {"id", "task_id", "finding_id", "source_revision_id", "session_id", "created_at"},
			"webapi_retest_results":       {"retest_id", "session_id", "turn_id", "call_id", "payload_json", "created_at"},
			"webapi_task_policies":        {"task_id", "scope", "approval", "updated_at"},
			"webapi_task_policy_events":   {"id", "task_id", "scope", "approval", "created_at"},
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
	db, err := sql.Open("sqlite", database+"?_journal_mode=WAL&_busy_timeout=5000")
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
