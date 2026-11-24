package webapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/chainreactors/cyber/aop/traffic"
	"github.com/chainreactors/cyber/core/operation"
)

// 一键重放（K2 后端）：人在漏洞卡上触发，对最小复现第 N 步证据的请求在任务授权内原样重放一次，
// 新证据写入 webapi_requests（合成 session，source=replay），并与原步骤做确定性对比。
// 重放结果只是平台对比提示：不改漏洞状态、不写 triage、不触发推送。契约见 native/REPLAY.md。

const (
	replaySessionID       = "replay" // 重放记录的合成 session_id（不是模型会话，taskForSession 映射不到）
	evidenceSourceReplay  = "replay"
	replayRateWindow      = 10 * time.Second
	replayCardMax         = 5
	hintStillReproducible = "still_reproducible"
	hintChanged           = "changed"
	hintUnknown           = "unknown"
)

var replayHintLabels = map[string]string{hintStillReproducible: "仍可复现", hintChanged: "有变化", hintUnknown: "无法判定"}

// replayModel 是一次已完成（收到响应）的重放；漏洞键 → 重放的索引与对比结论，证据本体仍在 webapi_requests。
type replayModel struct {
	bun.BaseModel `bun:"table:webapi_replays,alias:webapi_replay"`

	ID                 string `bun:"id,pk"`
	VulnKey            string `bun:"vuln_key,notnull"`
	Step               int64  `bun:"step,notnull"`
	TaskID             string `bun:"task_id,notnull"`
	EvidenceID         string `bun:"evidence_id,notnull"`
	OriginalEvidenceID string `bun:"original_evidence_id,notnull"`
	Method             string `bun:"method,notnull"`
	URL                string `bun:"url,type:text,notnull"`
	StatusCode         int64  `bun:"status_code,notnull"`
	VerdictHint        string `bun:"verdict_hint,notnull"`
	CreatedAt          string `bun:"created_at,notnull"`
}

// ---- 并发与限频（进程内存；Hub 重启清空） ----

type replayRuntime struct {
	mu       sync.Mutex
	inflight map[string]bool      // key\x00step → 进行中
	last     map[string]time.Time // key → 最近一次放行发送的时刻
}

// begin 原子检查并占位：同 {key,step} 进行中 → inProgress；同 key 窗口内已放行过 → retryAfter>0。
// 成功时返回 release；release(false)（最终未发送）撤销本次限频占位。
func (r *replayRuntime) begin(key string, step int, now time.Time) (release func(sent bool), inProgress bool, retryAfter time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inflight == nil {
		r.inflight, r.last = map[string]bool{}, map[string]time.Time{}
	}
	slot := key + "\x00" + strconv.Itoa(step)
	if r.inflight[slot] {
		return nil, true, 0
	}
	for other, at := range r.last {
		if now.Sub(at) >= replayRateWindow {
			delete(r.last, other)
		}
	}
	previous, had := r.last[key]
	if had && now.Sub(previous) < replayRateWindow {
		return nil, false, replayRateWindow - now.Sub(previous)
	}
	r.last[key], r.inflight[slot] = now, true
	return func(sent bool) {
		r.mu.Lock()
		defer r.mu.Unlock()
		delete(r.inflight, slot)
		if !sent && r.last[key].Equal(now) {
			delete(r.last, key)
		}
	}, false, 0
}

// ---- 请求重建 ----

// replayHTTPRequest 由证据 Flow 请求原样重建（不遮盖）：方法、URL、原始头（按原顺序，同名多值保持顺序）、正文；
// 跳过 HTTP/2 伪头与 Content-Length（客户端重算）。Host 必须与 URL authority 一致（URL 是唯一 Host 来源）后跳过；
// 其余头与 webapi_http 同一校验。返回审批判定用的头视图（同名多值以 ", " 合并）。
func replayHTTPRequest(flow *traffic.Flow) (*http.Request, map[string]string, error) {
	request := flow.GetRequest()
	if request == nil {
		return nil, nil, errors.New("原件缺少请求")
	}
	body := request.GetBody()
	req, err := buildHTTPRequest(request.GetMethod(), request.GetUrl(), nil, body)
	if err != nil {
		return nil, nil, err
	}
	review, seen := map[string]string{}, map[string]bool{}
	for _, header := range request.GetHeaders() {
		name, value := header.GetName(), header.GetValue()
		if name == "" || strings.HasPrefix(name, ":") || strings.EqualFold(name, "Content-Length") {
			continue
		}
		if strings.EqualFold(name, "Host") {
			if !strings.EqualFold(strings.TrimSpace(value), req.Host) {
				return nil, nil, fmt.Errorf("原件 Host %q 与 URL authority 不一致", value)
			}
			continue
		}
		if err := checkRequestHeader(name, value); err != nil {
			return nil, nil, err
		}
		canonical := http.CanonicalHeaderKey(name)
		if !seen[canonical] {
			seen[canonical] = true
			req.Header.Del(canonical) // 原件里的头替换 buildHTTPRequest 的默认 User-Agent/Accept-Encoding。
			review[canonical] = value
		} else {
			review[canonical] += ", " + value
		}
		req.Header.Add(canonical, value)
	}
	return req, review, nil
}

