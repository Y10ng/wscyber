package webapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// 任务删除（契约 native/TASK_DELETE.md）：不可恢复地删除任务本体及其流量、候选、验证、报告、复测、黑板、
// 子 Agent、计划后续、无资产候选的处置与全部任务会话；审计、推送记录、计划运行、审批记录保留但解除 task_id 关联；资产与漏洞条目保留，
// 只删本任务的漏洞成员，成员被移空的人工条目按 deleteEmptyManualVuln 删除。

// taskDeleteCountKeys 是响应 deleted 的固定键（缺省为 0）。
var taskDeleteCountKeys = []string{
	"requests", "findings", "verifications", "reports", "retests", "egress_flows",
	"board_endpoints", "board_facts", "board_oracles", "authz_compares", "subsessions", "sessions",
}

// taskDeleteTables 按序删除的表；key 非空的行数计入 deleted[key]；where 为空即 task_id = ?。
// 无 task_id 列的结果/草稿表经父表子查询定位，须排在父表之前。
var taskDeleteTables = []struct{ table, key, where string }{
	{"webapi_verification_results", "", "verification_id IN (SELECT id FROM webapi_verifications WHERE task_id = ?)"},
	{"webapi_report_drafts", "", "report_id IN (SELECT id FROM webapi_reports WHERE task_id = ?)"},
	{"webapi_retest_results", "", "retest_id IN (SELECT id FROM webapi_retests WHERE task_id = ?)"},
	{"webapi_requests", "requests", ""},
	{"webapi_findings", "findings", ""},
	{"webapi_finding_revisions", "", ""},
	{"webapi_verifications", "verifications", ""},
	{"webapi_reports", "reports", ""},
	{"webapi_retests", "retests", ""},
	{"webapi_egress_flows", "egress_flows", ""},
	{"webapi_board_endpoints", "board_endpoints", ""},
	{"webapi_board_facts", "board_facts", ""},
	{"webapi_board_oracles", "board_oracles", ""},
	{"webapi_board_oracle_events", "", ""},
	{"webapi_board_ingest", "", ""},
	{"webapi_authz_compares", "authz_compares", ""},
	{"webapi_replays", "", ""},
	{"webapi_task_policies", "", ""},
	{"webapi_task_policy_events", "", ""},
	{"webapi_task_scenarios", "", ""},
	{"webapi_task_explore", "", ""},
	{"webapi_task_subsessions", "subsessions", ""},
	{"webapi_subagent_reservations", "", ""},
	{"webapi_subsession_usage", "", ""},
	{"webapi_asset_tasks", "", ""},
	{"webapi_schedule_followups", "", ""},
	{"webapi_schedule_run_followups", "", ""},
	{"webapi_tasks", "", "id = ?"},
}

// taskDeleteDetach 保留行、解除任务关联（置空字符串）。须在删除候选之前执行（推送按候选子查询定位）。
var taskDeleteDetach = []struct{ table, set, where string }{
	{"webapi_notifications", "task_id = '', finding_id = ''", "task_id = ? OR finding_id IN (SELECT id FROM webapi_findings WHERE task_id = ?)"},
	{"webapi_audit_events", "task_id = ''", "task_id = ?"},
	{"webapi_schedule_runs", "task_id = ''", "task_id = ?"},
	{"webapi_approvals", "task_id = ''", "task_id = ?"},
}

// TaskDeleteResult 是 DELETE /api/webapi/tasks/{id} 的 200 响应。
type TaskDeleteResult struct {
	TaskID             string           `json:"task_id"`
	Deleted            map[string]int64 `json:"deleted"`
	OrphanVulnsRemoved int64            `json:"orphan_vulns_removed"`
}

var (
	errTaskDeleteGone = errors.New("任务不存在")
	// errTaskDeleteSessionsChanged：删会话之后、写事务内复核发现任务出现了未删除的新会话（并发发起）。
	errTaskDeleteSessionsChanged = errors.New("删除期间任务出现了新会话")
)

