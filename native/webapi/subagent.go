package webapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chainreactors/cyber/agent"
	agenthooks "github.com/chainreactors/cyber/agent/hooks"
	agentprompt "github.com/chainreactors/cyber/agent/prompt"
	"github.com/chainreactors/cyber/agent/subagent"
	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
	"github.com/uptrace/bun"
)

// M5a：命名子 Agent oracle_explorer 认领一个 Oracle 并行探索。
// 准入（tool.before）→ 同事务预占 Oracle 与登记预占行 → SessionStart 按（父会话, 调用 CallID）落表
// webapi_task_subsessions 并把 Oracle 认领给子会话 → 子会话经 taskForSession 归属同任务（请求/出口/审批/候选/黑板）
// → SessionEnd 结束并释放未结论 Oracle。契约见 native/SUBAGENT.md。

const (
	OracleExplorerName = "oracle_explorer"
	// maxOracleExplorers 是同一任务同时进行中（含已预占未启动）的子 Agent 上限：资源约束，不是实验性次数限制。
	maxOracleExplorers = 3
	// 子会话 Continue Run 不受 cfg.MaxTurns 约束，在模型请求策略处强制限制。
	maxOracleExplorerModelCalls = 24

	subsessionRunning = "running"
	subsessionEnded   = "ended"
	// subsessionInterrupted 是 Hub 重启时遗留 running 行的 stop_reason。
	subsessionInterrupted = "interrupted"

	// oracleActorAI 是子 Agent 生命周期（预占/释放）写 Oracle 事件的 actor。
	oracleActorAI = "ai"

	subsessionListLimit = 50
)

var (
	oracleExplorerTools  = []string{"webapi_http", "webapi_authz_compare", "webapi_finding", "webapi_board_read", "webapi_board_map", "webapi_board_fact", "webapi_board_oracle"}
	oraclePromptToken    = regexp.MustCompile(`^[Oo]-([0-9]{1,9})$`)
	errNoSubagentReserve = errors.New("无平台预占")
)

// ---- 表 ----

// subagentReservationModel 是准入放行后、子会话启动前的预占行；SessionStart 消费，未启动由 tool.completed 清理。
type subagentReservationModel struct {
	bun.BaseModel `bun:"table:webapi_subagent_reservations,alias:webapi_subagent_reservation"`

	ParentSessionID string `bun:"parent_session_id,pk"`
	ParentCallID    string `bun:"parent_call_id,pk"`
	TaskID          string `bun:"task_id,notnull"`
	OracleID        string `bun:"oracle_id,notnull"`
	CreatedAt       string `bun:"created_at,notnull"`
}

type taskSubsessionModel struct {
	bun.BaseModel `bun:"table:webapi_task_subsessions,alias:webapi_task_subsession"`

	SessionID       string         `bun:"session_id,pk"`
	TaskID          string         `bun:"task_id,notnull"`
	ParentSessionID string         `bun:"parent_session_id,notnull"`
	ParentCallID    string         `bun:"parent_call_id,notnull"`
	OracleID        string         `bun:"oracle_id,notnull"`
	AgentName       string         `bun:"agent_name,notnull"`
	Status          string         `bun:"status,notnull"`
	StopReason      string         `bun:"stop_reason,notnull"`
	Error           string         `bun:"error,type:text,notnull"`
	CreatedAt       string         `bun:"created_at,notnull"`
	EndedAt         sql.NullString `bun:"ended_at"`
}

// subsessionUsageModel 是节点侧观察到的子会话 Usage 事件（Hub 不持久化子会话事件，见 SUBAGENT.md）；id=事件 id。
type subsessionUsageModel struct {
	bun.BaseModel `bun:"table:webapi_subsession_usage,alias:webapi_subsession_usage"`

	ID               string `bun:"id,pk"`
	SessionID        string `bun:"session_id,notnull"`
	TaskID           string `bun:"task_id,notnull"`
	Model            string `bun:"model,notnull"`
	InputTokens      int64  `bun:"input_tokens,notnull"`
	OutputTokens     int64  `bun:"output_tokens,notnull"`
	TotalTokens      int64  `bun:"total_tokens,notnull"`
	CacheReadTokens  int64  `bun:"cache_read_tokens,notnull"`
	CacheWriteTokens int64  `bun:"cache_write_tokens,notnull"`
	CreatedAt        string `bun:"created_at,notnull"`
}

