package webapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	agenthooks "github.com/chainreactors/cyber/agent/hooks"
	agentprompt "github.com/chainreactors/cyber/agent/prompt"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/resource"
	types "github.com/chainreactors/cyber/core/types"
	"github.com/uptrace/bun"
	"google.golang.org/protobuf/types/known/anypb"
)

// 持续探索（M3）：任务研究会话的 Run 走上游 Goal 模式（agent/evaluator.RunWithEval）。
// 每轮结束由独立评估调用按任务完成标准判 pass/continue，反馈作为下一轮输入，最大轮数为硬上限。
// 本文件：最大轮数的存储与校验、研究会话 RunTurn 的 AgentRunOptions、评估提示词追加平台黑板段。
// 契约见 native/SCENARIO.md「持续探索」。不改上游循环。

const (
	exploreMinRounds = 1
	exploreMaxRounds = 20

	// exploreDigestChars 是评估提示词中黑板摘要的字数上限（boardDigest 参数）。
	exploreDigestChars = 2000
	// explorePendingTTL：研究 Run 结束（RunEnd）到其评估提示词构建之间只隔同一 goroutine 内的几步调用；
	// 超过该时间仍未被认领的记录（例如该 Run 未开 Goal 模式）视为过期。
	explorePendingTTL = 2 * time.Minute
	// exploreProgressTTL：评估进度（上一轮黑板计数与连续无新增轮数）的保留时间。
	exploreProgressTTL = 24 * time.Hour

	exploreSectionID = "wscyber.explore.board"
)

var errMaxRounds = fmt.Errorf("max_rounds 须为 %d..%d 的整数", exploreMinRounds, exploreMaxRounds)

func validMaxRounds(value int) bool { return value >= exploreMinRounds && value <= exploreMaxRounds }

// scenarioMaxRounds 返回场景默认最大轮数；未知场景按默认场景。
func scenarioMaxRounds(key string) int {
	if def, ok := scenarioDefOf(key); ok {
		return def.MaxRounds
	}
	def, _ := scenarioDefOf(ScenarioWebAPIPentest)
	return def.MaxRounds
}

// ---- 存储：本项目 schema 升级只补表不 ALTER，最大轮数用独立表（与场景表同键）。 ----

type taskExploreModel struct {
	bun.BaseModel `bun:"table:webapi_task_explore,alias:webapi_task_explore"`

	TaskID    string `bun:"task_id,pk"`
	MaxRounds int    `bun:"max_rounds,notnull"`
	UpdatedAt string `bun:"updated_at,notnull"`
}

type assetExploreModel struct {
	bun.BaseModel `bun:"table:webapi_asset_explore,alias:webapi_asset_explore"`

	AssetID   string `bun:"asset_id,pk"`
	MaxRounds int    `bun:"max_rounds,notnull"`
	UpdatedAt string `bun:"updated_at,notnull"`
}

// taskMaxRounds 读任务保存的最大轮数；无记录返回 0（用场景默认）。
func taskMaxRounds(ctx context.Context, db bun.IDB, taskID string) (int, error) {
	var model taskExploreModel
	err := db.NewSelect().Model(&model).Where("task_id = ?", taskID).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return model.MaxRounds, err
}

// assetMaxRounds 批量读资产保存的最大轮数（无记录的资产不在结果中）。
func assetMaxRounds(ctx context.Context, db bun.IDB, assetIDs ...string) (map[string]int, error) {
	out := map[string]int{}
	if len(assetIDs) == 0 {
		return out, nil
	}
	var models []assetExploreModel
	if err := db.NewSelect().Model(&models).Where("asset_id IN (?)", bun.In(assetIDs)).Scan(ctx); err != nil {
		return nil, err
	}
	for _, model := range models {
		out[model.AssetID] = model.MaxRounds
	}
	return out, nil
}

func putTaskMaxRounds(ctx context.Context, db bun.IDB, taskID string, rounds int) error {
	_, err := db.NewInsert().Model(&taskExploreModel{TaskID: taskID, MaxRounds: rounds, UpdatedAt: scenarioNow()}).
		On("CONFLICT (task_id) DO UPDATE").Set("max_rounds = EXCLUDED.max_rounds").Set("updated_at = EXCLUDED.updated_at").Exec(ctx)
	return err
}

