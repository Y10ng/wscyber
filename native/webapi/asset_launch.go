package webapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"

	aop "github.com/chainreactors/cyber/aop"
	types "github.com/chainreactors/cyber/core/types"
	managementapi "github.com/chainreactors/cyber/pkg/web/api"
)

// 与前端 lib/session-agent.ts 相同的节点能力标志：只选明确提供 webapi_http 的节点；
// 本机受控模型节点 Run 预算固定 3（同前端 handleSendMessage）。
const (
	webapiHTTPCapability = "wscyber.webapi-http.v1"
	localModelCapability = "wscyber.local-model.v1"
	localModelMaxTurns   = 3
)

// sessionLauncher 是从资产发起任务所需的原生会话能力；生产实现包装管理 API
// （Routes 时注入），测试可替换。
type sessionLauncher interface {
	Agents() []*types.AgentView
	// Session 读已存在会话；不存在返回 (nil, nil)。
	Session(ctx context.Context, sessionID string) (*aop.Session, error)
	OpenSession(ctx context.Context, requestID string, request *aop.OpenSessionRequest) (*aop.OpenSessionResponse, error)
	RunTurn(ctx context.Context, requestID string, request *aop.RunTurnRequest) (*aop.RunTurnResponse, error)
	// UserTurn 判断会话里是否已有用户首条消息（Run 已被接受）。
	UserTurn(ctx context.Context, sessionID string) (turnID string, found bool, err error)
	// SessionRunning 判断会话是否仍有进行中的轮次（周期调度“上一次仍在运行”与并发计数用）。
	SessionRunning(ctx context.Context, sessionID string) (bool, error)
	// TurnEnded 判断会话事件里是否至少有一个 TurnEnded（计划后续“研究已结束”判定用）。
	TurnEnded(ctx context.Context, sessionID string) (bool, error)
}

type apiLauncher struct{ api *managementapi.API }

func (l apiLauncher) Agents() []*types.AgentView {
	return l.api.ListAgents(&types.ListAgentsRequest{}).GetAgents()
}

func (l apiLauncher) Session(ctx context.Context, sessionID string) (*aop.Session, error) {
	if l.api.Sessions == nil {
		return nil, managementapi.Errorf(managementapi.CodeFailedPrecondition, "会话服务不可用")
	}
	response, err := l.api.Sessions.GetSession(ctx, &types.GetSessionRequest{SessionId: sessionID})
	if managementapi.ErrorCode(err) == managementapi.CodeNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return response.GetSession().GetSession(), nil
}

func (l apiLauncher) OpenSession(ctx context.Context, requestID string, request *aop.OpenSessionRequest) (*aop.OpenSessionResponse, error) {
	if l.api.Sessions == nil {
		return nil, managementapi.Errorf(managementapi.CodeFailedPrecondition, "会话服务不可用")
	}
	return l.api.Sessions.OpenSession(ctx, requestID, request)
}

func (l apiLauncher) RunTurn(ctx context.Context, requestID string, request *aop.RunTurnRequest) (*aop.RunTurnResponse, error) {
	if l.api.Sessions == nil {
		return nil, managementapi.Errorf(managementapi.CodeFailedPrecondition, "会话服务不可用")
	}
	return l.api.Sessions.RunTurn(ctx, requestID, request)
}

// 首条用户消息在 Run 接受时先于 Agent 执行持久化，位于会话最前部。
const userTurnScanLimit = 500

func (l apiLauncher) UserTurn(ctx context.Context, sessionID string) (string, bool, error) {
	if l.api.Sessions == nil {
		return "", false, managementapi.Errorf(managementapi.CodeFailedPrecondition, "会话服务不可用")
	}
	response, err := l.api.Sessions.ListEvents(ctx, &aop.ListEventsRequest{SessionId: sessionID, Limit: userTurnScanLimit})
	if err != nil {
		return "", false, err
	}
	for _, delivery := range response.GetEvents() {
		event := delivery.GetEvent()
		if message := event.GetMessage(); message != nil && message.GetRole() == "user" {
			return event.GetTurnId(), true, nil
		}
	}
	return "", false, nil
}

// sessionRunningPage 是扫描会话事件判定进行中轮次的分页大小。
const sessionRunningPage = 1000