// ---- 会话识别 ----

func (s *taskStore) subsessionFor(ctx context.Context, sessionID string) (*taskSubsessionModel, error) {
	if sessionID == "" {
		return nil, nil
	}
	var model taskSubsessionModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&model).Where("session_id = ?", sessionID).Limit(1).Scan(ctx)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

// oracleSubagentHolder 返回当前持有该 Oracle 的子 Agent：运行中子会话 id，或预占中（未启动）为 "reserved"；无则空串。
func (s *taskStore) oracleSubagentHolder(ctx context.Context, db bun.IDB, oracleID string) (string, error) {
	var running []taskSubsessionModel
	if err := db.NewSelect().Model(&running).Where("oracle_id = ? AND status = ?", oracleID, subsessionRunning).Limit(1).Scan(ctx); err != nil {
		return "", err
	}
	if len(running) > 0 {
		return running[0].SessionID, nil
	}
	count, err := db.NewSelect().Model((*subagentReservationModel)(nil)).Where("oracle_id = ?", oracleID).Count(ctx)
	if err != nil {
		return "", err
	}
	if count > 0 {
		return "reserved", nil
	}
	return "", nil
}

// ---- 准入 ----

type subagentCallArgs struct {
	Action    string `json:"action"`
	Prompt    string `json:"prompt"`
	Name      string `json:"name"`
	Label     string `json:"label"`
	SessionID string `json:"session_id"`
	Mode      string `json:"mode"`
	Timeout   string `json:"timeout"`
}

func subagentDenied(reason string) toolhooks.Admission {
	return toolhooks.Admission{Deny: fmt.Errorf("%w: 已拒绝 subagent：%s", operation.ErrDenied, reason)}
}

// admitSubagent 是 subagent 工具的平台准入（替换 M5-0 的一刀切拒绝）；放行 create 时同事务预占 Oracle。
func (c *Capability) admitSubagent(ctx context.Context, event toolhooks.CallEvent) (toolhooks.Admission, error) {
	if event.Call.GetName() != "subagent" {
		return toolhooks.Admission{}, nil
	}
	inv := operation.InvocationFromContext(ctx)
	var args subagentCallArgs
	if err := json.Unmarshal(event.Call.GetArguments().GetData(), &args); err != nil {
		return subagentDenied("参数不是合法 JSON"), nil
	}
	switch args.Action {
	case "catalog", "list":
		return toolhooks.Admission{}, nil
	case "kill":
		return c.admitSubagentKill(ctx, inv, args.SessionID), nil
	case "", "create":
	default:
		return subagentDenied("未知 action " + args.Action), nil
	}
	if args.Name != OracleExplorerName {
		return subagentDenied(`只开放命名子 Agent oracle_explorer（name="oracle_explorer"），匿名或其它名字不放行`), nil
	}
	switch args.Mode {
	case "", string(subagent.Async), string(subagent.Sync):
	default:
		return subagentDenied("mode=" + args.Mode + " 不放行（fork 会带入父会话历史）；用 async（默认）或 sync"), nil
	}
	if inv.SessionID == "" || inv.CallID == "" {
		return subagentDenied("缺少可信原生 session/call 上下文"), nil
	}
	task, err := c.store.researchTaskForSession(ctx, inv.SessionID)
	if err != nil {
		return subagentDenied("无法核对调用会话所属任务"), nil
	}
	if task == nil {
		return subagentDenied("只有 Web/API 任务的研究会话可派出 oracle_explorer；验证/复测/报告会话、普通对话与子 Agent 本身不能派出"), nil
	}
	fields := strings.Fields(args.Prompt)
	if len(fields) == 0 {
		return subagentDenied(`prompt 须以本任务 Oracle 编号开头（如 "O-001 先读黑板……"）`), nil
	}
	match := oraclePromptToken.FindStringSubmatch(fields[0])
	if match == nil {
		return subagentDenied(fmt.Sprintf(`prompt 首个词 %q 不是 Oracle 编号；须以本任务 Oracle 编号开头（如 "O-001 …"）`, capRunes(fields[0], 40))), nil
	}
	number, ok := parseOracleNumber(match[1])
	if !ok {
		return subagentDenied("Oracle 编号非法：" + fields[0]), nil
	}
	if err := c.store.reserveOracleExplorer(ctx, task.ID, number, inv.SessionID, inv.CallID, time.Now()); err != nil {
		var input boardInputError
		if errors.As(err, &input) {
			return subagentDenied(input.message), nil
		}
		return subagentDenied("预占 Oracle 失败"), nil
	}
	return toolhooks.Admission{}, nil
}