func putAssetMaxRounds(ctx context.Context, db bun.IDB, assetID string, rounds int) error {
	_, err := db.NewInsert().Model(&assetExploreModel{AssetID: assetID, MaxRounds: rounds, UpdatedAt: scenarioNow()}).
		On("CONFLICT (asset_id) DO UPDATE").Set("max_rounds = EXCLUDED.max_rounds").Set("updated_at = EXCLUDED.updated_at").Exec(ctx)
	return err
}

// ---- 研究会话 RunTurn 选项 ----

// exploreRunOptions：评估标准＝生效完成标准，轮数＝最大轮数（上游 ParseBudget 把纯数字当硬上限）。
func exploreRunOptions(scenario TaskScenario) *types.AgentRunOptions {
	return &types.AgentRunOptions{EvalCriteria: scenario.DoneCriteriaEffective, EvalRounds: strconv.Itoa(scenario.MaxRounds)}
}

// researchRunOptions 每次读库：改场景/完成标准/轮数后，下一次发起生效。
func (s *taskStore) researchRunOptions(ctx context.Context, taskID string) (*types.AgentRunOptions, error) {
	scenario, err := s.scenarioForTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return exploreRunOptions(scenario), nil
}

// packRunOptions 把 AgentRunOptions 打成 RunTurnRequest.Extensions（上游 RunAOPTurn 按类型解出）。
func packRunOptions(options *types.AgentRunOptions) ([]*anypb.Any, error) {
	if options == nil || strings.TrimSpace(options.EvalCriteria) == "" {
		return nil, nil
	}
	value, err := anypb.New(options)
	if err != nil {
		return nil, err
	}
	return []*anypb.Any{value}, nil
}

// ---- 评估提示词：研究会话的评估追加平台黑板段 ----
//
// 上游 evaluator.Evaluate 经 prompt Resolver 构建 EvaluatorSystem / EvaluatorRequest，Contribution 只拿到
// ctx 与 EvaluationContext{Goal, Criteria, Progress, Trace}：ctx 不带会话身份（Invocation 只在工具/BeforeModel
// 上设置）。因此由 RunEnd 钩子（与评估同一 goroutine、紧接在评估前同步触发，带 SessionID）登记
// “待评估的研究 Run”，Contribution 用 Trace 首行的消息数（= RunEnd.Messages 条数，同一 Result）与
// Criteria（= 任务生效完成标准）认领唯一一条；认领不到或不唯一就不追加（只记日志），不会把别的任务的黑板
// 交给评估器。

type explorePending struct {
	taskID   string
	turnID   string
	criteria string
	messages int
	at       time.Time
}

type exploreProgress struct {
	facts       int64 // 全部事实条数（含撤回，单调）
	conclusions int64 // 结论事件数（转为 holds/refuted/blocked，单调）
	idle        int   // 连续无新增的评估轮数
	at          time.Time
}

type exploreTracker struct {
	store *taskStore
	now   func() time.Time

	mu       sync.Mutex
	pending  map[string]explorePending  // 研究会话 → 最近一次结束、尚未评估的 Run
	progress map[string]exploreProgress // 研究会话 → 上一轮评估时的黑板计数
}

func newExploreTracker(store *taskStore) *exploreTracker {
	return &exploreTracker{store: store, now: time.Now, pending: map[string]explorePending{}, progress: map[string]exploreProgress{}}
}

// registerExplore 在 ToolExtension 中注册 RunEnd 钩子与 EvaluatorRequest Contribution。
// 宿主没有 prompt 扩展（只装工具的测试宿主）时不注册 Contribution。
func (c *Capability) registerExplore(scope *extension.Scope, registry *corehooks.Registry) (*corehooks.Subscription, error) {
	tracker := newExploreTracker(c.store)
	contribution := agentprompt.Contribution{Name: "wscyber.webapi.explore", Targets: []agentprompt.Target{agentprompt.EvaluatorRequest}, Apply: tracker.applyEvaluator}
	if err := extension.Add(scope, contribution); err != nil && !errors.Is(err, resource.ErrTypeUnknown) {
		return nil, err
	}
	return agenthooks.RunEnd.On(registry, "wscyber-webapi-explore", tracker.runEnd), nil
}