// handleDeleteTask：DELETE /api/webapi/tasks/{id}（空 body 或 {}）。顺序：判运行中（409）→ 逐个删原生会话 →
// 单写事务删/置空业务数据并同事务登记 task.delete。
func (c *Capability) handleDeleteTask(w http.ResponseWriter, r *http.Request) {
	id, err := taskIDFromPath(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := decodeJSONBody(r, &struct{}{}, true, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 一旦开始删会话就不随页面断开中止，避免停在半途。
	ctx := context.WithoutCancel(r.Context())
	// 与从资产/计划发起同一任务串行，删除期间不会对该任务新开研究会话。
	unlock := c.taskLaunchLock(id)
	defer unlock()

	task, err := c.store.Get(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务失败")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	sessions, err := c.store.taskSessionIDs(ctx, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务会话失败")
		return
	}
	running, err := c.taskInFlight(ctx, id, sessions)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "暂不能核对任务是否仍在运行，未删除任何数据；可重试")
		return
	}
	if running {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "任务仍在运行，请先停止后再删除", "code": "in_flight"})
		return
	}

	deleted := 0
	if c.launcher == nil {
		log.Printf("webapi 删除任务 %s：会话服务未注入，跳过 %d 个原生会话的删除", id, len(sessions))
	} else {
		for _, sessionID := range sessions {
			ok, err := c.launcher.DeleteSession(ctx, sessionID)
			if err != nil {
				log.Printf("webapi 删除任务 %s：删除会话 %s 失败：%v", id, sessionID, err)
				writeJSON(w, http.StatusInternalServerError, map[string]any{
					"error":            fmt.Sprintf("删除任务会话失败：已删除 %d 个会话，共 %d 个；任务数据未删除，可重试", deleted, len(sessions)),
					"code":             "session_delete_failed",
					"sessions_deleted": deleted,
				})
				return
			}
			if ok {
				deleted++
			}
		}
	}

	unlockVulns, err := c.lockTaskVulnAssets(ctx, id)
	if err != nil {
		writeTaskDeleteTxError(w, err, deleted)
		return
	}
	result, err := c.store.deleteTask(ctx, id, sessions, int64(deleted), c.scheduleClock())
	unlockVulns()
	if err != nil {
		log.Printf("webapi 删除任务 %s：删除业务数据失败：%v", id, err)
		writeTaskDeleteTxError(w, err, deleted)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeTaskDeleteTxError(w http.ResponseWriter, err error, sessionsDeleted int) {
	switch {
	case errors.Is(err, errTaskDeleteGone):
		writeError(w, http.StatusNotFound, "任务不存在")
	case errors.Is(err, errTaskDeleteSessionsChanged):
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":            fmt.Sprintf("删除期间任务出现了新会话：已删除 %d 个会话，任务数据未删除；请确认任务已停止后重试", sessionsDeleted),
			"code":             "sessions_changed",
			"sessions_deleted": sessionsDeleted,
		})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error":            fmt.Sprintf("删除任务数据失败：已删除 %d 个会话，任务数据未删除；可重试", sessionsDeleted),
			"code":             "delete_failed",
			"sessions_deleted": sessionsDeleted,
		})
	}
}

// taskInFlight 按 taskRunStatus 口径判定运行中：有运行中子 Agent 会话，或任一任务会话（研究/验证/报告/复测/子 Agent）
// SessionRunning。launcher 未注入时只看子会话记录（无法核对原生会话），记日志。
func (c *Capability) taskInFlight(ctx context.Context, taskID string, sessions []string) (bool, error) {
	if running, err := c.store.taskHasRunningSubsession(ctx, taskID); err != nil || running {
		return running, err
	}
	if c.launcher == nil {
		log.Printf("webapi 删除任务 %s：会话服务未注入，按无运行中会话处理", taskID)
		return false, nil
	}
	for _, sessionID := range sessions {
		running, err := c.launcher.SessionRunning(ctx, sessionID)
		if err != nil || running {
			return running, err
		}
	}
	return false, nil
}

// taskSessionIDs 是任务全部会话（研究/验证/报告/复测/子 Agent，与用量统计同一枚举口径），去重、去空、按字典序。
func (s *taskStore) taskSessionIDs(ctx context.Context, taskID string) ([]string, error) {
	sessions, _, err := s.usageSessionsForTasks(ctx, []string{taskID})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(sessions))
	for id := range sessions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, nil
}

// lockTaskVulnAssets 取任务关联资产（资产关联与漏洞成员）的漏洞库锁，避免同步在删除提交后补回已删候选的成员。
// 按资产 id 字典序加锁；其它路径一次只持一个资产锁，不会成环。
func (c *Capability) lockTaskVulnAssets(ctx context.Context, taskID string) (func(), error) {
	assets := []string{}
	err := c.store.queryRows(ctx, func(rows *sql.Rows) error {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		assets = append(assets, id)
		return nil
	}, `SELECT asset_id FROM webapi_asset_tasks WHERE task_id = ? UNION SELECT asset_id FROM webapi_asset_vuln_members WHERE task_id = ?`, taskID, taskID)
	if err != nil {
		return nil, err
	}
	slices.Sort(assets)
	unlocks := make([]func(), 0, len(assets))
	for _, id := range slices.Compact(assets) {
		unlocks = append(unlocks, c.lockAssetVulns(id))
	}
	return func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}, nil
}