// admitSubagentKill 只允许研究会话 kill 本任务运行中的子会话。
func (c *Capability) admitSubagentKill(ctx context.Context, inv operation.Invocation, target string) toolhooks.Admission {
	task, err := c.store.researchTaskForSession(ctx, inv.SessionID)
	if err != nil || task == nil {
		return subagentDenied("只有 Web/API 任务的研究会话可 kill 本任务的子 Agent")
	}
	sub, err := c.store.subsessionFor(ctx, target)
	if err != nil || sub == nil || sub.TaskID != task.ID {
		return subagentDenied(fmt.Sprintf("session_id=%q 不是本任务的子 Agent 会话", target))
	}
	return toolhooks.Admission{}
}

// reserveOracleExplorer 在一个写事务内校验 Oracle 状态与并发上限，置 in_progress、写事件并登记预占。拒绝不改任何状态。
func (s *taskStore) reserveOracleExplorer(ctx context.Context, taskID string, number int64, parentSessionID, callID string, now time.Time) error {
	at := boardNow(now)
	label := oracleNumber(number)
	return s.boardTx(ctx, taskID, func(ctx context.Context, tx bun.Tx) error {
		existing, err := tx.NewSelect().Model((*subagentReservationModel)(nil)).Where("parent_session_id = ? AND parent_call_id = ?", parentSessionID, callID).Count(ctx)
		if err != nil {
			return err
		}
		if existing > 0 {
			return nil // 同一调用重复准入：已预占。
		}
		var oracle boardOracleModel
		err = tx.NewSelect().Model(&oracle).Where("task_id = ? AND number = ?", taskID, number).Limit(1).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return boardInputError{"本任务不存在 " + label}
		}
		if err != nil {
			return err
		}
		switch {
		case oracle.Status == oracleOpen:
		case oracle.Status == oracleInProgress && (oracle.ClaimedSessionID == "" || oracle.ClaimedSessionID == parentSessionID):
		case oracle.Status == oracleInProgress:
			return boardInputError{fmt.Sprintf("%s 已被会话 %s 认领，不能再派子 Agent", label, oracle.ClaimedSessionID)}
		default:
			return boardInputError{fmt.Sprintf("%s 已是终态 %s，不能派子 Agent；如需重新评估，请用户先在任务页重开", label, oracle.Status)}
		}
		holder, err := s.oracleSubagentHolder(ctx, tx, oracle.ID)
		if err != nil {
			return err
		}
		if holder != "" {
			return boardInputError{label + " 已有子 Agent 在探索，不能重复派出"}
		}
		running, err := tx.NewSelect().Model((*taskSubsessionModel)(nil)).Where("task_id = ? AND status = ?", taskID, subsessionRunning).Count(ctx)
		if err != nil {
			return err
		}
		reserved, err := tx.NewSelect().Model((*subagentReservationModel)(nil)).Where("task_id = ?", taskID).Count(ctx)
		if err != nil {
			return err
		}
		if running+reserved >= maxOracleExplorers {
			return boardInputError{fmt.Sprintf("本任务进行中的子 Agent 已达上限 %d 个，等其结束后再派", maxOracleExplorers)}
		}
		from := oracle.Status
		oracle.Status, oracle.ClaimedSessionID, oracle.UpdatedAt = oracleInProgress, "", at
		if _, err := tx.NewUpdate().Model(&oracle).Column("status", "claimed_session_id", "updated_at").WherePK().Exec(ctx); err != nil {
			return err
		}
		if err := insertOracleEvent(ctx, tx, oracle, from, oracleInProgress, oracleActorAI, parentSessionID, at, "子 Agent 认领"); err != nil {
			return err
		}
		_, err = tx.NewInsert().Model(&subagentReservationModel{ParentSessionID: parentSessionID, ParentCallID: callID, TaskID: taskID, OracleID: oracle.ID, CreatedAt: at}).Exec(ctx)
		return err
	})
}

