package webapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/uptrace/bun"
)

// 漏洞卡“申请复测”（V3 后端）：人在漏洞卡上点“申请复测”，平台在代表候选的原任务下由后端直接新建复测
// （saveRetest，平台生成 UUID）并 Open+Run（与计划后续共用 startPinnedSession）。复测结论仍由
// webapi_retest_result 写入，按现有规则派生漏洞状态。不新增表：申请记录即审计 vuln.retest_request。
// 契约见 native/VULN_CARD.md「申请复测」。

const (
	retestRequestArchived      = "archived"
	retestRequestFalsePositive = "false_positive"
	retestRequestNoMember      = "no_member"
	retestRequestInFlight      = "in_flight"
	retestRequestNoModel       = "no_model"

	retestRequestRunning = "running"
	retestRequestDone    = "done"
)

// VulnCardRetestRequest 是卡片 retest_request：该键最近一次成功的“申请复测”及其会话状态。
type VulnCardRetestRequest struct {
	RetestID  string `json:"retest_id"`
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	FindingID string `json:"finding_id"`
	StartedAt string `json:"started_at"`
	Status    string `json:"status"` // running | done
}

// newUUIDv4 生成随机 UUID（复测记录 id）。
func newUUIDv4() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// retestRequestFinding 选用复测候选：与卡片 repro 同一来源（最新 supported 且带 key_steps 的验证所属候选）；
// 无 repro 时取成员中最近一次 supported 验证所属候选（同人工推送选用规则 manualNotifyVerification）；
// 都没有则取最近成员候选（成员按候选创建时间倒序）。选中的候选须仍是条目成员；返回 "" 表示无成员。
func retestRequestFinding(item *GlobalVuln, verifications []cardVerification) string {
	if len(item.members) == 0 {
		return ""
	}
	member := map[string]bool{}
	for _, m := range item.members {
		member[m.FindingID] = true
	}
	if chosen := manualNotifyVerification(verifications); chosen != nil && member[chosen.model.FindingID] {
		return chosen.model.FindingID
	}
	return item.members[0].FindingID
}

// sessionActive：会话仍在运行；无法核对按运行中计（同计划后续并发计数）。launcher 为空时视为不在运行。
func (c *Capability) sessionActive(ctx context.Context, sessionID string) bool {
	if c.launcher == nil || sessionID == "" {
		return false
	}
	running, err := c.launcher.SessionRunning(ctx, sessionID)
	return running || err != nil
}

// unresolvedRetests：成员候选下尚无结论的复测（任何来源：手动 PUT、计划后续、申请复测），新到旧。
func (s *taskStore) unresolvedRetests(ctx context.Context, findingIDs []string) ([]retestModel, error) {
	if len(findingIDs) == 0 {
		return nil, nil
	}
	var rows []retestModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&rows).
			Where("finding_id IN (?)", bun.In(findingIDs)).
			Where("NOT EXISTS (SELECT 1 FROM webapi_retest_results r WHERE r.retest_id = webapi_retest.id)").
			OrderExpr(`created_at COLLATE "C" DESC, id COLLATE "C" DESC`).Scan(ctx)
	})
	return rows, err
}

// inFlightRetest：成员中未出结论且会话正在运行的复测；没有返回 nil。
// 已保存但从未启动（手动 PUT 未 Run、发起失败）或会话已结束仍无结论的复测不算进行中。
func (c *Capability) inFlightRetest(ctx context.Context, findingIDs []string) (*retestModel, error) {
	rows, err := c.store.unresolvedRetests(ctx, findingIDs)
	if err != nil {
		return nil, err
	}
	for index := range rows {
		if c.sessionActive(ctx, rows[index].SessionID) {
			return &rows[index], nil
		}
	}
	return nil, nil
}

func writeRetestRequestConflict(w http.ResponseWriter, code, message string, extra map[string]any) {
	body := map[string]any{"error": message, "code": code}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, http.StatusConflict, body)
}

