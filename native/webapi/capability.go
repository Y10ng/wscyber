// Package webapi 保存“目标/已知信息 + 希望达到的目标 + 明确授权 origins”任务记录，
// 为后续原生 Open/Run 与 HTTP 证据接线提供可持久回读的关联意图。
package webapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/chainreactors/cyber/pkg/web"
	managementapi "github.com/chainreactors/cyber/pkg/web/api"
	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

// Task 是已保存任务的可核对记录。InitialSessionID 是服务端首次生成的
// 原生首次会话关联意图，不代表 session 已创建。
type Task struct {
	ID               string   `json:"id"`
	KnownInfo        string   `json:"known_info"`
	Objective        string   `json:"objective"`
	Origins          []string `json:"origins"`
	InitialSessionID string   `json:"initial_session_id"`
	CreatedAt        string   `json:"created_at"`
	// Policy 只在接口响应中附带当前测试策略，不进入任务规范比较。
	Policy *TaskPolicy `json:"policy,omitempty"`
	// Asset 只在接口响应中附带：从资产发起的任务为 {id,name}，否则 null。
	Asset *TaskAssetRef `json:"asset"`
	// Scenario 只在接口响应中附带当前研究场景（M1），不进入任务规范比较。
	Scenario *TaskScenario `json:"scenario,omitempty"`
}

// taskAuthorization 是规范保存的授权部分（已规范化 origins）。
type taskAuthorization struct {
	Confirmed bool     `json:"confirmed"`
	Origins   []string `json:"origins"`
}

// SaveTask 是 PUT 请求体。初始 session id 只由服务端生成，不接受客户端指定。
type SaveTask struct {
	KnownInfo     string             `json:"known_info"`
	Objective     string             `json:"objective"`
	Authorization *taskAuthorization `json:"authorization"`
	// 可选研究场景（M1）：只在新建任务时随任务保存；不参与规范比较。
	Scenario     *string `json:"scenario,omitempty"`
	DoneCriteria *string `json:"done_criteria,omitempty"`
}

// SaveResult 是 PUT 响应体。已存在且内容一致时返回同一任务（规范字段一致，
// 忽略服务端生成字段）。
type SaveResult struct {
	Task       *Task `json:"task"`
	Conflict   bool  `json:"conflict"`
	Created    bool  `json:"created"`
	Idempotent bool  `json:"idempotent"`
}

// normalizeOrigin 规范化一个明确 HTTP(S) origin：拒 userinfo/路径/query/fragment，
// scheme/host 小写、默认端口省略、IPv6 保留方括号、端口范围 1-65535；
// 非法或空返回错误。用 JoinHostPort 重建 authority，不做 DNS/连通探测。
func normalizeOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("origin 不能为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("origin 解析失败")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("origin 必须是 http/https")
	}
	if u.User != nil {
		return "", fmt.Errorf("origin 不允许 userinfo")
	}
	if u.Path != "" && u.Path != "/" {
		return "", fmt.Errorf("origin 不允许路径")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("origin 不允许 query/fragment")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", fmt.Errorf("origin 缺少 host")
	}
	port := u.Port()
	if port == "" {
		if scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	} else if n, convErr := strconv.Atoi(port); convErr != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("origin 端口超出范围")
	}
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		return scheme + "://" + defaultPortHost(host), nil
	}
	return scheme + "://" + net.JoinHostPort(host, port), nil
}

// defaultPortHost 在省略端口时重建 authority：IPv6 需保留方括号。
func defaultPortHost(host string) string {
	if strings.Contains(host, ":") {
		return "[" + strings.Trim(host, "[]") + "]"
	}
	return host
}