// releaseOracleTx 把仍由 holder（子会话 id；空串＝预占中未认领）持有的 in_progress Oracle 退回 open。
func releaseOracleTx(ctx context.Context, tx bun.Tx, oracleID, holder, sessionID, at, summary string) error {
	var oracle boardOracleModel
	err := tx.NewSelect().Model(&oracle).Where("id = ?", oracleID).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if oracle.Status != oracleInProgress || oracle.ClaimedSessionID != holder {
		return nil
	}
	oracle.Status, oracle.ClaimedSessionID, oracle.UpdatedAt = oracleOpen, "", at
	if _, err := tx.NewUpdate().Model(&oracle).Column("status", "claimed_session_id", "updated_at").WherePK().Exec(ctx); err != nil {
		return err
	}
	return insertOracleEvent(ctx, tx, oracle, oracleInProgress, oracleOpen, oracleActorAI, sessionID, at, summary)
}

// releaseSubagentReservation 清理未被 SessionStart 消费的预占（子会话未启动），Oracle 退回 open。
func (s *taskStore) releaseSubagentReservation(ctx context.Context, parentSessionID, callID string, now time.Time) error {
	var reservation subagentReservationModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&reservation).Where("parent_session_id = ? AND parent_call_id = ?", parentSessionID, callID).Limit(1).Scan(ctx)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	at := boardNow(now)
	return s.boardTx(ctx, reservation.TaskID, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewDelete().Model((*subagentReservationModel)(nil)).Where("parent_session_id = ? AND parent_call_id = ?", parentSessionID, callID).Exec(ctx)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return releaseOracleTx(ctx, tx, reservation.OracleID, "", parentSessionID, at, "子 Agent 未启动，退回 open")
	})
}

// ---- 会话生命周期 ----

// subagentSessionStart（FailClosed）：只处理委派子会话（ParentToolCallID 非空，上游 sessionexec 总会设置）。
// 有预占 → 落表并把 Oracle 认领给子会话；父会话属于任务却无预占或名字不符 → 拒绝启动。
// 会话轮换（/clear、/compact、恢复）产生的续接会话 ParentToolCallID 为空，直接放行、不落表。
func (c *Capability) subagentSessionStart(ctx context.Context, event agenthooks.SessionEvent) (struct{}, error) {
	if event.ParentID == "" || event.ParentToolCallID == "" {
		return struct{}{}, nil
	}
	var reservation subagentReservationModel
	err := c.store.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&reservation).Where("parent_session_id = ? AND parent_call_id = ?", event.ParentID, event.ParentToolCallID).Limit(1).Scan(ctx)
	})
	if errors.Is(err, sql.ErrNoRows) {
		task, taskErr := c.store.taskForSession(ctx, event.ParentID)
		if taskErr != nil {
			return struct{}{}, fmt.Errorf("无法核对父会话所属任务，子会话不启动: %w", taskErr)
		}
		if task != nil {
			return struct{}{}, fmt.Errorf("父会话属于任务 %s，但子会话无平台预占（须经 subagent 准入派出 oracle_explorer），不启动", task.ID)
		}
		return struct{}{}, nil
	}
	if err != nil {
		return struct{}{}, fmt.Errorf("无法读取子 Agent 预占，子会话不启动: %w", err)
	}
	if agentType := event.Delegation.GetAgentType(); agentType != OracleExplorerName {
		return struct{}{}, fmt.Errorf("子会话类型 %q 不是 oracle_explorer，不启动", agentType)
	}
	at := boardNow(time.Now())
	err = c.store.boardTx(ctx, reservation.TaskID, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewDelete().Model((*subagentReservationModel)(nil)).Where("parent_session_id = ? AND parent_call_id = ?", event.ParentID, event.ParentToolCallID).Exec(ctx)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return errNoSubagentReserve
		}
		row := &taskSubsessionModel{SessionID: event.SessionID, TaskID: reservation.TaskID, ParentSessionID: event.ParentID, ParentCallID: event.ParentToolCallID,
			OracleID: reservation.OracleID, AgentName: OracleExplorerName, Status: subsessionRunning, CreatedAt: at}
		if _, err := tx.NewInsert().Model(row).Exec(ctx); err != nil {
			return err
		}
		_, err = tx.NewUpdate().Model((*boardOracleModel)(nil)).Set("claimed_session_id = ?", event.SessionID).Set("updated_at = ?", at).
			Where("id = ? AND status = ? AND claimed_session_id = ''", reservation.OracleID, oracleInProgress).Exec(ctx)
		return err
	})
	if err != nil {
		return struct{}{}, fmt.Errorf("登记子会话失败，不启动: %w", err)
	}
	c.subsessionModelCalls.Store(event.SessionID, &atomic.Int64{})
	c.subsessions.Store(event.SessionID, reservation.TaskID)
	return struct{}{}, nil
}

