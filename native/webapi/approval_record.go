package webapi

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	"github.com/chainreactors/cyber/exts/guardrail"
)

// 审批记录（R2）：挂起落 pending 行、决定/超时/取消写终态、Hub 启动清理遗留、查询接口。契约见 native/POLICY.md「审批记录」。
// 内存运行时（approvalRuntime）仍负责真正挂起/放行调用；本表是权威记录。

const (
	approvalSummaryRunes = 200 // bash 摘要上限（同审计 operation）
	approvalPersistWait  = 10 * time.Second
	approvalPersistRetry = time.Second

	approvalExpiredReason   = "超时未处理，视为拒绝"
	approvalCancelledReason = "调用已取消（会话或轮次结束、节点关闭）"
	approvalUnscrubbed      = "（读取资产测试账号失败，摘要未保存）"
)

// approvalDraft 是 admitTool 在挂起前准备的记录（不含原始参数）；persist 回调补 operation 与时间后落库。
type approvalDraft struct {
	record approvalModel
	notify bool // 读到资产测试账号、能安全遮盖时才登记待审批运营通知
}

// approvalFinish 是一次写库失败、待重试的终态（进程内；见 retryApprovalBacklog）。
type approvalFinish struct {
	final approvalModel
	audit *auditEventModel
}

type approvalBacklog struct {
	mu    sync.Mutex
	items map[string]approvalFinish
}

// approvalSummary：webapi_http 为 方法+URL（去 userinfo），bash 为命令单行前 200 字，write 为路径；均经 scrub（口令占位 + guardrail 遮盖）。
func approvalSummary(tool, arguments string, scrub func(string) string) string {
	switch tool {
	case "webapi_http":
		if args, err := coretool.ParseArgs[httpToolArguments](arguments); err == nil {
			return strings.ToUpper(args.Method) + " " + scrub(stripURLUserinfo(args.URL))
		}
	case "bash":
		if args, err := coretool.ParseArgs[struct {
			Command string `json:"command"`
		}](arguments); err == nil {
			return capRunes(oneLine(scrub(args.Command)), approvalSummaryRunes)
		}
	case "write":
		if args, err := coretool.ParseArgs[struct {
			Path string `json:"path"`
		}](arguments); err == nil {
			return scrub(args.Path)
		}
	}
	return ""
}

// prepareApproval 读资产测试账号（口令明文 → {{cred:<id>.secret}}）与子 Agent 父会话，生成待落库的审批记录。
// 读不到资产密钥时摘要不落库、不登记通知（不冒险保存或外发明文）。
func (c *Capability) prepareApproval(ctx context.Context, inv operation.Invocation, task *Task, sub *taskSubsessionModel, tool, arguments, reason string) *approvalDraft {
	draft := &approvalDraft{notify: true, record: approvalModel{ID: newApprovalID(), SessionID: inv.SessionID, TurnID: inv.TurnID, CallID: inv.CallID,
		Tool: tool, Status: approvalStatusPending, HubInstance: c.instance}}
	var resolver *credentialResolver
	if task != nil {
		draft.record.TaskID = task.ID
		var err error
		if resolver, err = loadCredentialResolver(ctx, c.store, c.secrets, task.ID); err != nil {
			log.Printf("webapi 审批：读取任务 %s 的资产测试账号失败，审批摘要不落库：%v", task.ID, err)
			draft.notify = false
		}
	}
	if sub != nil {
		draft.record.ParentSessionID = sub.ParentSessionID
	}
	scrub := func(text string) string { return guardrail.RedactText(resolver.redact(text)) }
	draft.record.Reason = scrub(reason)
	if draft.notify {
		draft.record.Summary = approvalSummary(tool, arguments, scrub)
	} else {
		draft.record.Summary = approvalUnscrubbed
	}
	return draft
}