// newSessionID 生成独立会话关联 ID（与上游 randomID 同实现：8 字节加密随机 hex）。
func newSessionID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// validateSaveTask 校验并规范化输入；保存原始两输入文本（仅 Trim 判空），
// origins 去重规范化。这里只保存授权，不发网络、不做 DNS/连通探测。
func validateSaveTask(body *SaveTask) (origins []string, err error) {
	if body == nil {
		return nil, fmt.Errorf("请求体不能为空")
	}
	if strings.TrimSpace(body.KnownInfo) == "" {
		return nil, fmt.Errorf("known_info 不能为空")
	}
	if strings.TrimSpace(body.Objective) == "" {
		return nil, fmt.Errorf("objective 不能为空")
	}
	auth := body.Authorization
	if auth == nil || !auth.Confirmed {
		return nil, fmt.Errorf("授权必须 confirmed=true")
	}
	seen := map[string]bool{}
	for _, raw := range auth.Origins {
		norm, normErr := normalizeOrigin(raw)
		if normErr != nil {
			return nil, normErr
		}
		if !seen[norm] {
			seen[norm] = true
			origins = append(origins, norm)
		}
	}
	if len(origins) == 0 {
		return nil, fmt.Errorf("授权至少需要一个 origin")
	}
	// 排序保证同集合换序提交是同一规范请求，不产生无意义 409。
	sort.Strings(origins)
	return origins, nil
}

// decodeSaveTask 解析 JSON 请求体；顶层必须恰好一个 JSON 值（尾随再 Decode
// 应得 io.EOF），简单 JSON 错误返回 400。
func decodeSaveTask(r *http.Request) (*SaveTask, error) {
	defer r.Body.Close()
	var body SaveTask
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxRequestBytes))
	if err := decoder.Decode(&body); err != nil {
		return nil, fmt.Errorf("请求体不是合法 JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("请求体只能包含一个 JSON 对象")
	}
	return &body, nil
}

const maxRequestBytes = 1 << 20 // 1 MiB，沿用普通 JSON 请求上限习惯。

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// taskIDFromPath 校验路径中的任务 ID：普通安全路径 ID，不含分隔符/控制字符。
func taskIDFromPath(r *http.Request) (string, error) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" || len(id) > 128 {
		return "", fmt.Errorf("任务 id 非法")
	}
	if strings.ContainsAny(id, "/\\?#%") || strings.ContainsFunc(id, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
		return "", fmt.Errorf("任务 id 非法")
	}
	return id, nil
}

// handlePutTask 保存任务：同一 ID 同规范请求返回同一任务；不同内容 409，不覆盖原授权。
func (c *Capability) handlePutTask(w http.ResponseWriter, r *http.Request) {
	id, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	body, err := decodeSaveTask(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	origins, err := validateSaveTask(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var scenario *taskScenarioModel
	if input := (scenarioInput{Scenario: body.Scenario, DoneCriteria: body.DoneCriteria}); input.provided() {
		key, criteria, err := input.resolve(ScenarioWebAPIPentest, "")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		scenario = &taskScenarioModel{Scenario: key, DoneCriteria: criteria}
	}
	result, err := c.store.SaveWithScenario(r.Context(), id, body.KnownInfo, body.Objective, origins, scenario)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "保存任务失败"})
		return
	}
	task, err := c.store.withPolicy(r.Context(), result.Task)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取测试策略失败"})
		return
	}
	result.Task = task
	switch {
	case result.Conflict:
		writeJSON(w, http.StatusConflict, map[string]any{"error": "任务已存在且内容不同", "conflict": true, "task": result.Task})
	case result.Idempotent:
		writeJSON(w, http.StatusOK, SaveResult{Task: result.Task, Idempotent: true})
	default:
		c.auditTaskCreate(r.Context(), result.Task, "", result.Task.Policy.Scope, result.Task.Policy.Approval)
		status := http.StatusCreated
		writeJSON(w, status, SaveResult{Task: result.Task, Created: true})
	}
}