// subagentSessionEnd：行置 ended，未结论的 Oracle 退回 open。
func (c *Capability) subagentSessionEnd(ctx context.Context, event agenthooks.SessionEvent) (struct{}, error) {
	if event.ParentID == "" || event.ParentToolCallID == "" {
		return struct{}{}, nil
	}
	c.subsessions.Delete(event.SessionID)
	c.subsessionModelCalls.Delete(event.SessionID)
	errText := ""
	if event.Err != nil {
		errText = capRunes(event.Err.Error(), 2000)
	}
	if err := c.store.endSubsession(context.WithoutCancel(ctx), event.SessionID, string(event.Stop), errText, time.Now()); err != nil {
		log.Printf("webapi 子会话 %s 结束登记失败：%v", event.SessionID, err)
	}
	return struct{}{}, nil
}

func (s *taskStore) endSubsession(ctx context.Context, sessionID, stop, errText string, now time.Time) error {
	sub, err := s.subsessionFor(ctx, sessionID)
	if err != nil || sub == nil || sub.Status != subsessionRunning {
		return err
	}
	at := boardNow(now)
	if stop == "" {
		stop = "unknown"
	}
	return s.boardTx(ctx, sub.TaskID, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewUpdate().Model((*taskSubsessionModel)(nil)).Set("status = ?", subsessionEnded).Set("stop_reason = ?", stop).Set("error = ?", errText).
			Set("ended_at = ?", at).Where("session_id = ? AND status = ?", sessionID, subsessionRunning).Exec(ctx)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		summary := "子 Agent 结束未给结论（" + stop + "）"
		if errText != "" {
			summary += "：" + errText
		}
		return releaseOracleTx(ctx, tx, sub.OracleID, sessionID, sessionID, at, summary)
	})
}

// releaseOrphanSubsessions：Hub 启动时（模型节点随 Hub 重启）遗留的 running 行按已中断结束、预占行清除，Oracle 同样释放。
func (s *taskStore) releaseOrphanSubsessions(ctx context.Context, now time.Time) error {
	var running []taskSubsessionModel
	if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&running).Where("status = ?", subsessionRunning).Scan(ctx)
	}); err != nil {
		return err
	}
	var result error
	for _, row := range running {
		result = errors.Join(result, s.endSubsession(ctx, row.SessionID, subsessionInterrupted, "Hub 重启时子会话仍在运行，按已中断结束", now))
	}
	var reservations []subagentReservationModel
	if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error { return db.NewSelect().Model(&reservations).Scan(ctx) }); err != nil {
		return errors.Join(result, err)
	}
	for _, reservation := range reservations {
		result = errors.Join(result, s.releaseSubagentReservation(ctx, reservation.ParentSessionID, reservation.ParentCallID, now))
	}
	return result
}

