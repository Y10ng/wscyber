package webapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// 测试策略：只由人经鉴权 API 设置；模型、目标响应、资料都没有修改途径。
const (
	ScopeOrigins      = "origins"      // 默认：webapi_http 只发任务授权 origins
	ScopeUnrestricted = "unrestricted" // 任意 http/https，仍完整记录证据

	ApprovalOff      = "off"
	ApprovalHighRisk = "high_risk" // 默认
	ApprovalAll      = "all"
)

// TaskPolicy 是任务当前测试策略；无记录的任务（旧任务）为默认值且 IsDefault=true。
type TaskPolicy struct {
	Scope     string  `json:"scope"`
	Approval  string  `json:"approval"`
	IsDefault bool    `json:"is_default"`
	UpdatedAt *string `json:"updated_at"`
}

func defaultTaskPolicy() TaskPolicy {
	return TaskPolicy{Scope: ScopeOrigins, Approval: ApprovalHighRisk, IsDefault: true}
}

func validPolicy(scope, approval string) error {
	if scope != ScopeOrigins && scope != ScopeUnrestricted {
		return fmt.Errorf("scope 只能是 origins 或 unrestricted")
	}
	if approval != ApprovalOff && approval != ApprovalHighRisk && approval != ApprovalAll {
		return fmt.Errorf("approval 只能是 off、high_risk 或 all")
	}
	return nil
}

type taskPolicyModel struct {
	bun.BaseModel `bun:"table:webapi_task_policies,alias:webapi_task_policy"`

	TaskID    string `bun:"task_id,pk"`
	Scope     string `bun:"scope,notnull"`
	Approval  string `bun:"approval,notnull"`
	UpdatedAt string `bun:"updated_at,notnull"`
}

// taskPolicyEventModel 是策略变更记录（每次保存一行，含保存后的值）。
type taskPolicyEventModel struct {
	bun.BaseModel `bun:"table:webapi_task_policy_events,alias:webapi_task_policy_event"`

	ID        string `bun:"id,pk"`
	TaskID    string `bun:"task_id,notnull"`
	Scope     string `bun:"scope,notnull"`
	Approval  string `bun:"approval,notnull"`
	CreatedAt string `bun:"created_at,notnull"`
}

func (m *taskPolicyModel) toPolicy() TaskPolicy {
	updated := m.UpdatedAt
	return TaskPolicy{Scope: m.Scope, Approval: m.Approval, UpdatedAt: &updated}
}

// policyForTask 每次调用都读库：保存即生效，影响之后的调用。
func (s *taskStore) policyForTask(ctx context.Context, taskID string) (TaskPolicy, error) {
	var model taskPolicyModel
	err := s.orm.NewSelect().Model(&model).Where("task_id = ?", taskID).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return defaultTaskPolicy(), nil
	}
	if err != nil {
		return TaskPolicy{}, err
	}
	policy := model.toPolicy()
	if validPolicy(policy.Scope, policy.Approval) != nil {
		return TaskPolicy{}, fmt.Errorf("已保存测试策略非法")
	}
	return policy, nil
}

// policyForSession 用现有会话→任务映射；映射不到任务的会话按默认值（task=nil）。
func (s *taskStore) policyForSession(ctx context.Context, sessionID string) (TaskPolicy, *Task, error) {
	if sessionID == "" {
		return defaultTaskPolicy(), nil, nil
	}
	task, err := s.taskForSession(ctx, sessionID)
	if err != nil {
		return TaskPolicy{}, nil, err
	}
	if task == nil {
		return defaultTaskPolicy(), nil, nil
	}
	policy, err := s.policyForTask(ctx, task.ID)
	return policy, task, err
}

// setPolicy 在一个事务里写当前值并追加变更记录；首条 SQL 即写。
func (s *taskStore) setPolicy(ctx context.Context, taskID, scope, approval string) (TaskPolicy, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	current := &taskPolicyModel{TaskID: taskID, Scope: scope, Approval: approval, UpdatedAt: now}
	event := &taskPolicyEventModel{ID: "wp_" + newSessionID() + newSessionID(), TaskID: taskID, Scope: scope, Approval: approval, CreatedAt: now}
	err := s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(current).
			On("CONFLICT (task_id) DO UPDATE").Set("scope = EXCLUDED.scope").Set("approval = EXCLUDED.approval").Set("updated_at = EXCLUDED.updated_at").
			Exec(ctx); err != nil {
			return err
		}
		_, err := tx.NewInsert().Model(event).Exec(ctx)
		return err
	})
	if err != nil {
		return TaskPolicy{}, err
	}
	return current.toPolicy(), nil
}

// withPolicy 给任务响应附上当前策略与来源资产；读失败返回错误而不是伪造默认值。
func (s *taskStore) withPolicy(ctx context.Context, task *Task) (*Task, error) {
	if task == nil {
		return nil, nil
	}
	policy, err := s.policyForTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	asset, err := s.assetForTask(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	copied := *task
	copied.Policy = &policy
	copied.Asset = asset
	return &copied, nil
}

type savePolicy struct {
	Scope    *string `json:"scope"`
	Approval *string `json:"approval"`
}

// handlePutPolicy 保存任务测试策略：校验枚举，写当前值+变更记录，保存即生效。
func (c *Capability) handlePutPolicy(w http.ResponseWriter, r *http.Request) {
	id, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	defer r.Body.Close()
	var body savePolicy
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxRequestBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体须为只含 scope、approval 的 JSON 对象"})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体只能包含一个 JSON 对象"})
		return
	}
	if body.Scope == nil || body.Approval == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "scope 与 approval 都必须提供"})
		return
	}
	scope, approval := strings.TrimSpace(*body.Scope), strings.TrimSpace(*body.Approval)
	if err := validPolicy(scope, approval); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	task, err := c.store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取任务失败"})
		return
	}
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "任务不存在"})
		return
	}
	before, err := c.store.policyForTask(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取测试策略失败"})
		return
	}
	policy, err := c.store.setPolicy(r.Context(), id, scope, approval)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存测试策略失败"})
		return
	}
	c.auditTaskPolicy(r.Context(), id, before, policy)
	writeJSON(w, http.StatusOK, map[string]any{"task_id": id, "policy": policy})
}
