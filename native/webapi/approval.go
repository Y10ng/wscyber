package webapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	operationpb "github.com/chainreactors/cyber/aop/operation"
	"github.com/chainreactors/cyber/core/events"
	"github.com/chainreactors/cyber/core/extension"
	corehooks "github.com/chainreactors/cyber/core/hooks"
	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
	toolhooks "github.com/chainreactors/cyber/core/tool/hooks"
	"github.com/chainreactors/cyber/exts/guardrail"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ReviewTimeout 是危险操作等待人工审批的上限；超时＝拒绝。
const ReviewTimeout = 10 * time.Minute

// 被拒/超时/取消的 webapi_http 记为未发送时的状态。
const (
	requestReviewDenied   = "review_denied"
	requestReviewExpired  = "review_expired"
	requestReviewCanceled = "review_canceled"
)

// readOnlyMethods 在 approval=all 时免审批（仍受高危信号约束）。
var readOnlyMethods = map[string]bool{"GET": true, "HEAD": true, "OPTIONS": true}

// httpReviewReason 是 webapi_http 的审批判定；空串表示无需审批。
func httpReviewReason(approval, method, rawURL string, headers map[string]string, body []byte) string {
	if approval == ApprovalOff {
		return ""
	}
	if signal := requestReview(method, rawURL, headers, body); signal != "" {
		return requestPolicyVersion + "：" + signal
	}
	if approval == ApprovalAll && !readOnlyMethods[strings.ToUpper(method)] {
		return "全部人工审批：非只读方法 " + strings.ToUpper(method)
	}
	return ""
}

type pendingReview struct {
	review *guardrail.Review
	ctx    context.Context
	done   chan struct{}
}

// approvalRuntime 持有待审批调用与已批准调用；按 session 精确隔离。
// 只有 Resolve（经原生鉴权的 Hub 控制通道转发）能改变审批状态。
type approvalRuntime struct {
	mu       sync.Mutex
	timeout  time.Duration
	pending  map[string]*pendingReview
	approved map[string]struct{}
}

func newApprovalRuntime() *approvalRuntime {
	return &approvalRuntime{timeout: ReviewTimeout, pending: map[string]*pendingReview{}, approved: map[string]struct{}{}}
}

func callKey(inv operation.Invocation) string {
	return inv.SessionID + "\x00" + inv.TurnID + "\x00" + inv.CallID
}

// isApproved 供工具与命令边界核对：本调用是否经人工批准。
func (r *approvalRuntime) isApproved(inv operation.Invocation) bool {
	if r == nil || inv.SessionID == "" || inv.CallID == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.approved[callKey(inv)]
	return ok
}

func (r *approvalRuntime) forget(inv operation.Invocation) {
	r.mu.Lock()
	delete(r.approved, callKey(inv))
	r.mu.Unlock()
}