// ---- 用量（节点侧观察） ----

func (c *Capability) observeSubsessionUsage(event *aop.Event) {
	usage := event.GetUsage()
	if usage == nil || event.SessionId == "" {
		return
	}
	value, ok := c.subsessions.Load(event.SessionId)
	if !ok {
		return
	}
	taskID, _ := value.(string)
	total := usage.GetTotalTokens()
	if total == 0 {
		total = usage.GetInputTokens() + usage.GetOutputTokens()
	}
	at := time.Now().UTC()
	if event.EmittedAt != nil && event.EmittedAt.IsValid() {
		at = event.EmittedAt.AsTime().UTC()
	}
	id := event.Id
	if id == "" {
		id = "wsu_" + newSessionID() + newSessionID()
	}
	row := &subsessionUsageModel{ID: id, SessionID: event.SessionId, TaskID: taskID, Model: usage.GetModel(),
		InputTokens: int64(usage.GetInputTokens()), OutputTokens: int64(usage.GetOutputTokens()), TotalTokens: int64(total),
		CacheReadTokens: int64(usage.GetDetail()["cache_read"]), CacheWriteTokens: int64(usage.GetDetail()["cache_write"]), CreatedAt: at.Format(time.RFC3339Nano)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.store.write(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(row).On("CONFLICT (id) DO NOTHING").Exec(ctx)
		return err
	}); err != nil {
		log.Printf("webapi 子会话 %s 用量登记失败：%v", event.SessionId, err)
	}
}

// ---- 钩子装配（随 ApprovalExtension 装入，任何装了审批扩展的节点都强制准入与子会话归属） ----

func (c *Capability) subagentSubscriptions(registry *corehooks.Registry) []*corehooks.Subscription {
	return []*corehooks.Subscription{
		agenthooks.ModelRequestPolicy.On(registry, "wscyber-webapi-subsession-model-limit", func(ctx context.Context, event agenthooks.ModelRequestEvent) (agenthooks.ModelPolicy, error) {
			// 仅 SessionStart 已登记的子会话有计数；其它角色不受影响。
			if value, ok := c.subsessionModelCalls.Load(event.SessionID); ok && value.(*atomic.Int64).Add(1) > maxOracleExplorerModelCalls {
				return agenthooks.ModelPolicy{Deny: fmt.Errorf("子 Agent 模型调用已达上限 %d 次，停止；Oracle 未结案将退回 open", maxOracleExplorerModelCalls)}, nil
			}
			return agenthooks.ModelPolicy{}, nil
		}),
		toolhooks.Before.On(registry, "wscyber-webapi-subagent", c.admitSubagent),
		toolhooks.Completed.On(registry, "wscyber-webapi-subagent-release", func(ctx context.Context, completion toolhooks.Completion) (struct{}, error) {
			if completion.Call.GetName() != "subagent" {
				return struct{}{}, nil
			}
			inv := operation.InvocationFromContext(ctx)
			if inv.SessionID == "" || inv.CallID == "" {
				return struct{}{}, nil
			}
			if err := c.store.releaseSubagentReservation(context.WithoutCancel(ctx), inv.SessionID, inv.CallID, time.Now()); err != nil {
				log.Printf("webapi 子 Agent 预占清理失败：%v", err)
			}
			return struct{}{}, nil
		}),
		agenthooks.SessionStart.On(registry, "wscyber-webapi-subsession-start", c.subagentSessionStart),
		agenthooks.SessionEnd.On(registry, "wscyber-webapi-subsession-end", c.subagentSessionEnd),
	}
}

// ---- 命名子 Agent 定义 ----

// SubagentExtension 在上游 Subagent Point 注册 oracle_explorer；须装在 harness 自带的 subagent 扩展之后（harness.Config.Extensions 即可）。
func (c *Capability) SubagentExtension() extension.Extension {
	return extension.Func{LoadFunc: func(scope *extension.Scope) error {
		return extension.Add(scope, subagent.Subagent{
			Name:        OracleExplorerName,
			Description: "Explore exactly one open board oracle of the current Web/API task (prompt must start with its number, e.g. O-001); proves or refutes it with minimal requests and closes it.",
			DefaultMode: subagent.Async,
			Prepare:     c.prepareOracleExplorer,
		})
	}}
}