// deleteTask 在一个写事务（全局写锁）里删除任务业务数据并登记 task.delete。deletedSessions 是事务前已
// 处理（删除或已不存在）的会话；事务内复核任务会话集合，出现其外的新会话即回滚。
func (s *taskStore) deleteTask(ctx context.Context, taskID string, deletedSessions []string, sessionsDeleted int64, at time.Time) (*TaskDeleteResult, error) {
	result := &TaskDeleteResult{TaskID: taskID, Deleted: map[string]int64{}}
	for _, key := range taskDeleteCountKeys {
		result.Deleted[key] = 0
	}
	result.Deleted["sessions"] = sessionsDeleted
	err := s.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		var objective string
		err := tx.QueryRowContext(ctx, `SELECT objective FROM webapi_tasks WHERE id = ?`, taskID).Scan(&objective)
		if errors.Is(err, sql.ErrNoRows) {
			return errTaskDeleteGone
		}
		if err != nil {
			return err
		}
		current, _, err := usageSessionsTx(ctx, tx, []string{taskID})
		if err != nil {
			return err
		}
		for sessionID := range current {
			if !slices.Contains(deletedSessions, sessionID) {
				return errTaskDeleteSessionsChanged
			}
		}

		// 漏洞成员：只删本任务成员；被移空的人工条目删除，其余受影响条目刷新 updated_at。
		vulnIDs := []string{}
		if err := scanRows(tx.QueryContext(ctx, `SELECT DISTINCT vuln_id FROM webapi_asset_vuln_members WHERE task_id = ? ORDER BY vuln_id`, taskID))(func(rows *sql.Rows) error {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			vulnIDs = append(vulnIDs, id)
			return nil
		}); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM webapi_asset_vuln_members WHERE task_id = ?`, taskID); err != nil {
			return err
		}
		now := nowRFC3339()
		for _, vulnID := range vulnIDs {
			removed, err := deleteEmptyManualVuln(ctx, tx, vulnID)
			if err != nil {
				return err
			}
			if removed {
				result.OrphanVulnsRemoved++
				continue
			}
			if _, err := tx.NewUpdate().Model((*assetVulnModel)(nil)).Set("updated_at = ?", now).Where("id = ?", vulnID).Exec(ctx); err != nil {
				return err
			}
		}

		// 无资产任务候选在全局漏洞列表里自成一条（键 finding:<finding_id>），其处置随候选删除；
		// 只删本任务候选的该前缀键，不计入 orphan_vulns_removed（该计数只计被删除的资产漏洞条目行）。
		if _, err := tx.ExecContext(ctx, `DELETE FROM webapi_vuln_triage WHERE vuln_key IN (SELECT 'finding:' || id FROM webapi_findings WHERE task_id = ?)`, taskID); err != nil {
			return fmt.Errorf("删除候选处置: %w", err)
		}
		for _, detach := range taskDeleteDetach {
			args := slices.Repeat([]any{taskID}, strings.Count(detach.where, "?"))
			if _, err := tx.ExecContext(ctx, `UPDATE `+detach.table+` SET `+detach.set+` WHERE `+detach.where, args...); err != nil {
				return fmt.Errorf("解除 %s 关联: %w", detach.table, err)
			}
		}
		for _, item := range taskDeleteTables {
			where := item.where
			if where == "" {
				where = "task_id = ?"
			}
			res, err := tx.ExecContext(ctx, `DELETE FROM `+item.table+` WHERE `+where, taskID)
			if err != nil {
				return fmt.Errorf("删除 %s: %w", item.table, err)
			}
			if item.key != "" {
				affected, err := res.RowsAffected()
				if err != nil {
					return err
				}
				result.Deleted[item.key] = affected
			}
		}

		title := usageTitle(objective)
		detail := map[string]any{"task_id": taskID, "title": title, "orphan_vulns_removed": result.OrphanVulnsRemoved}
		for key, value := range result.Deleted {
			detail[key] = value
		}
		model, err := newAuditModel(auditEntry{Actor: auditActorUser, Action: "task.delete", TargetKind: "task", TargetID: taskID,
			Summary: "删除任务：" + title, Detail: detail}, at)
		if err != nil {
			return err
		}
		return insertAuditTx(ctx, tx, model)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