// review 发 PENDING 事件并阻塞到批准/拒绝/超时/取消，再发终态事件。
func (r *approvalRuntime) review(ctx, lifetime context.Context, stream *events.Stream, call *aop.ToolCall, ref *operationpb.Ref, reason string) guardrail.ReviewState {
	inv := operation.InvocationFromContext(ctx)
	id := ref.GetOperationId()
	if id == "" || call == nil || inv.SessionID == "" {
		return guardrail.ReviewState_REVIEW_STATE_CANCELED
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(lifetime, cancel)
	defer stop()
	p := &pendingReview{ctx: ctx, done: make(chan struct{}), review: &guardrail.Review{
		Call: guardrail.SanitizeCall(call), Operation: proto.Clone(ref).(*operationpb.Ref), SessionId: inv.SessionID,
		Decision: &guardrail.Decision{Action: guardrail.Action_ACTION_REVIEW, Reason: guardrail.RedactText(reason)},
		State:    guardrail.ReviewState_REVIEW_STATE_PENDING, ExpiresAt: timestamppb.New(time.Now().Add(r.timeout)),
	}}
	r.mu.Lock()
	if ctx.Err() != nil || r.pending[id] != nil {
		r.mu.Unlock()
		return guardrail.ReviewState_REVIEW_STATE_CANCELED
	}
	r.pending[id] = p
	snapshot := proto.Clone(p.review).(*guardrail.Review)
	r.mu.Unlock()
	emitReview(stream, inv, snapshot)
	timer := time.NewTimer(time.Until(p.review.ExpiresAt.AsTime()))
	defer timer.Stop()
	select {
	case <-p.done:
	case <-ctx.Done():
		r.finish(p, guardrail.ReviewState_REVIEW_STATE_CANCELED)
	case <-timer.C:
		r.finish(p, guardrail.ReviewState_REVIEW_STATE_EXPIRED)
	}
	r.mu.Lock()
	state := p.review.State
	if state == guardrail.ReviewState_REVIEW_STATE_APPROVED && ctx.Err() != nil {
		state = guardrail.ReviewState_REVIEW_STATE_CANCELED
		p.review.State = state
	}
	if state == guardrail.ReviewState_REVIEW_STATE_APPROVED {
		r.approved[callKey(inv)] = struct{}{}
	}
	snapshot = proto.Clone(p.review).(*guardrail.Review)
	r.mu.Unlock()
	emitReview(stream, inv, snapshot)
	return state
}

// finishLocked 是唯一状态迁移；解决时同样核对取消与过期，迟到的批准不能放行。
func (r *approvalRuntime) finishLocked(p *pendingReview, state guardrail.ReviewState) bool {
	if p.review.State != guardrail.ReviewState_REVIEW_STATE_PENDING {
		return false
	}
	if p.ctx.Err() != nil {
		state = guardrail.ReviewState_REVIEW_STATE_CANCELED
	} else if !time.Now().Before(p.review.ExpiresAt.AsTime()) {
		state = guardrail.ReviewState_REVIEW_STATE_EXPIRED
	}
	p.review.State = state
	delete(r.pending, p.review.Operation.GetOperationId())
	close(p.done)
	return true
}

func (r *approvalRuntime) finish(p *pendingReview, state guardrail.ReviewState) {
	r.mu.Lock()
	r.finishLocked(p, state)
	r.mu.Unlock()
}

// Pending 列出某 session 当前待审批调用。
func (r *approvalRuntime) Pending(sessionID string) []*guardrail.Review {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*guardrail.Review, 0)
	for _, p := range r.pending {
		if p.review.SessionId == sessionID && p.ctx.Err() == nil && time.Now().Before(p.review.ExpiresAt.AsTime()) {
			out = append(out, proto.Clone(p.review).(*guardrail.Review))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Operation.GetOperationId() < out[j].Operation.GetOperationId() })
	return out
}

// Resolve 只按 session+operation 精确匹配；source 记入终态事件。
func (r *approvalRuntime) Resolve(sessionID, operationID string, approve bool, source string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.pending[operationID]
	if p == nil || p.review.SessionId != sessionID {
		return errors.New("pending review not found in this session")
	}
	state := guardrail.ReviewState_REVIEW_STATE_REJECTED
	if approve {
		state = guardrail.ReviewState_REVIEW_STATE_APPROVED
	}
	r.finishLocked(p, state)
	if p.review.State != state {
		return errors.New("review is no longer pending")
	}
	p.review.ResolutionSource = source
	return nil
}

func emitReview(stream *events.Stream, inv operation.Invocation, review *guardrail.Review) {
	if stream == nil {
		return
	}
	payload, err := anypb.New(review)
	if err != nil {
		return
	}
	event := &aop.Event{SessionId: inv.SessionID, TurnId: inv.TurnID, Emitter: "guardrail", Payload: &aop.Event_Extension{Extension: payload}}
	if review.Operation != nil {
		if ref, err := anypb.New(review.Operation); err == nil {
			event.Extensions = append(event.Extensions, ref)
		}
	}
	stream.Publish(event)
}

func reviewDenied(state guardrail.ReviewState, reason string) toolhooks.Admission {
	return toolhooks.Admission{Deny: fmt.Errorf("%w: 人工审批结果 %s，未执行。%s", operation.ErrDenied, reviewStateText(state), reason)}
}

func reviewStateText(state guardrail.ReviewState) string {
	switch state {
	case guardrail.ReviewState_REVIEW_STATE_REJECTED:
		return "已拒绝"
	case guardrail.ReviewState_REVIEW_STATE_EXPIRED:
		return "超时（视为拒绝）"
	default:
		return "已取消"
	}
}

func unsentStatus(state guardrail.ReviewState) string {
	switch state {
	case guardrail.ReviewState_REVIEW_STATE_REJECTED:
		return requestReviewDenied
	case guardrail.ReviewState_REVIEW_STATE_EXPIRED:
		return requestReviewExpired
	default:
		return requestReviewCanceled
	}
}