// persistApproval 是 review 的落库回调（PENDING 事件已发出后调用，失败由 review 每秒重试）：
// 补 operation、创建/到期时间，单事务写 pending 行与待登记的 approval_pending 通知。
func (c *Capability) persistApproval(ctx context.Context, draft *approvalDraft, review *guardrail.Review) error {
	record := &draft.record
	record.OperationID = review.GetOperation().GetOperationId()
	if record.CreatedAt == "" {
		record.CreatedAt = notifyTime(time.Now())
	}
	record.ExpiresAt = notifyTime(review.GetExpiresAt().AsTime())
	var job *notifyRegistrationModel
	if draft.notify {
		job = &notifyRegistrationModel{Key: notifyKeyApproval + record.OperationID, Kind: NotifyKindApprovalPending, ApprovalID: record.ID, ArgsJSON: "{}",
			NextAttemptAt: record.CreatedAt, CreatedAt: record.CreatedAt}
	}
	if err := c.store.insertApproval(ctx, record, job); err != nil {
		log.Printf("webapi 审批：记录 %s 落库失败，稍后重试：%v", record.OperationID, err)
		return err
	}
	if job != nil {
		c.wakeOps()
	}
	return nil
}

func approvalStatusOf(state guardrail.ReviewState) (status, decidedBy, reason string) {
	switch state {
	case guardrail.ReviewState_REVIEW_STATE_APPROVED:
		return approvalStatusApproved, auditActorUser, ""
	case guardrail.ReviewState_REVIEW_STATE_REJECTED:
		return approvalStatusRejected, auditActorUser, ""
	case guardrail.ReviewState_REVIEW_STATE_EXPIRED:
		return approvalStatusExpired, auditActorSystem, approvalExpiredReason
	default:
		return approvalStatusCancelled, auditActorSystem, approvalCancelledReason
	}
}

// finishApprovalRecord 写终态（同事务：审计、删除待登记、撤销未发出的待审批通知）。写库失败转入进程内补写，
// 由投递 goroutine 每轮重试；进程在补写成功前退出时，该行会在下次启动时按“Hub 重启”取消（见 POLICY.md 已知限制）。
func (c *Capability) finishApprovalRecord(ctx context.Context, draft *approvalDraft, state guardrail.ReviewState) {
	if draft == nil || draft.record.OperationID == "" {
		return // 未进入挂起（如重复 operation）：没有记录
	}
	final := draft.record
	final.Status, final.DecidedBy, final.StatusReason = approvalStatusOf(state)
	final.DecidedAt = sql.NullString{String: notifyTime(time.Now()), Valid: true}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), approvalPersistWait)
	defer cancel()
	var audit *auditEventModel
	assetID, _ := c.store.taskAssetID(ctx, final.TaskID)
	if entry := reviewAuditEntry(&final, assetID); entry != nil {
		var err error
		if audit, err = newAuditModel(*entry, c.scheduleClock()); err != nil {
			log.Printf("webapi 审计：生成 %s 失败：%v", entry.Action, err)
		}
	}
	if _, err := c.store.finishApproval(ctx, final, audit); err != nil {
		log.Printf("webapi 审批：记录 %s 终态 %s 落库失败，转入补写：%v", final.OperationID, final.Status, err)
		c.backlog.mu.Lock()
		if c.backlog.items == nil {
			c.backlog.items = map[string]approvalFinish{}
		}
		c.backlog.items[final.ID] = approvalFinish{final: final, audit: audit}
		c.backlog.mu.Unlock()
		c.wakeOps()
	}
}

// retryApprovalBacklog 补写此前失败的审批终态（投递 goroutine 每轮调用）。
func (c *Capability) retryApprovalBacklog(ctx context.Context) {
	c.backlog.mu.Lock()
	items := make([]approvalFinish, 0, len(c.backlog.items))
	for _, item := range c.backlog.items {
		items = append(items, item)
	}
	c.backlog.mu.Unlock()
	for _, item := range items {
		if ctx.Err() != nil {
			return
		}
		attempt, cancel := context.WithTimeout(ctx, opsRegisterWait)
		_, err := c.store.finishApproval(attempt, item.final, item.audit)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("webapi 审批：补写 %s 终态仍失败，保留重试：%v", item.final.OperationID, err)
			}
			continue
		}
		c.backlog.mu.Lock()
		delete(c.backlog.items, item.final.ID)
		c.backlog.mu.Unlock()
	}
}

// recoverInterruptedApprovals：Hub 启动时（模型节点随 Hub 进程启动）把上次进程遗留的 pending 行改为 cancelled
// （system，“Hub 重启，调用已中断”），并登记 approval.cancel 审计、撤销其待审批通知与待登记。
func (c *Capability) recoverInterruptedApprovals(ctx context.Context) error {
	rows, err := c.store.cancelInterruptedApprovals(ctx, c.instance, notifyTime(time.Now()), func(row approvalModel, assetID string) (*auditEventModel, error) {
		return newAuditModel(approvalCancelAudit(row, assetID), c.scheduleClock())
	})
	if err != nil {
		return err
	}
	if len(rows) > 0 {
		log.Printf("webapi 审批：Hub 重启，%d 项遗留待审批已标记为中断（cancelled）", len(rows))
	}
	return nil
}