// handleGetTask 回读真实保存字段；不存在 404。
func (c *Capability) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id, err := taskIDFromPath(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	task, err := c.store.Get(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取任务失败"})
		return
	}
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "任务不存在"})
		return
	}
	if task, err = c.store.withPolicy(r.Context(), task); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "读取测试策略失败"})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (c *Capability) handleGetTaskBySession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		writeJSON(w, 400, map[string]string{"error": "session_id不能为空"})
		return
	}
	task, err := c.store.taskForSession(r.Context(), sessionID)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取会话任务失败"})
		return
	}
	if task == nil {
		writeJSON(w, 404, map[string]string{"error": "会话无已保存任务"})
		return
	}
	if task, err = c.store.withPolicy(r.Context(), task); err != nil {
		writeJSON(w, 500, map[string]string{"error": "读取测试策略失败"})
		return
	}
	writeJSON(w, 200, task)
}

// Capability 是原生 Web Capability：任务与受保护HTTP证据业务表 + 鉴权内路由。
type Capability struct {
	store     *taskStore
	approvals *approvalRuntime
	// secrets 加密资产测试账号；密钥文件与 hub.sqlite 同在 state-dir。
	secrets *secretBox
	// launcher 由 Routes 从原生管理 API 注入（从资产发起任务用）；测试可替换。
	launcher    sessionLauncher
	launchLocks sync.Map
	// vulnLocks 按资产串行漏洞库同步与人工归并操作。
	vulnLocks sync.Map
	// triageMu 串行漏洞处置与 SLA 的读改写（资产条目另持 vulnLocks，先资产锁后本锁）。
	triageMu sync.Mutex
	// 周期计划调度器（Routes 拿到原生管理 API 时启动，Close 停止）；测试可注入时钟与时区。
	sched       scheduler
	scheduleNow func() time.Time
	scheduleLoc *time.Location
	// egressCADir 是全工具出口 MITM CA 目录：state-dir 下 egress-ca/，不在节点工作区内。
	egressCADir string
	// notify 是企业微信推送 goroutine（Routes 拿到原生管理 API 时启动，Close 停止）与发送频率窗口。
	notify notifyRuntime
	// replays 是一键重放的进行中占位与按条目限频（进程内存，见 replay.go）。
	replays replayRuntime
	// subsessions 是本进程内运行中的子 Agent 会话 → 任务（节点侧用量观察用，见 subagent.go）。
	subsessions sync.Map
	// subsessionModelCalls 是子会话的模型请求计数（*atomic.Int64），SessionEnd 清除。
	subsessionModelCalls sync.Map
}

// NewCapability 创建能力；database 为同一 hub.sqlite 路径。
func NewCapability(database string) (*Capability, error) {
	store, err := openTaskStore(database)
	if err != nil {
		return nil, fmt.Errorf("打开 webapi 任务存储: %w", err)
	}
	capability := &Capability{store: store, approvals: newApprovalRuntime(), secrets: newSecretBox(filepath.Join(filepath.Dir(database), secretKeyFileName)),
		egressCADir: filepath.Join(filepath.Dir(database), egressCADirName)}
	notifyCapabilities.Store(store, capability)
	return capability, nil
}

func (c *Capability) ID() string { return "webapi" }

func (c *Capability) Manifest() web.Capability {
	return web.Capability{ID: "webapi", Title: "Web/API 任务输入", APIRoutes: []string{"WebAPITaskService"}}
}

// SchemaModules 声明本能力任务、证据、候选、验证、报告和复测业务表。
func (c *Capability) SchemaModules() []webservice.SchemaModule {
	return []webservice.SchemaModule{SchemaModule()}
}