// ApprovalExtension 在原生 tool.before / command.before 上按会话所属任务的测试策略
// 判定是否需人工审批；需审批时以 guardrail.Review 事件挂起，等人经 Hub 批准/拒绝。
// 只读工具与平台结果工具不审批。审批在工具执行前完成，不计入终端命令超时。
func (c *Capability) ApprovalExtension() extension.Extension {
	var subscriptions []*corehooks.Subscription
	return extension.Func{
		LoadFunc: func(scope *extension.Scope) error {
			registry, err := extension.Use[*corehooks.Registry](scope)
			if err != nil {
				return err
			}
			stream, err := extension.Use[*events.Stream](scope)
			if err != nil {
				return err
			}
			lifetime := scope.Lifetime()
			subscriptions = append(subscriptions,
				toolhooks.Before.On(registry, "wscyber-webapi-approval", func(ctx context.Context, event toolhooks.CallEvent) (toolhooks.Admission, error) {
					return c.admitTool(ctx, lifetime, stream, event)
				}),
				toolhooks.BeforeCommand.On(registry, "wscyber-webapi-command-approval", c.admitCommand),
				toolhooks.Completed.On(registry, "wscyber-webapi-approval-release", func(ctx context.Context, _ toolhooks.Completion) (struct{}, error) {
					c.approvals.forget(operation.InvocationFromContext(ctx))
					return struct{}{}, nil
				}),
			)
			return nil
		},
		CloseFunc: func(ctx context.Context) error {
			var result error
			for _, subscription := range subscriptions {
				result = errors.Join(result, subscription.Close(ctx))
			}
			return result
		},
	}
}

// admitTool 判定 webapi_http / bash / write 是否需人工审批并在需要时挂起等待；
// webapi_http 被拒/超时/取消时按现有 recordUnsent 记为未发送。
func (c *Capability) admitTool(ctx, lifetime context.Context, stream *events.Stream, event toolhooks.CallEvent) (toolhooks.Admission, error) {
	inv := operation.InvocationFromContext(ctx)
	name := event.Call.GetName()
	if name != "webapi_http" && name != "bash" && name != "write" {
		return toolhooks.Admission{}, nil
	}
	policy, task, err := c.store.policyForSession(ctx, inv.SessionID)
	if err != nil {
		return toolhooks.Admission{Deny: errors.New("无法读取任务测试策略，未执行")}, nil
	}
	if policy.Approval == ApprovalOff {
		return toolhooks.Admission{}, nil
	}
	arguments := string(event.Call.GetArguments().GetData())
	reason := ""
	var pendingHTTP *httpPlan
	switch name {
	case "webapi_http":
		plan := planHTTP(ctx, c.store, inv, arguments)
		if plan == nil || plan.task == nil || plan.outOfScope {
			return toolhooks.Admission{}, nil // 工具自身按现有规则拒绝并如实记录。
		}
		reason = httpReviewReason(policy.Approval, plan.req.Method, plan.req.URL.String(), plan.headers, plan.body)
		pendingHTTP = plan
	case "bash":
		args, parseErr := coretool.ParseArgs[struct {
			Command string `json:"command"`
		}](arguments)
		if parseErr != nil {
			return toolhooks.Admission{}, nil // 原生工具自身报参数错误。
		}
		if policy.Approval == ApprovalAll {
			reason = "全部人工审批：终端命令"
		} else if rule := commandReviewReason(args.Command); rule != "" {
			reason = "高危终端命令：" + rule
		}
	case "write":
		if policy.Approval == ApprovalAll {
			reason = "全部人工审批：文件写入"
		}
	}
	if reason == "" {
		return toolhooks.Admission{}, nil
	}
	if task != nil {
		reason += fmt.Sprintf("（任务 %s 测试策略 approval=%s）", task.ID, policy.Approval)
	}
	state := c.approvals.review(ctx, lifetime, stream, event.Call, event.Operation, reason)
	if state == guardrail.ReviewState_REVIEW_STATE_APPROVED {
		return toolhooks.Admission{}, nil
	}
	if pendingHTTP != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		result, _ := (&httpTool{store: c.store}).recordUnsent(cleanup, inv, pendingHTTP.task.ID, pendingHTTP.req, pendingHTTP.body, unsentStatus(state), "人工审批"+reviewStateText(state)+"："+reason)
		return reviewDenied(state, coretool.ResultText(result)), nil
	}
	return reviewDenied(state, reason), nil
}