type filteredTools struct {
	base    coretool.Executor
	allowed map[string]bool
}

func (f filteredTools) ToolDefinitions() []*aop.ToolDefinition {
	var out []*aop.ToolDefinition
	for _, definition := range f.base.ToolDefinitions() {
		if f.allowed[definition.GetName()] {
			out = append(out, definition)
		}
	}
	return out
}

func (f filteredTools) ExecuteTool(ctx context.Context, name, arguments string) (*aop.ToolResult, error) {
	if !f.allowed[name] {
		return nil, fmt.Errorf("工具 %s 不对 oracle_explorer 开放（只可用 %s）", name, strings.Join(oracleExplorerTools, "/"))
	}
	return f.base.ExecuteTool(ctx, name, arguments)
}

// prepareOracleExplorer：核对预占 → 工具白名单 → 系统提示词（任务场景同一构建函数 + 子 Agent 段）→ 首条输入（Oracle 全文 + 父会话余下文本）。
func (c *Capability) prepareOracleExplorer(ctx context.Context, cfg agent.Config, input subagent.Input) (agent.Config, string, error) {
	inv := operation.InvocationFromContext(ctx)
	var reservation subagentReservationModel
	err := c.store.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&reservation).Where("parent_session_id = ? AND parent_call_id = ?", cfg.SessionID, inv.CallID).Limit(1).Scan(ctx)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return cfg, "", errors.New("oracle_explorer 无平台预占（须由任务研究会话经 subagent 准入派出），不启动")
	}
	if err != nil {
		return cfg, "", fmt.Errorf("读取子 Agent 预占失败: %w", err)
	}
	var oracle boardOracleModel
	if err := c.store.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&oracle).Where("id = ?", reservation.OracleID).Limit(1).Scan(ctx)
	}); err != nil {
		return cfg, "", fmt.Errorf("读取预占 Oracle 失败: %w", err)
	}
	scenario, err := c.store.scenarioForTask(ctx, reservation.TaskID)
	if err != nil {
		return cfg, "", fmt.Errorf("读取任务研究场景失败: %w", err)
	}
	policy, err := c.store.policyForTask(ctx, reservation.TaskID)
	if err != nil {
		return cfg, "", fmt.Errorf("读取任务测试策略失败: %w", err)
	}
	if cfg.Tools == nil {
		return cfg, "", errors.New("oracle_explorer 需要工具执行器")
	}
	allowed := map[string]bool{}
	for _, name := range oracleExplorerTools {
		allowed[name] = true
	}
	tools := filteredTools{base: cfg.Tools, allowed: allowed}
	cfg = cfg.WithTools(tools)
	base := ""
	if cfg.PromptResolver != nil {
		agentContext := agentprompt.AgentContext{Name: OracleExplorerName, Model: cfg.Model, Now: time.Now()}
		for _, definition := range tools.ToolDefinitions() {
			agentContext.Tools = append(agentContext.Tools, agentprompt.Tool{Name: definition.GetName(), Description: definition.GetDescription()})
		}
		base = cfg.PromptResolver.Build(ctx, agentprompt.Context{Target: agentprompt.MainSystem, Agent: agentContext}).Prompt
	}
	system := injectScenarioPrompt(base, scenario, policy.Scope) + "\n\n" + oracleExplorerSystemPrompt(oracleNumber(oracle.Number))
	cfg = cfg.WithSystemPrompt(system)
	return cfg, oracleExplorerTask(oracle, input.Prompt), nil
}