// SessionRunning 依据原生会话状态判定“仍在运行”，三者同时成立才算：
//  1. 会话存在；
//  2. 会话绑定节点当前在线且 AgentView.Busy（原生 Agent 池中该连接有已派发未结束的轮次）；
//  3. 持久化事件里存在已开始（用户消息或 TurnStarted）但没有 TurnEnded 的轮次。
//
// 原生 RunTurn 先持久化用户消息再派发，轮次结束（含断连/派发失败/取消）由运行时写 TurnEnded。
// Hub 重启后旧轮次可能缺 TurnEnded，但重连后的节点连接不再 Busy，故条件 2 避免永久误判为运行中。
func (l apiLauncher) SessionRunning(ctx context.Context, sessionID string) (bool, error) {
	session, err := l.Session(ctx, sessionID)
	if err != nil || session == nil {
		return false, err
	}
	node := findAgent(l.Agents(), session.GetNodeId())
	if node == nil || !node.GetBusy() {
		return false, nil
	}
	open := map[string]bool{}
	err = l.scanEvents(ctx, sessionID, func(event *aop.Event) bool {
		turnID := event.GetTurnId()
		if turnID == "" {
			return true
		}
		switch {
		case event.GetTurnEnded() != nil:
			delete(open, turnID)
		case event.GetTurnStarted() != nil:
			open[turnID] = true
		case event.GetMessage() != nil && event.GetMessage().GetRole() == "user":
			open[turnID] = true
		}
		return true
	})
	return len(open) > 0, err
}

// TurnEnded 判断会话持久化事件里是否至少有一个 TurnEnded（计划后续“研究已结束”判定用）。
func (l apiLauncher) TurnEnded(ctx context.Context, sessionID string) (bool, error) {
	session, err := l.Session(ctx, sessionID)
	if err != nil || session == nil {
		return false, err
	}
	ended := false
	err = l.scanEvents(ctx, sessionID, func(event *aop.Event) bool {
		ended = event.GetTurnEnded() != nil
		return !ended
	})
	return ended, err
}

// scanEvents 按页顺序读取会话持久化事件，visit 返回 false 时提前结束。
func (l apiLauncher) scanEvents(ctx context.Context, sessionID string, visit func(*aop.Event) bool) error {
	cursor := ""
	for {
		response, err := l.api.Sessions.ListEvents(ctx, &aop.ListEventsRequest{SessionId: sessionID, AfterCursor: cursor, Limit: sessionRunningPage})
		if err != nil {
			return err
		}
		for _, delivery := range response.GetEvents() {
			if !visit(delivery.GetEvent()) {
				return nil
			}
		}
		if len(response.GetEvents()) < sessionRunningPage || response.GetNextCursor() == "" || response.GetNextCursor() == cursor {
			return nil
		}
		cursor = response.GetNextCursor()
	}
}

// AssetLaunchOptions 覆盖资产默认值；空串＝用资产的 objective/scope/approval。
type AssetLaunchOptions struct {
	Objective string `json:"objective"`
	Scope     string `json:"scope"`
	Approval  string `json:"approval"`
}

// AssetLaunchProgress 如实记录已完成到哪一步。
type AssetLaunchProgress struct {
	TaskCreated   bool `json:"task_created"`
	SessionOpened bool `json:"session_opened"`
	TurnStarted   bool `json:"turn_started"`
}

// AssetLaunchResult 是发起结果（成功或部分完成）。
type AssetLaunchResult struct {
	AssetID   string              `json:"asset_id"`
	Task      *Task               `json:"task"`
	SessionID string              `json:"session_id"`
	NodeID    string              `json:"node_id"`
	TurnID    string              `json:"turn_id"`
	Progress  AssetLaunchProgress `json:"progress"`
	// Created=本次新建任务；Idempotent=本次调用前已全部完成，未做任何动作。
	Created    bool `json:"created"`
	Idempotent bool `json:"idempotent"`
}

// AssetLaunchError 带 HTTP 状态、失败阶段与已完成进度；重试同一 task_id 续完。
type AssetLaunchError struct {
	Status int
	Stage  string // validate | task | first_message | select_node | open_session | run_turn
	Msg    string
	Result *AssetLaunchResult
}

func (e *AssetLaunchError) Error() string { return e.Msg }

func launchFail(status int, stage, message string, result *AssetLaunchResult) *AssetLaunchError {
	return &AssetLaunchError{Status: status, Stage: stage, Msg: message, Result: result}
}