// ---- 对比 ----

type ReplayMarks struct {
	Request  []string `json:"request"`
	Response []string `json:"response"`
}

type ReplayComparison struct {
	SameStatus                bool   `json:"same_status"`
	ResponseMarksStillPresent bool   `json:"response_marks_still_present"`
	VerdictHint               string `json:"verdict_hint"`
}

func flowMarks(flow *traffic.Flow, step verificationKeyStep) ReplayMarks {
	request, _ := marksIn(flowRequestLines(flow), step.RequestMarks)
	response, _ := marksIn(flowResponseLines(flow), step.ResponseMarks)
	return ReplayMarks{Request: request, Response: response}
}

// compareReplay 的规则（确定性）：对比基准是原步骤原件中找到的 response marks（即卡片上高亮的片段）。
// 有基准时：全部仍出现在新响应且状态码相同 → still_reproducible，否则 changed；
// 无基准时：状态码相同 → unknown（无法判定），不同 → changed。无基准时 response_marks_still_present=false。
func compareReplay(originalStatus, replayedStatus int64, original, replayed []string) ReplayComparison {
	result := ReplayComparison{SameStatus: originalStatus != 0 && originalStatus == replayedStatus}
	if len(original) > 0 {
		present := map[string]bool{}
		for _, mark := range replayed {
			present[mark] = true
		}
		result.ResponseMarksStillPresent = true
		for _, mark := range original {
			if !present[mark] {
				result.ResponseMarksStillPresent = false
				break
			}
		}
		result.VerdictHint = hintChanged
		if result.ResponseMarksStillPresent && result.SameStatus {
			result.VerdictHint = hintStillReproducible
		}
		return result
	}
	result.VerdictHint = hintChanged
	if result.SameStatus {
		result.VerdictHint = hintUnknown
	}
	return result
}

// ---- 卡片联动 ----

type VulnCardReplay struct {
	ReplayID    string `json:"replay_id"`
	Step        int64  `json:"step"`
	TaskID      string `json:"task_id"`
	EvidenceID  string `json:"evidence_id"`
	At          string `json:"at"`
	StatusCode  int64  `json:"status_code"`
	VerdictHint string `json:"verdict_hint"`
}

// vulnReplays：该键全部已完成重放，新到旧。
func (s *taskStore) vulnReplays(ctx context.Context, key string) ([]replayModel, error) {
	var rows []replayModel
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return db.NewSelect().Model(&rows).Where("vuln_key = ?", key).OrderExpr(`created_at COLLATE "C" DESC, id COLLATE "C" DESC`).Scan(ctx)
	})
	return rows, err
}

func urlPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.EscapedPath() == "" {
		return "/"
	}
	return u.EscapedPath()
}

func replayLifecycleEvent(row replayModel) VulnCardEvent {
	return VulnCardEvent{At: row.CreatedAt, Kind: "replay", Title: "一键重放：" + labelOr(replayHintLabels, row.VerdictHint),
		Detail: capRunes(fmt.Sprintf("%s %s → %d", strings.ToUpper(row.Method), urlPath(row.URL), row.StatusCode), vulnCardDetailRunes), Actor: auditActorUser}
}

// cardReplays 返回卡片 replays[]（最近 replayCardMax 条）与全部重放的生命周期事件。
func (c *Capability) cardReplays(ctx context.Context, key string) ([]VulnCardReplay, []VulnCardEvent, error) {
	rows, err := c.store.vulnReplays(ctx, key)
	if err != nil {
		return nil, nil, err
	}
	recent, events := []VulnCardReplay{}, make([]VulnCardEvent, 0, len(rows))
	for _, row := range rows {
		if len(recent) < replayCardMax {
			recent = append(recent, VulnCardReplay{ReplayID: row.ID, Step: row.Step, TaskID: row.TaskID, EvidenceID: row.EvidenceID, At: row.CreatedAt, StatusCode: row.StatusCode, VerdictHint: row.VerdictHint})
		}
		events = append(events, replayLifecycleEvent(row))
	}
	return recent, events, nil
}