func (x *exploreTracker) runEnd(ctx context.Context, event agenthooks.RunEndEvent) (struct{}, error) {
	if event.SessionID == "" {
		return struct{}{}, nil
	}
	if event.Stop == agenthooks.StopReasonError || event.Stop == agenthooks.StopReasonCanceled {
		// 上游对出错/取消的 Run 不评估，直接结束循环。
		x.mu.Lock()
		delete(x.pending, event.SessionID)
		x.mu.Unlock()
		return struct{}{}, nil
	}
	task, err := x.store.researchTaskForSession(ctx, event.SessionID)
	if err != nil {
		log.Printf("webapi 持续探索：读取会话任务失败：%v", err)
		return struct{}{}, nil
	}
	if task == nil {
		return struct{}{}, nil
	}
	scenario, err := x.store.scenarioForTask(ctx, task.ID)
	if err != nil {
		log.Printf("webapi 持续探索：读取任务 %s 研究场景失败：%v", task.ID, err)
		return struct{}{}, nil
	}
	now := x.now()
	x.mu.Lock()
	defer x.mu.Unlock()
	x.pruneLocked(now)
	x.pending[event.SessionID] = explorePending{taskID: task.ID, turnID: event.TurnID, criteria: strings.TrimSpace(scenario.DoneCriteriaEffective), messages: len(event.Messages), at: now}
	return struct{}{}, nil
}

func (x *exploreTracker) pruneLocked(now time.Time) {
	for sessionID, item := range x.pending {
		if now.Sub(item.at) > explorePendingTTL {
			delete(x.pending, sessionID)
		}
	}
	for sessionID, item := range x.progress {
		if now.Sub(item.at) > exploreProgressTTL {
			delete(x.progress, sessionID)
		}
	}
}

var (
	traceMessagesPattern = regexp.MustCompile(`\| Messages: (\d+) \|`)
	progressRoundPattern = regexp.MustCompile(`(?m)^Round (\d+)\b`)
)

// claim 认领本次评估对应的研究 Run：消息数与完成标准都一致且唯一。
func (x *exploreTracker) claim(evaluation agentprompt.EvaluationContext) (string, explorePending, bool) {
	match := traceMessagesPattern.FindStringSubmatch(evaluation.Trace)
	if match == nil {
		return "", explorePending{}, false
	}
	messages, _ := strconv.Atoi(match[1])
	criteria := strings.TrimSpace(evaluation.Criteria)
	x.mu.Lock()
	defer x.mu.Unlock()
	x.pruneLocked(x.now())
	found := []string{}
	for sessionID, item := range x.pending {
		if item.messages == messages && item.criteria == criteria {
			found = append(found, sessionID)
		}
	}
	if len(found) != 1 {
		if len(found) > 1 {
			log.Printf("webapi 持续探索：%d 个研究会话同时匹配本次评估，不追加黑板", len(found))
		}
		return "", explorePending{}, false
	}
	item := x.pending[found[0]]
	delete(x.pending, found[0])
	return found[0], item, true
}

func (x *exploreTracker) applyEvaluator(ctx context.Context, document *agentprompt.Document, input agentprompt.Context) error {
	sessionID, item, ok := x.claim(input.Evaluation)
	if !ok {
		return nil
	}
	section := x.render(ctx, sessionID, item, evaluationRound(input.Evaluation.Progress))
	if err := document.After(agentprompt.SectionRequest, exploreSectionID, agentprompt.Static(section)); err != nil {
		return document.Add(exploreSectionID, agentprompt.Static(section))
	}
	return nil
}

func evaluationRound(progress string) int {
	match := progressRoundPattern.FindStringSubmatch(progress)
	if match == nil {
		return 0
	}
	round, _ := strconv.Atoi(match[1])
	return round
}