func randomRequestID(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// taskLaunchLock 按 task_id 串行化同进程内并发发起，避免重复 Open/Run。
func (c *Capability) taskLaunchLock(taskID string) func() {
	value, _ := c.launchLocks.LoadOrStore(taskID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// LaunchAssetTask 从资产一次完成：建任务（含策略与资产关联）→ Open → Run（首条消息附
// 测试账号与禁止行为块）。按 task_id 幂等：已完成的步骤不重复；失败返回 *AssetLaunchError
// 说明阶段与进度，同 task_id 重试续完。供 HTTP 与周期调度共用。
func (c *Capability) LaunchAssetTask(ctx context.Context, assetID, taskID string, opts AssetLaunchOptions) (*AssetLaunchResult, error) {
	if err := validPathID(assetID); err != nil {
		return nil, launchFail(http.StatusBadRequest, "validate", "资产 id 非法", nil)
	}
	if err := validPathID(taskID); err != nil {
		return nil, launchFail(http.StatusBadRequest, "validate", "任务 id 非法", nil)
	}
	opts.Scope, opts.Approval = strings.TrimSpace(opts.Scope), strings.TrimSpace(opts.Approval)
	if opts.Scope != "" && opts.Scope != ScopeOrigins && opts.Scope != ScopeUnrestricted {
		return nil, launchFail(http.StatusBadRequest, "validate", "scope 只能是 origins 或 unrestricted", nil)
	}
	if opts.Approval != "" && opts.Approval != ApprovalOff && opts.Approval != ApprovalHighRisk && opts.Approval != ApprovalAll {
		return nil, launchFail(http.StatusBadRequest, "validate", "approval 只能是 off、high_risk 或 all", nil)
	}
	unlock := c.taskLaunchLock(taskID)
	defer unlock()

	asset, err := c.store.assetModel(ctx, assetID)
	if err != nil {
		return nil, launchFail(http.StatusInternalServerError, "task", "读取资产失败", nil)
	}
	if asset == nil {
		return nil, launchFail(http.StatusNotFound, "validate", "资产不存在", nil)
	}
	result := &AssetLaunchResult{AssetID: assetID}

	// 1. 任务 + 策略 + 关联（单事务）。已存在且属于本资产则原样沿用，不按当前资产重算。
	existing, err := c.store.Get(ctx, taskID)
	if err != nil {
		return nil, launchFail(http.StatusInternalServerError, "task", "读取任务失败", nil)
	}
	knownInfo := composeAssetKnownInfo(asset)
	objective := opts.Objective
	if strings.TrimSpace(objective) == "" {
		objective = asset.Objective
	}
	scope, approval := asset.Scope, asset.Approval
	if opts.Scope != "" {
		scope = opts.Scope
	}
	if opts.Approval != "" {
		approval = opts.Approval
	}
	origins, err := decodeOrigins(asset.OriginsJSON)
	if err != nil {
		return nil, launchFail(http.StatusInternalServerError, "task", "资产 origins 损坏", nil)
	}
	if existing == nil {
		if strings.TrimSpace(knownInfo) == "" {
			return nil, launchFail(http.StatusBadRequest, "validate", "资产已知信息、环境、负责人均为空，无法组成任务已知信息", nil)
		}
		if strings.TrimSpace(objective) == "" {
			return nil, launchFail(http.StatusBadRequest, "validate", "资产未设默认目标，请求也未提供 objective", nil)
		}
		if len(origins) == 0 {
			return nil, launchFail(http.StatusBadRequest, "validate", "资产没有授权 origin", nil)
		}
		if err := validPolicy(scope, approval); err != nil {
			return nil, launchFail(http.StatusBadRequest, "validate", err.Error(), nil)
		}
	}
	task, created, err := c.store.createAssetTask(ctx, assetID, taskID, knownInfo, objective, origins, scope, approval)
	if errors.Is(err, errAssetTaskOther) {
		return nil, launchFail(http.StatusConflict, "task", errAssetTaskOther.Error(), nil)
	}
	if err != nil {
		return nil, launchFail(http.StatusInternalServerError, "task", "保存任务失败", nil)
	}
	result.Created = created
	result.Progress.TaskCreated = true
	result.SessionID = task.InitialSessionID
	if decorated, err := c.store.withPolicy(ctx, task); err == nil {
		result.Task = decorated
	} else {
		result.Task = task
	}

	// 2–4. 会话：已存在沿用原绑定节点；首条消息（解密测试账号）在 Open 前组装，密钥问题不留下空会话。
	started, err := c.startSession(ctx, task.InitialSessionID, "任务", func() (string, error) { return c.assetFirstMessage(ctx, asset, task) }, result)
	if err != nil {
		return result, err
	}
	result.Idempotent = started && !created
	return result, nil
}

// startSession 是服务端启动原生会话的公共步骤（资产发起任务与计划后续共用），按 sessionID 幂等：
// 会话已存在则沿用原绑定节点、不再 Open；会话里已有用户消息即视为已启动，不再 Run（返回 started=true）。
// 否则组装首条消息 → 选节点（在线且提供 webapi_http，按 node_id 取第一个；本机模型须已配置）→ Open → Run。
// subject 是失败文案里“已保存”的对象（任务/验证/复测）；失败返回 *AssetLaunchError（阶段与进度写入 result）。
func (c *Capability) startSession(ctx context.Context, sessionID, subject string, buildMessage func() (string, error), result *AssetLaunchResult) (bool, error) {
	if c.launcher == nil {
		return false, launchFail(http.StatusServiceUnavailable, "open_session", "会话服务不可用，"+subject+"已保存但未启动；可重试", result)
	}
	session, err := c.launcher.Session(ctx, sessionID)
	if err != nil {
		return false, launchFail(http.StatusInternalServerError, "open_session", "暂不能核对原生会话状态，"+subject+"已保存；可重试（不会重复 Open）", result)
	}
	agents := c.launcher.Agents()
	if session != nil {
		result.Progress.SessionOpened = true
		result.NodeID = session.GetNodeId()
		turnID, found, err := c.launcher.UserTurn(ctx, sessionID)
		if err != nil {
			return false, launchFail(http.StatusInternalServerError, "run_turn", "暂不能核对首条消息是否已发送，"+subject+"与会话已就绪；可重试（不会盲发新 Run）", result)
		}
		if found {
			result.Progress.TurnStarted = true
			result.TurnID = turnID
			return true, nil
		}
	}

	message, err := buildMessage()
	if err != nil {
		return false, launchFail(http.StatusInternalServerError, "first_message", err.Error(), result)
	}

	var node *types.AgentView
	if session != nil {
		node = findAgent(agents, session.GetNodeId())
		if node == nil {
			return false, launchFail(http.StatusConflict, "run_turn", "会话绑定的节点 "+session.GetNodeId()+" 当前不在线，"+subject+"与会话已保存但未启动；节点上线后重试", result)
		}
	} else {
		node = pickWebAPINode(agents)
		if node == nil {
			return false, launchFail(http.StatusConflict, "select_node", "当前没有已提供 webapi_http 工具的在线节点，"+subject+"已保存但未启动；请先启动或连接模型节点后重试", result)
		}
	}
	if hasCapability(node, localModelCapability) && (strings.TrimSpace(node.GetStatus().GetProvider()) == "" || strings.TrimSpace(node.GetStatus().GetModel()) == "") {
		return false, launchFail(http.StatusConflict, "select_node", "本机模型节点尚未配置模型，"+subject+"已保存但未启动；请在设置中配置模型后重试", result)
	}
	result.NodeID = node.GetHello().GetNodeId()
	if session == nil {
		response, err := c.launcher.OpenSession(ctx, randomRequestID("wa-open-"), &aop.OpenSessionRequest{SessionId: sessionID, NodeId: result.NodeID})
		if err != nil {
			return false, launchFail(http.StatusBadGateway, "open_session", "创建原生会话失败："+err.Error(), result)
		}
		if rejected := response.GetRejected(); rejected != nil || response.GetAccepted() == nil {
			return false, rejectionFail("open_session", "创建原生会话被拒绝", subject, rejected, result)
		}
		result.Progress.SessionOpened = true
	}

	maxTurns := uint32(0)
	if hasCapability(node, localModelCapability) {
		maxTurns = localModelMaxTurns
	}
	turnID := randomRequestID("")
	run, err := c.launcher.RunTurn(ctx, randomRequestID("wa-run-"), &aop.RunTurnRequest{
		SessionId: sessionID, TurnId: turnID, MaxTurns: maxTurns,
		Input: &aop.Message{Id: randomRequestID(""), Role: "user", Content: []*aop.Content{{Value: &aop.Content_Text{Text: &aop.TextContent{Text: message}}}}},
	})
	if err != nil {
		return false, launchFail(http.StatusBadGateway, "run_turn", "发送首条消息失败："+err.Error(), result)
	}
	if rejected := run.GetRejected(); rejected != nil || run.GetAccepted() == nil {
		return false, rejectionFail("run_turn", "首条消息被拒绝", subject, rejected, result)
	}
	result.Progress.TurnStarted = true
	result.TurnID = run.GetAccepted().GetTurnId()
	return false, nil
}

func rejectionFail(stage, prefix, subject string, rejected *aop.Rejection, result *AssetLaunchResult) *AssetLaunchError {
	code, detail := rejected.GetCode(), rejected.GetMessage()
	status := http.StatusBadGateway
	message := fmt.Sprintf("%s（%s：%s），%s已保存；可重试", prefix, code, detail, subject)
	if code == string(managementapi.CodeUnavailable) {
		status = http.StatusConflict
		message = prefix + "：节点不在线，" + subject + "已保存但未启动；节点上线后重试"
	}
	return launchFail(status, stage, message, result)
}

func hasCapability(agent *types.AgentView, capability string) bool {
	for _, value := range agent.GetHello().GetCapabilities() {
		if value == capability {
			return true
		}
	}
	return false
}

func findAgent(agents []*types.AgentView, nodeID string) *types.AgentView {
	for _, agent := range agents {
		if agent.GetHello().GetNodeId() == nodeID && nodeID != "" {
			return agent
		}
	}
	return nil
}

// pickWebAPINode 同前端 findWebApiHttpNode：在线且提供 webapi_http；服务端无“当前选中”，
// 按 node_id 排序取第一个保证确定性。
func pickWebAPINode(agents []*types.AgentView) *types.AgentView {
	capable := []*types.AgentView{}
	for _, agent := range agents {
		if agent.GetHello().GetNodeId() != "" && hasCapability(agent, webapiHTTPCapability) {
			capable = append(capable, agent)
		}
	}
	sort.Slice(capable, func(i, j int) bool { return capable[i].GetHello().GetNodeId() < capable[j].GetHello().GetNodeId() })
	if len(capable) == 0 {
		return nil
	}
	return capable[0]
}

// webAPIFirstMessage 与前端 useChatSession.ts webApiFirstMessage 逐行一致。
func webAPIFirstMessage(task *Task) []string {
	lines := []string{
		"开始探索任务。",
		"",
		"目标／已知信息：" + task.KnownInfo,
		"希望达到的目标：" + task.Objective,
		"",
		"HTTP(S) 授权范围（仅限以下 origin，出站请求不得超出）：",
	}
	for _, origin := range task.Origins {
		lines = append(lines, "- "+origin)
	}
	return append(lines, "", "提示：需要发送 HTTP 请求时，优先使用 webapi_http 工具以留存请求/响应证据（此提示不构成额外授权）。")
}

// assetFirstMessage = 前端首条消息 + 测试账号块（发起时解密，只进首条消息）+ 禁止行为块。
func (c *Capability) assetFirstMessage(ctx context.Context, asset *assetModel, task *Task) (string, error) {
	lines := webAPIFirstMessage(task)
	credentials, err := c.store.assetCredentials(ctx, asset.ID)
	if err != nil {
		return "", errors.New("读取测试账号失败")
	}
	if items := credentials[asset.ID]; len(items) > 0 {
		lines = append(lines, "", "测试账号（资产登记，仅用于上述授权范围内的测试）：")
		for _, item := range items {
			fields := []string{}
			if item.Label != "" {
				fields = append(fields, "名称："+item.Label)
			}
			if item.Username != "" {
				fields = append(fields, "用户名："+item.Username)
			}
			if item.SecretCipher != "" {
				aead, err := c.secrets.load(false)
				if err != nil {
					return "", err
				}
				secret, err := openSecret(aead, asset.ID, item.ID, item.SecretCipher)
				if err != nil {
					return "", err
				}
				fields = append(fields, "密码："+secret)
			}
			if len(fields) > 0 {
				lines = append(lines, "- "+strings.Join(fields, "；"))
			}
		}
	}
	if forbidden := strings.TrimSpace(asset.Forbidden); forbidden != "" {
		lines = append(lines, "", "禁止行为（提示词约束，非拦截）：", forbidden)
	}
	return strings.Join(lines, "\n"), nil
}
