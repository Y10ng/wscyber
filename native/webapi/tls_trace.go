package webapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// tlsSendTrace 随一次目标发送的请求 ctx 传入 DialTLSContext（transport 拨号上下文保留请求的 values）：
//   - 携带任务/会话与登记函数，补链的每次实际出网登记一条 tls.aia_fetch；
//   - 记录补链阶段，供发送失败时把“补链进行中/未完成被截止”归为 tls_chain_incomplete；
//   - parent 是本次发送的请求上下文（含总超时）：transport 的拨号上下文脱离了请求取消，dialTLSAt 用它接回。
//
// 取不到 trace（测试直接用 verifier、其它调用方）时不登记、不改变握手与分类。
type tlsSendTrace struct {
	taskID, sessionID string
	parent            context.Context
	record            func(entry auditEntry, at time.Time) // nil = 不登记

	mu    sync.Mutex
	phase aiaPhase
}

type aiaPhase int

const (
	aiaIdle       aiaPhase = iota // 未进入补链
	aiaRunning                    // 补链进行中
	aiaFinished                   // 补链走完（成功，或补齐后仍不可信 / 获取失败）
	aiaIncomplete                 // 补链未完成：预算耗尽、单次超时或上层截止
)

type tlsTraceKey struct{}

func withTLSTrace(ctx context.Context, trace *tlsSendTrace) context.Context {
	return context.WithValue(ctx, tlsTraceKey{}, trace)
}

func tlsTraceFrom(ctx context.Context) *tlsSendTrace {
	trace, _ := ctx.Value(tlsTraceKey{}).(*tlsSendTrace)
	return trace
}

func (t *tlsSendTrace) setPhase(phase aiaPhase) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.phase = phase
	t.mu.Unlock()
}

func (t *tlsSendTrace) chainUnfinished() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.phase == aiaRunning || t.phase == aiaIncomplete
}

// classify：发送超时且超时发生时补链在进行中/未完成 → 包成 tlsChainIncompleteError。
// http.Client 截止时只返回自身的超时错误（拿不到 verifier 的错误），靠 trace 阶段关联。
// 上下文取消（context.Canceled，人为停止）保持 context_canceled，不改写。
func (t *tlsSendTrace) classify(err error) error {
	var incomplete *tlsChainIncompleteError
	if err == nil || errors.As(err, &incomplete) || !t.chainUnfinished() {
		return err
	}
	var networkError net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &networkError) && networkError.Timeout() {
		return &tlsChainIncompleteError{err: err}
	}
	return err
}

// tlsChainIncompleteError：AIA 补链未能完成导致的握手失败（httpFailureReason → tls_chain_incomplete）。
type tlsChainIncompleteError struct{ err error }

func (e *tlsChainIncompleteError) Error() string {
	return "tls: certificate chain completion incomplete"
}
func (e *tlsChainIncompleteError) Unwrap() error { return e.err }

// AIA 获取结果固定枚举（审计 detail.result），不输出 err.Error()。
const (
	aiaResultOK             = "ok"
	aiaResultRefusedAddress = "refused_address"
	aiaResultUnsupportedURL = "unsupported_url"
	aiaResultHTTPStatus     = "http_status"
	aiaResultTooLarge       = "too_large"
	aiaResultParseFailed    = "parse_failed"
	aiaResultTimeout        = "timeout"
	aiaResultNetwork        = "network"
)

var aiaResultLabels = map[string]string{
	aiaResultOK: "成功", aiaResultRefusedAddress: "失败：非公网地址被拒", aiaResultUnsupportedURL: "失败：不支持的地址",
	aiaResultHTTPStatus: "失败：非 200 响应", aiaResultTooLarge: "失败：响应过大", aiaResultParseFailed: "失败：证书无法解析",
	aiaResultTimeout: "失败：超时", aiaResultNetwork: "失败：网络错误",
}

// aiaAuditEntry 构造 tls.aia_fetch：只含 host 与固定枚举，不含 URL 路径、证书内容或目标请求内容。
func aiaAuditEntry(trace *tlsSendTrace, aiaHost, targetHost, result string, size, redirects int) auditEntry {
	aiaHost, targetHost = capRunes(oneLine(aiaHost), 260), capRunes(oneLine(targetHost), 260)
	detail := map[string]any{"aia_host": aiaHost, "target_host": targetHost, "result": result, "redirects": redirects}
	if result == aiaResultOK {
		detail["bytes"] = size
	}
	if trace.sessionID != "" {
		detail["session_id"] = trace.sessionID
	}
	shown := aiaHost
	if shown == "" {
		shown = "（无效地址）"
	}
	return auditEntry{Actor: auditActorSystem, Action: "tls.aia_fetch", TargetKind: "host", TargetID: aiaHost, TaskID: trace.taskID,
		Summary: fmt.Sprintf("证书补链：获取 %s 的中间证书（%s）", shown, aiaResultLabels[result]), Detail: detail}
}

// aiaAuditSink 收集一次发送中的补链审计：握手路径只把条目放进内存，发送（Do）返回后由 flush
// 统一写库（at 为实际获取时刻）；flush 之后才到达的条目（拨号 goroutine 收尾）各自异步写入。
// 写库用脱离请求取消的 5s 上下文，失败只记日志，不影响握手与发送结果。
type aiaAuditSink struct {
	store   *taskStore
	taskID  string
	mu      sync.Mutex
	pending []aiaAuditItem
	flushed bool
}

type aiaAuditItem struct {
	entry auditEntry
	at    time.Time
}

func (s *aiaAuditSink) add(entry auditEntry, at time.Time) {
	s.mu.Lock()
	if !s.flushed {
		s.pending = append(s.pending, aiaAuditItem{entry, at})
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	go s.write(context.Background(), []aiaAuditItem{{entry, at}})
}

func (s *aiaAuditSink) flush(ctx context.Context) {
	s.mu.Lock()
	items := s.pending
	s.pending, s.flushed = nil, true
	s.mu.Unlock()
	s.write(ctx, items)
}

func (s *aiaAuditSink) write(ctx context.Context, items []aiaAuditItem) {
	if len(items) == 0 || s.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	assetID := ""
	if s.taskID != "" {
		assetID, _ = s.store.taskAssetID(ctx, s.taskID)
	}
	for _, item := range items {
		item.entry.AssetID = assetID
		if err := s.store.appendAudit(ctx, item.entry, item.at); err != nil {
			log.Printf("webapi 审计：登记 %s 失败：%v", item.entry.Action, err)
		}
	}
}