// oracleExplorerTask 是子会话首条输入：Oracle 全文 + 父会话 prompt 去掉首个编号后的余下文本。
func oracleExplorerTask(oracle boardOracleModel, prompt string) string {
	rest := strings.TrimSpace(prompt)
	if fields := strings.Fields(rest); len(fields) > 0 {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, fields[0]))
	}
	lines := []string{
		fmt.Sprintf("请探索 Oracle %s（你只负责这一个）。", oracleNumber(oracle.Number)),
		"- 问题：" + oracle.Question,
		fmt.Sprintf("- 类别：%s；优先级：%s", oracle.Category, oracle.Priority),
	}
	keys := decodeStrings(oracle.EndpointKeysJSON)
	if len(keys) == 0 {
		lines = append(lines, "- 端点：（未指定）")
	} else {
		lines = append(lines, "- 端点："+strings.Join(keys, "；"))
	}
	if rest != "" {
		lines = append(lines, "", "研究会话补充说明：", rest)
	}
	return strings.Join(lines, "\n")
}

// ---- 接口：GET /tasks/{id}/subsessions ----

type SubsessionOracle struct {
	ID       string `json:"id"`
	Number   string `json:"number"`
	Question string `json:"question"`
	Status   string `json:"status"`
}

type SubsessionItem struct {
	SessionID    string            `json:"session_id"`
	Oracle       *SubsessionOracle `json:"oracle"`
	Status       string            `json:"status"`
	StopReason   string            `json:"stop_reason"`
	Error        string            `json:"error"`
	CreatedAt    string            `json:"created_at"`
	EndedAt      *string           `json:"ended_at"`
	RequestCount int64             `json:"request_count"`
	FindingCount int64             `json:"finding_count"`
}

func (s *taskStore) listSubsessions(ctx context.Context, taskID string) ([]SubsessionItem, error) {
	var rows []taskSubsessionModel
	if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&rows).Where("task_id = ?", taskID).OrderExpr(`created_at COLLATE "C" DESC, session_id COLLATE "C" DESC`).Limit(subsessionListLimit).Scan(ctx)
	}); err != nil {
		return nil, err
	}
	items := make([]SubsessionItem, 0, len(rows))
	for _, row := range rows {
		item := SubsessionItem{SessionID: row.SessionID, Status: row.Status, StopReason: row.StopReason, Error: row.Error, CreatedAt: row.CreatedAt}
		if row.EndedAt.Valid {
			ended := row.EndedAt.String
			item.EndedAt = &ended
		}
		var oracles []boardOracleModel
		if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
			return db.NewSelect().Model(&oracles).Where("id = ? AND task_id = ?", row.OracleID, taskID).Limit(1).Scan(ctx)
		}); err != nil {
			return nil, err
		}
		if len(oracles) == 1 {
			item.Oracle = &SubsessionOracle{ID: oracles[0].ID, Number: oracleNumber(oracles[0].Number), Question: oracles[0].Question, Status: oracles[0].Status}
		}
		if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
			return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webapi_requests WHERE task_id = ? AND session_id = ?`, taskID, row.SessionID).Scan(&item.RequestCount)
		}); err != nil {
			return nil, err
		}
		if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
			return db.QueryRowContext(ctx, `SELECT COUNT(DISTINCT finding_id) FROM webapi_finding_revisions WHERE task_id = ? AND session_id = ?`, taskID, row.SessionID).Scan(&item.FindingCount)
		}); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *taskStore) taskHasRunningSubsession(ctx context.Context, taskID string) (bool, error) {
	count, err := readValue(ctx, s, func(ctx context.Context, db bun.IDB) (int64, error) {
		return db.NewSelect().Model((*taskSubsessionModel)(nil)).Where("task_id = ? AND status = ?", taskID, subsessionRunning).Count(ctx)
	})
	return count > 0, err
}

// runningSubsessionsOf 列出某父会话下运行中的子会话 id（审批 Pending/Resolve 经父会话聚合用）。
func (s *taskStore) runningSubsessionsOf(ctx context.Context, parentSessionID string) ([]string, error) {
	var rows []taskSubsessionModel
	if err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&rows).Column("session_id").Where("parent_session_id = ? AND status = ?", parentSessionID, subsessionRunning).OrderExpr(`session_id COLLATE "C" ASC`).Scan(ctx)
	}); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.SessionID)
	}
	return ids, nil
}

func (c *Capability) handleSubsessionList(w http.ResponseWriter, r *http.Request) {
	id, err := taskIDFromPath(r)
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
	items, err := c.store.listSubsessions(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取子 Agent 会话失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