// admitCommand 是 shell 展开后 command.before 的兜底：命中高危规则但本调用未经人工批准时直接拒绝，
// 不在此等待（此处已在终端超时计时内）。本固定上游版本只有注册命令（如 tmux）经过 command.before，
// 外部程序不经过，所以动态展开由执行前的静态判定（commandReviewReason）保守要求审批。
func (c *Capability) admitCommand(ctx context.Context, event toolhooks.CommandEvent) (toolhooks.Admission, error) {
	inv := operation.InvocationFromContext(ctx)
	policy, _, err := c.store.policyForSession(ctx, inv.SessionID)
	if err != nil {
		return toolhooks.Admission{Deny: errors.New("无法读取任务测试策略，未执行")}, nil
	}
	if policy.Approval == ApprovalOff || c.approvals.isApproved(inv) {
		return toolhooks.Admission{}, nil
	}
	words := append([]string{event.Name}, event.Args...)
	if rule := invocationReason(words, 0); rule != "" {
		return toolhooks.Admission{Deny: fmt.Errorf("高危终端命令（%s）未经人工审批，未执行；请把命令写成字面形式以便发起审批", rule)}, nil
	}
	return toolhooks.Admission{}, nil
}

// ApprovalBinding 是本节点 guardrail 命名空间（Pending/Resolve）的处理；
// Hub 经上游 forwardGuardrail 转发。不绑定上游 guardrail.NewProtocol。
func (c *Capability) ApprovalBinding() aop.Binding {
	return aop.Shared(&guardrail.ProtocolMessage{}, c.handleApprovalProtocol)
}

func (c *Capability) handleApprovalProtocol(ctx context.Context, envelope *aop.Envelope, message proto.Message, send aop.SendFunc) error {
	value, ok := message.(*guardrail.ProtocolMessage)
	if !ok {
		return fmt.Errorf("unexpected guardrail namespace message")
	}
	reply := func(payload proto.Message) error { return send(aop.Reply(envelope.GetId(), payload)) }
	bound := operation.InvocationFromContext(ctx).SessionID
	if pending := value.GetPending(); pending != nil {
		if pending.SessionId == "" || bound != "" && bound != pending.SessionId {
			return reply(aop.NewProtocolError("GUARDRAIL_DENIED", "session scope mismatch"))
		}
		return reply(&guardrail.ProtocolMessage{Message: &guardrail.ProtocolMessage_PendingResult{PendingResult: &guardrail.PendingResponse{Reviews: c.approvals.Pending(pending.SessionId)}}})
	}
	if resolve := value.GetResolve(); resolve != nil {
		if resolve.OperationId == "" || resolve.SessionId == "" {
			return reply(aop.NewProtocolError("INVALID_ARGUMENT", "session_id and operation_id are required"))
		}
		if bound != "" && bound != resolve.SessionId {
			return reply(aop.NewProtocolError("GUARDRAIL_DENIED", "session scope mismatch"))
		}
		if err := c.approvals.Resolve(resolve.SessionId, resolve.OperationId, resolve.Approve, "control"); err != nil {
			return reply(aop.NewProtocolError("GUARDRAIL_DENIED", err.Error()))
		}
		return reply(&guardrail.ProtocolMessage{Message: &guardrail.ProtocolMessage_Resolved{Resolved: &guardrail.ResolveResponse{}}})
	}
	return reply(aop.NewProtocolError("INVALID_ARGUMENT", "pending or resolve request required"))
}

// httpPlan 是 webapi_http 参数在发送前的同一构造结果（工具与审批共用）。
type httpPlan struct {
	task       *Task
	policy     TaskPolicy
	req        *http.Request
	headers    map[string]string
	body       []byte
	outOfScope bool
}

func planHTTP(ctx context.Context, store *taskStore, inv operation.Invocation, arguments string) *httpPlan {
	args, err := coretool.ParseArgs[httpToolArguments](arguments)
	if err != nil {
		return nil
	}
	task, err := store.taskForSession(ctx, inv.SessionID)
	if err != nil || task == nil {
		return nil
	}
	policy, err := store.policyForTask(ctx, task.ID)
	if err != nil {
		return nil
	}
	body := []byte(args.Body)
	req, err := buildHTTPRequest(args.Method, args.URL, args.Headers, body)
	if err != nil {
		return nil
	}
	return &httpPlan{task: task, policy: policy, req: req, headers: args.Headers, body: body,
		outOfScope: policy.Scope != ScopeUnrestricted && !originAllowed(req.URL.String(), task.Origins)}
}