// handleVulnCardRetest: POST /api/webapi/vulns/{key}/card/retest（空 body 或 {}）→ 201
// {retest_id, session_id, task_id, finding_id, revision_id}。409 依次：archived、false_positive、no_member、in_flight、no_model。
func (c *Capability) handleVulnCardRetest(w http.ResponseWriter, r *http.Request) {
	key, ok := vulnCardKey(w, r)
	if !ok {
		return
	}
	var input struct{}
	if err := decodeJSONBody(r, &input, true, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 人工触发后建记录与启动会话不随页面断开取消；同键串行，避免连点建出两个复测。
	ctx := context.WithoutCancel(r.Context())
	unlock := c.taskLaunchLock("vuln-retest\x00" + key)
	defer unlock()

	item, _, err := c.cardTarget(ctx, key)
	if errors.Is(err, errTriageNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞条目失败")
		return
	}
	if item.assetID != "" {
		asset, err := c.store.assetModel(ctx, item.assetID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取资产失败")
			return
		}
		if asset != nil && asset.Archived {
			writeRetestRequestConflict(w, retestRequestArchived, "资产已归档，不复测", nil)
			return
		}
	}
	if item.Triage != nil && item.Triage.Disposition == DispositionFalsePositive {
		writeRetestRequestConflict(w, retestRequestFalsePositive, "该漏洞已处置为误报，不复测", nil)
		return
	}
	findingIDs := memberFindingIDs(item)
	verifications, err := c.store.cardVerifications(ctx, findingIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取验证结果失败")
		return
	}
	noMember := func() {
		writeRetestRequestConflict(w, retestRequestNoMember, "该漏洞没有可用于复测的候选", nil)
	}
	findingID := retestRequestFinding(item, verifications)
	if findingID == "" {
		noMember()
		return
	}
	finding, err := readOptional[findingModel](ctx, c.store, "id = ?", findingID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取候选失败")
		return
	}
	var task *Task
	if finding != nil {
		if task, err = c.store.Get(ctx, finding.TaskID); err != nil {
			writeError(w, http.StatusInternalServerError, "读取任务失败")
			return
		}
	}
	if finding == nil || task == nil || finding.CurrentRevisionID == "" {
		noMember()
		return
	}
	running, err := c.inFlightRetest(ctx, findingIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取复测记录失败")
		return
	}
	if running != nil {
		writeRetestRequestConflict(w, retestRequestInFlight, "该漏洞已有未出结论的复测正在运行", map[string]any{"retest_id": running.ID, "session_id": running.SessionID})
		return
	}

	summary, isNew, conflict, err := c.store.saveRetest(ctx, newUUIDv4(), task.ID, finding.ID, finding.CurrentRevisionID)
	switch {
	case errors.Is(err, errFindingNotFound):
		noMember()
		return
	case errors.Is(err, errRetestStale):
		writeError(w, http.StatusInternalServerError, "候选修订刚刚更新，未发起复测；请重试")
		return
	case err != nil || conflict || !isNew:
		writeError(w, http.StatusInternalServerError, "保存复测失败")
		return
	}
	c.auditSessionStart(ctx, "retest", task.ID, finding.ID, summary.RetestID, summary.SessionID)
	if err := c.startPinnedSession(ctx, task, finding.ID, finding.CurrentRevisionID, summary.SessionID, "复测", retestMessageVariant); err != nil {
		// 与计划后续一致：复测记录保留（不删除、不自动重试）；再次申请会新建复测记录。
		var launchErr *AssetLaunchError
		switch {
		case errors.As(err, &launchErr):
			writeRetestRequestConflict(w, retestRequestNoModel, launchErr.Stage+"："+launchErr.Msg,
				map[string]any{"stage": launchErr.Stage, "retest_id": summary.RetestID, "session_id": summary.SessionID})
		case errors.Is(err, errPinnedSourceMissing):
			noMember()
		default:
			writeError(w, http.StatusInternalServerError, "启动复测会话失败，复测已保存但未启动")
		}
		return
	}
	number := c.findingNumberOf(ctx, finding.ID)
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "vuln.retest_request", TargetKind: "vuln", TargetID: item.ID, TaskID: task.ID, AssetID: item.assetID,
		Summary: fmt.Sprintf("申请复测漏洞「%s」：候选 %s", item.Title, number),
		Detail:  map[string]any{"vuln_key": item.ID, "retest_id": summary.RetestID, "session_id": summary.SessionID, "task_id": task.ID, "finding_id": finding.ID}})
	writeJSON(w, http.StatusCreated, map[string]any{"retest_id": summary.RetestID, "session_id": summary.SessionID, "task_id": task.ID,
		"finding_id": finding.ID, "revision_id": finding.CurrentRevisionID})
}

// ---- 卡片联动 ----

type retestRequestDetail struct {
	RetestID  string `json:"retest_id"`
	SessionID string `json:"session_id"`
	TaskID    string `json:"task_id"`
	FindingID string `json:"finding_id"`
}

// cardRetestRequest 由该键全部审计（新到旧）得出卡片 retest_request（最近一次申请）与每次申请的生命周期事件。
// status：复测已有结论或会话不在运行 → done；无结论且会话运行中（无法核对按运行中）→ running。
func (c *Capability) cardRetestRequest(ctx context.Context, changes []AuditEvent) (*VulnCardRetestRequest, []VulnCardEvent, error) {
	var latest *VulnCardRetestRequest
	events := []VulnCardEvent{}
	for _, change := range changes {
		if change.Action != "vuln.retest_request" {
			continue
		}
		var detail retestRequestDetail
		_ = json.Unmarshal(change.Detail, &detail)
		events = append(events, VulnCardEvent{At: change.At, Kind: "retest_request", Title: "申请复测",
			Detail: capRunes(change.Summary, vulnCardDetailRunes), Actor: change.Actor})
		if latest != nil {
			continue
		}
		latest = &VulnCardRetestRequest{RetestID: detail.RetestID, SessionID: detail.SessionID, TaskID: detail.TaskID, FindingID: detail.FindingID,
			StartedAt: change.At, Status: retestRequestDone}
		result, err := readOptional[retestResultModel](ctx, c.store, "retest_id = ?", detail.RetestID)
		if err != nil {
			return nil, nil, err
		}
		if result == nil && c.sessionActive(ctx, detail.SessionID) {
			latest.Status = retestRequestRunning
		}
	}
	return latest, events, nil
}
