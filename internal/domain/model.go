package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound    = errors.New("记录不存在")
	ErrConflict    = errors.New("状态已变化或操作不适用")
	ErrInvalid     = errors.New("输入无效")
	ErrUnavailable = errors.New("能力未配置或暂时不可用")
)

type Mission struct {
	ID             string    `json:"id"`
	Title          string    `json:"title"`
	Origin         string    `json:"origin"`
	Goal           string    `json:"goal"`
	AllowedOrigins []string  `json:"allowed_origins"`
	CreatedAt      time.Time `json:"created_at"`
}

type Thread struct {
	ID             string    `json:"id"`
	MissionID      string    `json:"mission_id"`
	Title          string    `json:"title"`
	Question       string    `json:"question"`
	Rationale      string    `json:"rationale"`
	NextStep       string    `json:"next_step"`
	Status         string    `json:"status"`
	Priority       int       `json:"priority"`
	AutoRun        bool      `json:"auto_run"`
	PauseRequested bool      `json:"pause_requested"`
	StepCount      int       `json:"step_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Attempt struct {
	ID        string     `json:"id"`
	ThreadID  string     `json:"thread_id"`
	Status    string     `json:"status"`
	Summary   string     `json:"summary"`
	Error     string     `json:"error"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

type Observation struct {
	ID        string    `json:"id"`
	ThreadID  string    `json:"thread_id"`
	AttemptID string    `json:"attempt_id,omitempty"`
	ActionID  string    `json:"action_id,omitempty"`
	Kind      string    `json:"kind"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type ActionInput struct {
	Kind    string            `json:"kind"` // http 或 local_command，参数创建后不可变。
	Method  string            `json:"method,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
	Command string            `json:"command,omitempty"`
	Reason  string            `json:"reason"`
}

type ActionResult struct {
	StatusCode    int                 `json:"status_code,omitempty"`
	Headers       map[string][]string `json:"headers,omitempty"`
	Body          []byte              `json:"body_base64,omitempty"`
	Stdout        []byte              `json:"stdout_base64,omitempty"`
	Stderr        []byte              `json:"stderr_base64,omitempty"`
	ExitCode      int                 `json:"exit_code"`
	Truncated     bool                `json:"truncated"`
	EffectUnknown bool                `json:"effect_unknown"`
	ResolvedIP    string              `json:"resolved_ip,omitempty"`
	DurationMS    int64               `json:"duration_ms"`
}

type Action struct {
	ID             string        `json:"id"`
	ThreadID       string        `json:"thread_id"`
	AttemptID      string        `json:"attempt_id,omitempty"`
	Input          ActionInput   `json:"input"`
	RequestHash    string        `json:"request_hash"`
	Status         string        `json:"status"`
	Result         *ActionResult `json:"result,omitempty"`
	Error          string        `json:"error"`
	DecisionReason string        `json:"decision_reason"`
	ResolvedAt     *time.Time    `json:"resolved_at,omitempty"`
	ResolutionNote string        `json:"resolution_note,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type Event struct {
	ID                int64     `json:"id"`
	MissionID         string    `json:"mission_id"`
	ThreadID          string    `json:"thread_id,omitempty"`
	Kind              string    `json:"kind"`
	Message           string    `json:"message"`
	CreatedAt         time.Time `json:"created_at"`
	FindingID         string    `json:"finding_id,omitempty"`
	FindingRevisionID string    `json:"finding_revision_id,omitempty"`
}

type Snapshot struct {
	Mission      Mission       `json:"mission"`
	Thread       Thread        `json:"thread"`
	Observations []Observation `json:"observations"`
	Actions      []Action      `json:"actions"`
	Attempts     []Attempt     `json:"attempts"`
	// 仅由模型路由器从当前任务不可变资料装配，普通Snapshot接口保持不加载正文。
	ResearchContext  *ResearchContext     `json:"research_context,omitempty"`
	SkillContext     *SkillContext        `json:"skill_context,omitempty"`
	Supplements      []Supplement         `json:"operator_supplements,omitempty"`
	ContextSelection *ContextSelection    `json:"context_selection,omitempty"`
	Assembly         *ContextAssembly     `json:"-"`
	Findings         []FindingRevision    `json:"findings,omitempty"`
	Verification     *VerificationContext `json:"verification_context,omitempty"`
}

// Decision 是下一项研究活动，不是漏洞真实性判决。
type Decision struct {
	Kind         string                  `json:"kind"` // observe, action, delegate, conclude, block, skill_discover/load/release
	Summary      string                  `json:"summary"`
	NextStep     string                  `json:"next_step"`
	Observation  string                  `json:"observation,omitempty"`
	Action       *ActionInput            `json:"action,omitempty"`
	Title        string                  `json:"title,omitempty"`
	Question     string                  `json:"question,omitempty"`
	Rationale    string                  `json:"rationale,omitempty"`
	Skill        *SkillRequest           `json:"skill,omitempty"`
	Finding      *FindingProposal        `json:"finding,omitempty"`
	Verification *VerificationConclusion `json:"verification,omitempty"`
}

type Model interface {
	Name() string
	Decide(context.Context, Snapshot) (Decision, error)
}

type Executor interface {
	Validate(ActionInput, []string) error
	Execute(context.Context, ActionInput, []string) (ActionResult, error)
}

// Repository 的迁移由持久层原子完成；零值返回只用于无可认领工作。
type Repository interface {
	// StartExploration 原子创建并启用初始方向；同键回放优先于新启动能力检查。
	StartExploration(context.Context, string, ExplorationInput, bool) (ExplorationStart, error) // key, 规范化输入, modelConfigured
	GetOperation(context.Context, string) (OperationReceipt, error)
	CreateMission(context.Context, Mission) (Mission, error)
	ListMissions(context.Context) ([]Mission, error)
	GetMission(context.Context, string) (Mission, error)
	CreateThread(context.Context, Thread) (Thread, error)
	ListThreads(context.Context, string) ([]Thread, error)
	GetThread(context.Context, string) (Thread, error)
	SetThreadControl(context.Context, string, string, bool) (Thread, error) // ready 或 paused；未知动作仍阻止认领。
	Snapshot(context.Context, string) (Snapshot, error)
	AddObservation(context.Context, Observation) (Observation, error)
	ProposeAction(context.Context, string, string, ActionInput) (Action, error) // threadID, attemptID（人工为空）
	GetAction(context.Context, string) (Action, error)
	ListActions(context.Context, string) ([]Action, error) // missionID；空为全局
	ListHTTPRecords(context.Context, string, HTTPRecordQuery) (HTTPRecordPage, error)
	GetHTTPRecord(context.Context, string, string) (HTTPRecordDetail, error) // missionID, actionID；同一只读快照验证归属。
	DecideAction(context.Context, string, string, string) (Action, error)    // approved/rejected, reason
	ClaimAction(context.Context) (*Action, error)
	FinishAction(context.Context, string, string, ActionResult, string) error
	ResolveUnknownAction(context.Context, string, string) error                  // 人工已核对，仅解除阻塞，保留未知历史不改成成功。
	ClaimThread(context.Context, int) (*Attempt, error)                          // maxSteps；只认领 auto_run 且无 pending/approved/running/outcome_unknown 动作的方向。
	FinishAttempt(context.Context, string, string, string, string, string) error // id, attemptStatus, threadStatus, summary, nextStep
	ListEvents(context.Context, string, int64) ([]Event, error)
	Recover(context.Context) error // 仅进程启动且拥有单实例锁后调用。
	Close()
}
