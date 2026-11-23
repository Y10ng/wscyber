package webapi

import (
	"context"
	"net/http"
)

// FollowupCounts 是某类后续项的状态计数。
type FollowupCounts struct {
	Pending  int `json:"pending"`
	Launched int `json:"launched"`
	Failed   int `json:"failed"`
	Skipped  int `json:"skipped"`
}

// ScheduleFollowupSummary 是计划运行的后续摘要。state：
//   - waiting：研究会话尚未结束（或尚未核对到结束），后续未规划；
//   - not_planned：研究结束前已有更新的计划运行，本次不再规划后续；
//   - in_progress：已规划，仍有 pending 项或已发起的后续会话仍在运行；
//   - completed：全部项已发起/失败/跳过，且已发起的会话均不在运行。
//
// auto_verify/auto_retest 为规划时的开关快照（未规划为 null）。
type ScheduleFollowupSummary struct {
	State       string         `json:"state"`
	Completed   bool           `json:"completed"`
	AutoVerify  *bool          `json:"auto_verify"`
	AutoRetest  *bool          `json:"auto_retest"`
	PlannedAt   *string        `json:"planned_at"`
	CompletedAt *string        `json:"completed_at"`
	Verify      FollowupCounts `json:"verify"`
	Retest      FollowupCounts `json:"retest"`
}

func countsOf(values map[string]int) FollowupCounts {
	return FollowupCounts{Pending: values[FollowupPending], Launched: values[FollowupLaunched], Failed: values[FollowupFailed], Skipped: values[FollowupSkipped]}
}

func (c *Capability) followupSummary(ctx context.Context, run scheduleRunModel) (*ScheduleFollowupSummary, error) {
	if run.Status != ScheduleRunLaunched || run.TaskID == "" {
		return nil, nil
	}
	marker, err := readOptional[scheduleRunFollowupModel](ctx, c.store, "run_id = ?", run.ID)
	if err != nil {
		return nil, err
	}
	summary := &ScheduleFollowupSummary{}
	if marker == nil {
		latest, err := c.store.latestLaunchedRun(ctx, run.AssetID, "")
		if err != nil {
			return nil, err
		}
		summary.State = "not_planned"
		if latest != nil && latest.ID == run.ID {
			summary.State = "waiting"
		}
		return summary, nil
	}
	counts, err := c.store.followupCounts(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	autoVerify, autoRetest, plannedAt := marker.AutoVerify, marker.AutoRetest, marker.PlannedAt
	summary.AutoVerify, summary.AutoRetest, summary.PlannedAt = &autoVerify, &autoRetest, &plannedAt
	summary.Verify, summary.Retest = countsOf(counts[FollowupVerify]), countsOf(counts[FollowupRetest])
	summary.State = "in_progress"
	if marker.CompletedAt != "" {
		completedAt := marker.CompletedAt
		summary.State, summary.Completed, summary.CompletedAt = "completed", true, &completedAt
	}
	return summary, nil
}

func (c *Capability) runViewWithFollowups(ctx context.Context, model scheduleRunModel) (ScheduleRun, error) {
	view := runView(model)
	summary, err := c.followupSummary(ctx, model)
	view.Followups = summary
	return view, err
}

// ScheduleFollowupItem 是后续项明细；verdict/conclusion 为对应验证/复测已保存的判读，没有为 null。
type ScheduleFollowupItem struct {
	ID            string  `json:"id"`
	Kind          string  `json:"kind"`
	TaskID        string  `json:"task_id"`
	FindingID     string  `json:"finding_id"`
	FindingNumber string  `json:"finding_number"`
	FindingTitle  string  `json:"finding_title"`
	RevisionID    string  `json:"revision_id"`
	TargetID      string  `json:"target_id"`
	SessionID     *string `json:"session_id"`
	Status        string  `json:"status"`
	Error         string  `json:"error"`
	Verdict       *string `json:"verdict"`
	Conclusion    *string `json:"conclusion"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

func (c *Capability) followupItemView(ctx context.Context, item scheduleFollowupModel) (ScheduleFollowupItem, error) {
	view := ScheduleFollowupItem{ID: item.ID, Kind: item.Kind, TaskID: item.TaskID, FindingID: item.FindingID, RevisionID: item.RevisionID, TargetID: item.TargetID, Status: item.Status, Error: item.Error, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
	source, err := c.store.findingRevision(ctx, item.TaskID, item.FindingID, item.RevisionID)
	if err != nil {
		return view, err
	}
	if source != nil {
		view.FindingNumber, view.FindingTitle = source.Number, source.Content.Title
	}
	sessionID, err := c.store.followupTargetSession(ctx, item)
	if err != nil || sessionID == "" {
		return view, err
	}
	view.SessionID = &sessionID
	if item.Kind == FollowupVerify {
		result, err := c.store.verificationResult(ctx, item.TargetID)
		if result != nil {
			verdict := result.Payload.Verdict
			view.Verdict = &verdict
		}
		return view, err
	}
	result, err := c.store.retestResult(ctx, item.TargetID)
	if result != nil {
		conclusion := result.Payload.Conclusion
		view.Conclusion = &conclusion
	}
	return view, err
}

// handleScheduleRunFollowups：GET /api/webapi/schedule-runs/{run_id}/followups。
func (c *Capability) handleScheduleRunFollowups(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "run_id", "计划运行")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	run, err := c.store.scheduleRun(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取计划运行记录失败")
		return
	}
	if run == nil {
		writeError(w, http.StatusNotFound, "计划运行不存在")
		return
	}
	view, err := c.runViewWithFollowups(r.Context(), *run)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取计划后续失败")
		return
	}
	models, err := c.store.runFollowups(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取计划后续失败")
		return
	}
	items := make([]ScheduleFollowupItem, 0, len(models))
	for _, model := range models {
		item, err := c.followupItemView(r.Context(), model)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取计划后续失败")
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": view, "items": items})
}