// Routes 注册原生鉴权内的业务输入/读取路由。
func (c *Capability) Routes(api *managementapi.API) []web.Route {
	if api != nil {
		c.launcher = apiLauncher{api: api}
		// 模型节点随 Hub 进程启动：上次遗留的 running 子会话按已中断结束并释放 Oracle（M5a）。
		if err := c.store.releaseOrphanSubsessions(context.Background(), time.Now()); err != nil {
			log.Printf("webapi 遗留子会话释放失败：%v", err)
		}
		c.startScheduler()
		c.startNotifier()
	}
	return []web.Route{
		{Pattern: "PUT /api/webapi/tasks/{id}", Handler: http.HandlerFunc(c.handlePutTask)},
		{Pattern: "GET /api/webapi/tasks", Handler: http.HandlerFunc(c.handleGetTaskBySession)},
		{Pattern: "GET /api/webapi/tasks/{id}", Handler: http.HandlerFunc(c.handleGetTask)},
		{Pattern: "PUT /api/webapi/tasks/{id}/policy", Handler: http.HandlerFunc(c.handlePutPolicy)},
		{Pattern: "PUT /api/webapi/tasks/{id}/scenario", Handler: http.HandlerFunc(c.handlePutTaskScenario)},
		{Pattern: "GET /api/webapi/scenarios", Handler: http.HandlerFunc(c.handleScenarioList)},
		{Pattern: "GET /api/webapi/tasks/{id}/evidence", Handler: http.HandlerFunc(c.handleEvidenceList)},
		{Pattern: "GET /api/webapi/tasks/{id}/evidence/{evidence_id}", Handler: http.HandlerFunc(c.handleEvidenceGet)},
		{Pattern: "GET /api/webapi/tasks/{id}/evidence/{evidence_id}/image.png", Handler: http.HandlerFunc(c.handleEvidenceImage)},
		{Pattern: "GET /api/webapi/tasks/{id}/egress", Handler: http.HandlerFunc(c.handleEgressList)},
		{Pattern: "GET /api/webapi/tasks/{id}/egress/{flow_id}", Handler: http.HandlerFunc(c.handleEgressGet)},
		{Pattern: "GET /api/webapi/tasks/{id}/egress/{flow_id}/image.png", Handler: http.HandlerFunc(c.handleEgressImage)},
		{Pattern: "GET /api/webapi/tasks/{id}/authz-compares", Handler: http.HandlerFunc(c.handleAuthzCompareList)},
		{Pattern: "GET /api/webapi/tasks/{id}/subsessions", Handler: http.HandlerFunc(c.handleSubsessionList)},
		{Pattern: "GET /api/webapi/tasks/{id}/board", Handler: http.HandlerFunc(c.handleBoardGet)},
		{Pattern: "GET /api/webapi/tasks/{id}/board/endpoints", Handler: http.HandlerFunc(c.handleBoardEndpoints)},
		{Pattern: "GET /api/webapi/tasks/{id}/board/facts", Handler: http.HandlerFunc(c.handleBoardFacts)},
		{Pattern: "GET /api/webapi/tasks/{id}/board/oracles", Handler: http.HandlerFunc(c.handleBoardOracles)},
		{Pattern: "POST /api/webapi/tasks/{id}/board/oracles", Handler: http.HandlerFunc(c.handleBoardOracleCreate)},
		{Pattern: "PUT /api/webapi/tasks/{id}/board/oracles/{number}", Handler: http.HandlerFunc(c.handleBoardOracleUpdate)},
		{Pattern: "GET /api/webapi/tasks/{id}/findings", Handler: http.HandlerFunc(c.handleFindingList)},
		{Pattern: "GET /api/webapi/tasks/{id}/findings/{finding_id}", Handler: http.HandlerFunc(c.handleFindingCurrent)},
		{Pattern: "GET /api/webapi/tasks/{id}/findings/{finding_id}/history", Handler: http.HandlerFunc(c.handleFindingHistory)},
		{Pattern: "GET /api/webapi/tasks/{id}/findings/{finding_id}/revisions/{revision_id}", Handler: http.HandlerFunc(c.handleFindingRevision)},
		{Pattern: "PUT /api/webapi/tasks/{id}/findings/{finding_id}/verifications/{verification_id}", Handler: http.HandlerFunc(c.handleVerificationPut)},
		{Pattern: "GET /api/webapi/tasks/{id}/findings/{finding_id}/verifications/{verification_id}", Handler: http.HandlerFunc(c.handleVerificationGet)},
		{Pattern: "GET /api/webapi/tasks/{id}/findings/{finding_id}/verifications", Handler: http.HandlerFunc(c.handleVerificationList)},
		{Pattern: "GET /api/webapi/verifications", Handler: http.HandlerFunc(c.handleVerificationBySession)},
		{Pattern: "PUT /api/webapi/tasks/{id}/findings/{finding_id}/retests/{retest_id}", Handler: http.HandlerFunc(c.handleRetestPut)},
		{Pattern: "GET /api/webapi/tasks/{id}/findings/{finding_id}/retests/{retest_id}", Handler: http.HandlerFunc(c.handleRetestGet)},
		{Pattern: "GET /api/webapi/tasks/{id}/findings/{finding_id}/retests", Handler: http.HandlerFunc(c.handleRetestList)},
		{Pattern: "GET /api/webapi/retests", Handler: http.HandlerFunc(c.handleRetestBySession)},
		{Pattern: "PUT /api/webapi/tasks/{id}/reports/{report_id}", Handler: http.HandlerFunc(c.handleReportPut)},
		{Pattern: "GET /api/webapi/tasks/{id}/reports", Handler: http.HandlerFunc(c.handleReportList)},
		{Pattern: "GET /api/webapi/tasks/{id}/reports/{report_id}", Handler: http.HandlerFunc(c.handleReportGet)},
		{Pattern: "GET /api/webapi/tasks/{id}/reports/{report_id}/export", Handler: http.HandlerFunc(c.handleReportExport)},
		{Pattern: "GET /api/webapi/reports", Handler: http.HandlerFunc(c.handleReportBySession)},
		{Pattern: "POST /api/webapi/session-kinds", Handler: http.HandlerFunc(c.handleSessionKinds)},
		{Pattern: "GET /api/webapi/usage", Handler: http.HandlerFunc(c.handleUsage)},
		{Pattern: "GET /api/webapi/task-list", Handler: http.HandlerFunc(c.handleTaskList)},
		{Pattern: "GET /api/webapi/vulns", Handler: http.HandlerFunc(c.handleGlobalVulns)},
		{Pattern: "PUT /api/webapi/vulns/{key}/triage", Handler: http.HandlerFunc(c.handleVulnTriagePut)},
		{Pattern: "GET /api/webapi/vulns/{key}/card", Handler: http.HandlerFunc(c.handleVulnCard)},
		{Pattern: "GET /api/webapi/vulns/{key}/card/curl", Handler: http.HandlerFunc(c.handleVulnCardCurl)},
		{Pattern: "POST /api/webapi/vulns/{key}/card/replay", Handler: http.HandlerFunc(c.handleVulnCardReplay)},
		{Pattern: "POST /api/webapi/vulns/{key}/card/notify", Handler: http.HandlerFunc(c.handleVulnCardNotify)},
		{Pattern: "GET /api/webapi/vulns/{key}/card/notify/preview", Handler: http.HandlerFunc(c.handleVulnCardNotifyPreview)},
		{Pattern: "GET /api/webapi/vulns/{key}/card/export", Handler: http.HandlerFunc(c.handleVulnCardExport)},
		{Pattern: "POST /api/webapi/vulns/export", Handler: http.HandlerFunc(c.handleVulnExport)},
		{Pattern: "GET /api/webapi/vuln-sla", Handler: http.HandlerFunc(c.handleVulnSLAGet)},
		{Pattern: "PUT /api/webapi/vuln-sla", Handler: http.HandlerFunc(c.handleVulnSLAPut)},
		{Pattern: "GET /api/webapi/report-list", Handler: http.HandlerFunc(c.handleGlobalReportList)},
		{Pattern: "GET /api/webapi/overview", Handler: http.HandlerFunc(c.handleOverview)},
		{Pattern: "GET /api/webapi/assets", Handler: http.HandlerFunc(c.handleAssetList)},
		{Pattern: "GET /api/webapi/assets/{id}", Handler: http.HandlerFunc(c.handleAssetGet)},
		{Pattern: "PUT /api/webapi/assets/{id}", Handler: http.HandlerFunc(c.handleAssetPut)},
		{Pattern: "PUT /api/webapi/assets/{id}/credentials/{cid}", Handler: http.HandlerFunc(c.handleCredentialPut)},
		{Pattern: "DELETE /api/webapi/assets/{id}/credentials/{cid}", Handler: http.HandlerFunc(c.handleCredentialDelete)},
		{Pattern: "GET /api/webapi/assets/{id}/tasks", Handler: http.HandlerFunc(c.handleAssetTasks)},
		{Pattern: "PUT /api/webapi/assets/{id}/tasks/{task_id}", Handler: http.HandlerFunc(c.handleAssetLaunch)},
		{Pattern: "GET /api/webapi/assets/{id}/schedule", Handler: http.HandlerFunc(c.handleScheduleGet)},
		{Pattern: "PUT /api/webapi/assets/{id}/schedule", Handler: http.HandlerFunc(c.handleSchedulePut)},
		{Pattern: "DELETE /api/webapi/assets/{id}/schedule", Handler: http.HandlerFunc(c.handleScheduleDelete)},
		{Pattern: "GET /api/webapi/assets/{id}/schedule/runs", Handler: http.HandlerFunc(c.handleScheduleRuns)},
		{Pattern: "GET /api/webapi/assets/{id}/vulns", Handler: http.HandlerFunc(c.handleVulnList)},
		{Pattern: "GET /api/webapi/assets/{id}/vulns/{vid}", Handler: http.HandlerFunc(c.handleVulnGet)},
		{Pattern: "PUT /api/webapi/assets/{id}/vulns/{vid}", Handler: http.HandlerFunc(c.handleVulnTitle)},
		{Pattern: "PUT /api/webapi/assets/{id}/vulns/{vid}/members/{finding_id}", Handler: http.HandlerFunc(c.handleVulnMember)},
		{Pattern: "POST /api/webapi/assets/{id}/vulns/{vid}/merge", Handler: http.HandlerFunc(c.handleVulnMerge)},
		{Pattern: "GET /api/webapi/schedules", Handler: http.HandlerFunc(c.handleScheduleList)},
		{Pattern: "GET /api/webapi/schedule-runs/{run_id}/followups", Handler: http.HandlerFunc(c.handleScheduleRunFollowups)},
		{Pattern: "GET /api/webapi/notify/wecom", Handler: http.HandlerFunc(c.handleNotifySettingsGet)},
		{Pattern: "PUT /api/webapi/notify/wecom", Handler: http.HandlerFunc(c.handleNotifySettingsPut)},
		{Pattern: "POST /api/webapi/notify/wecom/test", Handler: http.HandlerFunc(c.handleNotifyTest)},
		{Pattern: "GET /api/webapi/notifications", Handler: http.HandlerFunc(c.handleNotificationList)},
		{Pattern: "POST /api/webapi/notifications/{id}/confirm", Handler: http.HandlerFunc(c.handleNotificationConfirm)},
		{Pattern: "POST /api/webapi/notifications/{id}/dismiss", Handler: http.HandlerFunc(c.handleNotificationDismiss)},
		{Pattern: "POST /api/webapi/notifications/{id}/retry", Handler: http.HandlerFunc(c.handleNotificationRetry)},
		{Pattern: "GET /api/webapi/audit", Handler: http.HandlerFunc(c.handleAuditList)},
	}
}

func (c *Capability) RegisterNamespaces(_ *aop.NamespaceMux) error { return nil }

// Close 生命周期跟随原生 Capability.Close，关闭自有连接。
func (c *Capability) Close(_ context.Context) error {
	c.stopScheduler()
	c.stopNotifier()
	if c.store != nil {
		notifyCapabilities.Delete(c.store)
		return c.store.Close()
	}
	return nil
}

var _ webservice.Capability = (*Capability)(nil)