// ---- 查询 ----

// ApprovalView 是审批记录接口的项（契约见 native/POLICY.md「审批记录」）。
type ApprovalView struct {
	ID              string  `json:"id"`
	OperationID     string  `json:"operation_id"`
	TaskID          string  `json:"task_id"`
	SessionID       string  `json:"session_id"`
	ParentSessionID string  `json:"parent_session_id"`
	ReviewSessionID string  `json:"review_session_id"`
	TurnID          string  `json:"turn_id"`
	CallID          string  `json:"call_id"`
	Tool            string  `json:"tool"`
	Summary         string  `json:"summary"`
	Reason          string  `json:"reason"`
	Status          string  `json:"status"`
	DecidedBy       string  `json:"decided_by"`
	StatusReason    string  `json:"status_reason"`
	CreatedAt       string  `json:"created_at"`
	ExpiresAt       string  `json:"expires_at"`
	DecidedAt       *string `json:"decided_at"`
	Actionable      bool    `json:"actionable"`
}

func (c *Capability) approvalView(row approvalModel) ApprovalView {
	view := ApprovalView{ID: row.ID, OperationID: row.OperationID, TaskID: row.TaskID, SessionID: row.SessionID, ParentSessionID: row.ParentSessionID,
		ReviewSessionID: row.SessionID, TurnID: row.TurnID, CallID: row.CallID, Tool: row.Tool, Summary: row.Summary, Reason: row.Reason, Status: row.Status,
		DecidedBy: row.DecidedBy, StatusReason: row.StatusReason, CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt}
	if row.ParentSessionID != "" {
		view.ReviewSessionID = row.ParentSessionID
	}
	if row.DecidedAt.Valid {
		at := row.DecidedAt.String
		view.DecidedAt = &at
	}
	view.Actionable = row.Status == approvalStatusPending && c.approvalStillPending(notifyKeyApproval+row.OperationID)
	return view
}

// handleApprovalList: GET /api/webapi/approvals?task_id=&session_id=&status=&limit=&cursor=
func (c *Capability) handleApprovalList(w http.ResponseWriter, r *http.Request) {
	c.serveApprovals(w, r, r.URL.Query().Get("task_id"))
}

// handleTaskApprovals: GET /api/webapi/tasks/{id}/approvals?session_id=&status=&limit=&cursor=（任务不存在 404）
func (c *Capability) handleTaskApprovals(w http.ResponseWriter, r *http.Request) {
	taskID, err := pathID(r, "id", "任务")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	task, err := c.store.Get(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务失败")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "任务不存在")
		return
	}
	c.serveApprovals(w, r, taskID)
}

func (c *Capability) serveApprovals(w http.ResponseWriter, r *http.Request, taskID string) {
	limit, _, _, err := listParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := r.URL.Query()
	filter := approvalFilter{taskID: taskID, sessionID: query.Get("session_id"), status: query.Get("status"), limit: limit + 1}
	if filter.taskID != "" && validPathID(filter.taskID) != nil {
		writeError(w, http.StatusBadRequest, "task_id 非法")
		return
	}
	if filter.sessionID != "" && validPathID(filter.sessionID) != nil {
		writeError(w, http.StatusBadRequest, "session_id 非法")
		return
	}
	if filter.status != "" && !approvalStatuses[filter.status] {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("status 只能是 %s/%s/%s/%s/%s", approvalStatusPending, approvalStatusApproved, approvalStatusRejected, approvalStatusExpired, approvalStatusCancelled))
		return
	}
	afterAt, afterID, hasCursor, err := decodeKeyCursor(query.Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if hasCursor {
		filter.afterAt, filter.afterID = notifyTime(afterAt), afterID
	}
	rows, pending, err := c.store.approvals(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取审批记录失败")
		return
	}
	var next any
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeKeyCursor(last.CreatedAt, last.ID)
	}
	items := make([]ApprovalView, 0, len(rows))
	for _, row := range rows {
		items = append(items, c.approvalView(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "pending_count": pending, "has_more": hasMore, "next_cursor": next})
}