// ---- HTTP ----

func writeReplayError(w http.ResponseWriter, code int, message string, extra map[string]any) {
	body := map[string]any{"error": message}
	for k, v := range extra {
		body[k] = v
	}
	writeJSON(w, code, body)
}

func statusOf(model *requestModel, flow *traffic.Flow) int64 {
	if model != nil && model.StatusCode.Valid {
		return model.StatusCode.Int64
	}
	return int64(flow.GetResponse().GetStatusCode())
}

// handleVulnCardReplay: POST /api/webapi/vulns/{key}/card/replay {"step":N}（1..3，默认 1）。
func (c *Capability) handleVulnCardReplay(w http.ResponseWriter, r *http.Request) {
	key, ok := vulnCardKey(w, r)
	if !ok {
		return
	}
	var input struct {
		Step *int `json:"step"`
	}
	if err := decodeJSONBody(r, &input, true, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	step := 1
	if input.Step != nil {
		// 上界按该条目最小复现的实际步数，由 locateReproStep 判定。
		if *input.Step < 1 {
			writeError(w, http.StatusBadRequest, "step 必须是从 1 开始的整数")
			return
		}
		step = *input.Step
	}
	ctx := r.Context()
	target, code, message := c.locateReproStep(ctx, key, step)
	if target == nil {
		writeError(w, code, message)
		return
	}
	req, reviewHeaders, err := replayHTTPRequest(target.flow)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "证据原件无法原样重放："+err.Error())
		return
	}
	body := target.flow.GetRequest().GetBody()
	// 授权与 webapi_http 一致：任务、归档、策略每次现读。
	task, err := c.store.Get(ctx, target.chosen.model.TaskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务失败")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, "证据所属任务不存在")
		return
	}
	assetID, err := c.store.taskAssetID(ctx, task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取任务资产失败")
		return
	}
	if assetID != "" {
		asset, err := c.store.assetModel(ctx, assetID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取任务资产失败")
			return
		}
		if asset != nil && asset.Archived {
			writeReplayError(w, http.StatusConflict, "资产已归档，不重放", map[string]any{"status": "archived"})
			return
		}
	}
	policy, err := c.store.policyForTask(ctx, task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法读取任务测试策略；未发送")
		return
	}
	replayID := "rp_" + newSessionID() + newSessionID()
	recorder := requestRecorder{store: c.store, taskID: task.ID, trusted: true,
		inv: operation.Invocation{SessionID: replaySessionID, TurnID: replayID, CallID: fmt.Sprintf("step-%d", step)}}
	switch decision, reason := httpPolicyDecision(policy, task.Origins, req, reviewHeaders, body); decision {
	case "denied":
		extra := map[string]any{"status": "denied"}
		model, _, err := recorder.recordUnsent(ctx, req, body, decision, reason+"（一键重放）")
		if model != nil {
			extra["evidence_id"] = model.ID
		} else if err != nil {
			log.Printf("webapi 一键重放：保存未发送记录失败：%v", err)
		}
		writeReplayError(w, http.StatusForbidden, "请求 origin 不在任务授权 origins 内（测试策略 scope=origins），未发送", extra)
		return
	case "review_required":
		writeReplayError(w, http.StatusConflict, "该请求按任务审批策略需人工审批，一键重放不发送："+reason, map[string]any{"status": "review_required", "reason": reason})
		return
	}
	release, inProgress, retryAfter := c.replays.begin(key, step, c.scheduleClock())
	if inProgress {
		writeReplayError(w, http.StatusConflict, "该步骤已有进行中的重放", map[string]any{"status": "replay_in_progress"})
		return
	}
	if retryAfter > 0 {
		seconds := int(math.Ceil(retryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		writeReplayError(w, http.StatusTooManyRequests, fmt.Sprintf("同一漏洞每 %d 秒最多重放一次", int(replayRateWindow.Seconds())), map[string]any{"retry_after": seconds})
		return
	}
	sent := false
	defer func() { release(sent) }()
	// 人工触发后发送与存证不随页面断开取消（directClient 自带超时），保证记录走完状态机。
	res := recorder.send(context.WithoutCancel(ctx), req, body)
	original := flowMarks(target.flow, target.keyStep)
	detail := map[string]any{"vuln_key": key, "step": step, "evidence_id": "", "original_evidence_id": target.keyStep.EvidenceID, "status_code": nil, "verdict_hint": ""}
	if res.model != nil {
		detail["evidence_id"] = res.model.ID
	}
	switch res.state {
	case requestCompleted:
	case sendClaimFailed:
		if errors.Is(res.err, errUnknownReplay) && res.model != nil {
			writeReplayError(w, http.StatusConflict, "相同请求已有未解决的未知结果，不自动重发；请先核对该证据", map[string]any{"status": "unresolved", "evidence_id": res.model.ID})
			return
		}
		writeError(w, http.StatusInternalServerError, "发送前认领失败，未发送")
		return
	case sendExisting:
		writeError(w, http.StatusInternalServerError, "重放记录冲突，未发送")
		return
	default: // unknown / capture_failed / lost：已尝试发送，结果未知，不重试
		sent = true
		detail["outcome"] = res.state
		c.auditReplay(ctx, target, task.ID, req, detail, "结果未知")
		writeReplayError(w, http.StatusBadGateway, res.note, map[string]any{"status": "unknown", "evidence_id": detail["evidence_id"]})
		return
	}
	sent = true
	replayedFlow, err := decodeEvidenceFlow(res.model)
	if err != nil {
		writeReplayError(w, http.StatusInternalServerError, "重放已完成并存证，但新证据原件无法解析", map[string]any{"evidence_id": res.model.ID})
		return
	}
	replayed := flowMarks(replayedFlow, target.keyStep)
	originalStatus, replayedStatus := statusOf(target.model, target.flow), res.model.StatusCode.Int64
	comparison := compareReplay(originalStatus, replayedStatus, original.Response, replayed.Response)
	row := replayModel{ID: replayID, VulnKey: key, Step: int64(step), TaskID: task.ID, EvidenceID: res.model.ID, OriginalEvidenceID: target.keyStep.EvidenceID,
		Method: req.Method, URL: stripURLUserinfo(req.URL.String()), StatusCode: replayedStatus, VerdictHint: comparison.VerdictHint, CreatedAt: c.scheduleClock().UTC().Format(time.RFC3339Nano)}
	detail["status_code"], detail["verdict_hint"] = replayedStatus, comparison.VerdictHint
	c.auditReplay(ctx, target, task.ID, req, detail, fmt.Sprintf("%d（%s）", replayedStatus, replayHintLabels[comparison.VerdictHint]))
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := c.store.write(saveCtx, nil, func(ctx context.Context, tx bun.Tx) error { _, err := tx.NewInsert().Model(&row).Exec(ctx); return err }); err != nil {
		writeReplayError(w, http.StatusInternalServerError, "重放已完成并存证，但重放记录保存失败", map[string]any{"evidence_id": res.model.ID})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"replay_id": replayID, "step": step, "evidence_id": res.model.ID, "task_id": task.ID, "at": row.CreatedAt,
		"request": map[string]any{"method": req.Method, "url": row.URL},
		"original": map[string]any{"evidence_id": target.keyStep.EvidenceID, "status_code": originalStatus,
			"body_bytes": len(target.flow.GetResponse().GetBody()), "marks_found": original},
		"replayed": map[string]any{"status_code": replayedStatus, "body_bytes": len(replayedFlow.GetResponse().GetBody()),
			"capture_truncated": res.model.CaptureTruncated, "marks_found": replayed},
		"comparison": comparison,
		"note":       "重放结果是平台对比提示，不是复测结论；不修改漏洞状态、不写处置、不触发推送",
	})
}

// auditReplay 登记 vuln.replay（每次实际发送；不含请求/响应内容）。
func (c *Capability) auditReplay(ctx context.Context, target *reproStepTarget, taskID string, req *http.Request, detail map[string]any, outcome string) {
	c.audit(ctx, auditEntry{Actor: auditActorUser, Action: "vuln.replay", TargetKind: "vuln", TargetID: target.item.ID, TaskID: taskID, AssetID: target.item.assetID,
		Summary: fmt.Sprintf("一键重放「%s」第 %d 步：%s %s → %s", target.item.Title, detail["step"], req.Method, urlPath(req.URL.String()), outcome),
		Detail:  detail})
}