// 评估要求（中文，追加在上游评估请求之后，不替换上游评估器规则）。
const exploreEvaluatorRules = `## 平台评估要求

- 按上方 Acceptance Criteria（任务完成标准）判定，以平台黑板为准；执行者的自述不能替代黑板记录。
- 未达标且 continue=true 时，feedback 必须点名下一轮要处理的 Oracle 编号（如 O-003）或尚未被 Oracle 覆盖的端点（如 GET https://example.test/api/items/{id}）。
- 连续两轮黑板无新事实、无新结论（见上方“连续无新增轮数”达到 2）时，判 pass=false、continue=false，并在 reason 中说明停止原因。`

// exploreUnresolvedNotice：本 turn 有未决 HTTP 时，平台 unknown 阻断会拒绝后续轮次的模型调用（以 error 结束），
// 因此要求评估器直接停止，循环以本轮结果正常结束，留给人工核对。
const exploreUnresolvedNotice = "注意：本轮存在待核对的 HTTP 请求（claimed/unknown/capture_failed/review_required）。平台会拒绝本 turn 后续的模型调用，下一轮无法推进：必须判 pass=false、continue=false，reason 写明“有发送结果未知或待审批的请求，需人工核对”。"

// render 生成平台黑板段：未决 HTTP 提示 + boardDigest + 计数行 + 与上一轮评估相比的增量；读库失败如实写明。
func (x *exploreTracker) render(ctx context.Context, sessionID string, item explorePending, round int) string {
	board, err := x.boardSection(ctx, sessionID, item.taskID, round)
	if err != nil {
		log.Printf("webapi 持续探索：读取任务 %s 黑板失败：%v", item.taskID, err)
		board = "（本轮读取平台黑板失败，无黑板摘要；请仅依据执行轨迹判断，不要据此判定通过。）"
	}
	if blocked, err := x.store.unresolvedTurn(ctx, sessionID, item.turnID); err != nil || blocked {
		if err != nil {
			log.Printf("webapi 持续探索：核对会话 %s 未决 HTTP 失败：%v", sessionID, err)
		}
		board = exploreUnresolvedNotice + "\n\n" + board
	}
	return "## 平台黑板\n\n" + board + "\n\n" + exploreEvaluatorRules
}

func (x *exploreTracker) boardSection(ctx context.Context, sessionID, taskID string, round int) (string, error) {
	digest, err := x.store.boardDigest(ctx, taskID, exploreDigestChars)
	if err != nil {
		return "", err
	}
	counts, err := x.store.boardCounts(ctx, taskID)
	if err != nil {
		return "", err
	}
	var facts, conclusions int64
	if err := x.store.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webapi_board_facts WHERE task_id = ?`, taskID).Scan(&facts)
	}); err != nil {
		return "", err
	}
	if err := x.store.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webapi_board_oracle_events WHERE task_id = ? AND to_status IN (?, ?, ?)`,
			taskID, oracleHolds, oracleRefuted, oracleBlocked).Scan(&conclusions)
	}); err != nil {
		return "", err
	}
	summary := fmt.Sprintf("地图端点 %d，未覆盖 %d；Oracle open %d / in_progress %d / holds %d / refuted %d / blocked %d",
		counts.Endpoints, counts.UntestedEndpoints, counts.Oracles[oracleOpen], counts.Oracles[oracleInProgress],
		counts.Oracles[oracleHolds], counts.Oracles[oracleRefuted], counts.Oracles[oracleBlocked])

	now := x.now()
	x.mu.Lock()
	previous, seen := x.progress[sessionID]
	current := exploreProgress{facts: facts, conclusions: conclusions, at: now}
	var delta string
	if round <= 1 || !seen {
		delta = fmt.Sprintf("首轮评估：黑板现有事实 %d 条、已结论 Oracle %d 次；连续无新增轮数 0。", facts, conclusions)
	} else {
		newFacts, newConclusions := max(facts-previous.facts, 0), max(conclusions-previous.conclusions, 0)
		if newFacts == 0 && newConclusions == 0 {
			current.idle = previous.idle + 1
		}
		delta = fmt.Sprintf("相比上一轮评估：新增事实 %d 条、新结论 %d 个；连续无新增轮数 %d。", newFacts, newConclusions, current.idle)
	}
	x.progress[sessionID] = current
	x.mu.Unlock()
	return digest + "\n\n" + summary + "\n" + delta, nil
}
