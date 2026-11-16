// Package httpapi 提供同源、令牌保护的研究 API，不在请求处理中执行目标动作。
package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"wscyber/internal/domain"
	"wscyber/internal/execution"
	"wscyber/internal/modelconfig"
	"wscyber/internal/modelledger"
	"wscyber/internal/modelprobe"
)

type SystemInfo struct {
	ModelContentRecordingAvailable bool   `json:"model_content_recording_available"`
	Name                           string `json:"name"`
	Version                        string `json:"version"`
	ModelConfigured                bool   `json:"model_configured"`
	ModelManagementAvailable       bool   `json:"model_management_available"`
	ModelSource                    string `json:"model_source"`
	DefaultModelConfigID           string `json:"default_model_config_id"`
	Model                          string `json:"model"`
	SandboxImage                   string `json:"sandbox_image"`
	SandboxRuntime                 string `json:"sandbox_runtime"`
	NetworkMode                    string `json:"network_mode"`
	MaxParallel                    int    `json:"max_parallel"`
	MaxSteps                       int    `json:"max_steps"`
}

type Config struct {
	Token                  string
	WebDir                 string
	System                 SystemInfo
	Models                 *modelconfig.Manager
	Probes                 *modelprobe.Service
	Ledger                 *modelledger.Service
	EnvironmentFingerprint string
	MissionModels          interface {
		MissionModel(context.Context, string) (domain.MissionModelView, error)
		ValidateMissionModel(context.Context, string) error
	}
}

type server struct {
	repo domain.Repository
	exec domain.Executor
	cfg  Config
}

func New(repo domain.Repository, exec domain.Executor, cfg Config) http.Handler {
	s := &server{repo: repo, exec: exec, cfg: cfg}
	api := http.NewServeMux()
	api.HandleFunc("GET /api/system", s.systemInfo)
	api.HandleFunc("GET /api/library/folders", s.libraryFolders)
	api.HandleFunc("GET /api/library/items", s.libraryItems)
	api.HandleFunc("GET /api/library/items/{itemID}", s.libraryItem)
	api.HandleFunc("PUT /api/library/items/{itemID}", s.saveLibraryItem)
	api.HandleFunc("DELETE /api/library/items/{itemID}", s.deleteLibraryItem)
	api.HandleFunc("GET /api/library/operations/{key}", s.libraryOperation)
	api.HandleFunc("GET /api/missions/{id}/research-config", s.researchConfiguration)
	api.HandleFunc("GET /api/missions/{id}/materials/{itemID}", s.taskMaterial)
	api.HandleFunc("GET /api/missions/{id}/skill-usage", s.skillUsage)
	api.HandleFunc("GET /api/missions/{id}/skill-activities", s.skillActivities)
	api.HandleFunc("GET /api/missions/{id}/skill-loads/{loadID}", s.skillLoad)
	api.HandleFunc("GET /api/missions/{id}/skill-loads/{loadID}/calls", s.skillCalls)
	api.HandleFunc("POST /api/missions/{id}/threads/{threadID}/control", s.controlResearch)
	api.HandleFunc("POST /api/missions/{id}/threads/{threadID}/supplements", s.addSupplement)
	api.HandleFunc("GET /api/missions/{id}/threads/{threadID}/supplements", s.supplements)
	api.HandleFunc("GET /api/missions/{id}/threads/{threadID}/supplements/{supplementID}", s.supplement)
	api.HandleFunc("GET /api/missions/{id}/threads/{threadID}/supplements/{supplementID}/calls", s.supplementCalls)
	api.HandleFunc("GET /api/model-calls", s.listModelCalls)
	api.HandleFunc("GET /api/model-calls/{id}", s.getModelCall)
	api.HandleFunc("GET /api/model-calls/{id}/content", s.getModelCallContent)
	api.HandleFunc("GET /api/model-calls/{id}/context", s.getModelCallContext)
	api.HandleFunc("GET /api/model-usage", s.getModelUsage)
	api.HandleFunc("GET /api/missions/{id}/model-usage", s.getModelUsage)
	api.HandleFunc("GET /api/missions/{id}/model-recording", s.getModelRecording)
	api.HandleFunc("GET /api/model-prices", s.listModelPrices)
	api.HandleFunc("PUT /api/model-prices/{id}", s.saveModelPrice)
	api.HandleFunc("GET /api/missions/{id}/model", s.missionModel)
	api.HandleFunc("PATCH /api/model-configs/{id}", s.saveModelConfig)
	api.HandleFunc("GET /api/model-configs", s.listModelConfigs)
	api.HandleFunc("GET /api/model-configs/{id}", s.getModelConfig)
	api.HandleFunc("PUT /api/model-configs/{id}", s.saveModelConfig)
	api.HandleFunc("POST /api/model-configs/{id}/tests/{testID}", s.createModelTest)
	api.HandleFunc("GET /api/model-configs/{id}/tests/{testID}", s.getModelTest)
	api.HandleFunc("GET /api/model-settings", s.getModelSettings)
	api.HandleFunc("PUT /api/model-settings", s.setModelDefault)
	api.HandleFunc("POST /api/explorations", s.startExploration)
	api.HandleFunc("GET /api/operations/{key}", s.operation)
	api.HandleFunc("GET /api/missions", s.listMissions)
	api.HandleFunc("POST /api/missions", s.createMission)
	api.HandleFunc("GET /api/missions/{id}", s.getMission)
	api.HandleFunc("GET /api/missions/{id}/threads", s.listThreads)
	api.HandleFunc("POST /api/missions/{id}/findings/{findingID}/verifications", s.startVerification)
	api.HandleFunc("GET /api/missions/{id}/findings/{findingID}/verifications", s.listVerifications)
	api.HandleFunc("GET /api/missions/{id}/findings/{findingID}/verifications/{verificationID}", s.verification)
	api.HandleFunc("GET /api/missions/{id}/findings/{findingID}/verifications/{verificationID}/evidence/{index}", s.verificationEvidence)
	api.HandleFunc("GET /api/missions/{id}/findings", s.listFindings)
	api.HandleFunc("GET /api/missions/{id}/findings/{findingID}", s.finding)
	api.HandleFunc("GET /api/missions/{id}/findings/{findingID}/summary", s.findingSummary)
	api.HandleFunc("GET /api/missions/{id}/findings/{findingID}/history", s.findingHistory)
	api.HandleFunc("GET /api/missions/{id}/findings/{findingID}/revisions/{revisionID}", s.finding)
	api.HandleFunc("GET /api/missions/{id}/findings/{findingID}/revisions/{revisionID}/evidence/{index}", s.findingEvidence)
	api.HandleFunc("GET /api/missions/{id}/http-records", s.listHTTPRecords)
	api.HandleFunc("GET /api/missions/{id}/http-records/{actionID}", s.httpRecord)
	api.HandleFunc("POST /api/missions/{id}/threads", s.createThread)
	api.HandleFunc("GET /api/threads/{id}", s.snapshot)
	api.HandleFunc("POST /api/threads/{id}/control", s.control)
	api.HandleFunc("POST /api/threads/{id}/observations", s.observe)
	api.HandleFunc("POST /api/threads/{id}/actions", s.propose)
	api.HandleFunc("GET /api/actions", s.listActions)
	api.HandleFunc("POST /api/actions/{id}/decision", s.decide)
	api.HandleFunc("POST /api/actions/{id}/resolve", s.resolve)
	api.HandleFunc("GET /api/events", s.events)
	api.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fail(w, domain.ErrNotFound) })
	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	root.Handle("/api/", s.auth(api))
	root.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "方法不支持", http.StatusMethodNotAllowed)
			return
		}
		s.static(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		root.ServeHTTP(w, r)
	})
}

func (s *server) auth(next http.Handler) http.Handler {
	expected := sha256.Sum256([]byte(s.cfg.Token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		auth := r.Header.Get("Authorization")
		supplied := sha256.Sum256([]byte(strings.TrimPrefix(auth, "Bearer ")))
		if s.cfg.Token == "" || !strings.HasPrefix(auth, "Bearer ") || subtle.ConstantTimeCompare(expected[:], supplied[:]) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "需要有效的访问令牌"})
			return
		}
		// Bearer 不自动随跨站请求附带；再拒绝跨站上下文，且不开放 CORS。
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			if err != nil || u.Scheme != scheme || u.Host != r.Host || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
				writeJSON(w, 403, map[string]string{"error": "不允许跨源请求"})
				return
			}
		}
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeJSON(w, 403, map[string]string{"error": "不允许跨站请求"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
		return fmt.Errorf("%w：需要 application/json", domain.ErrInvalid)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w：JSON 格式、字段或大小不符合要求", domain.ErrInvalid)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%w：只能提交一个 JSON 对象", domain.ErrInvalid)
	}
	return nil
}
func required(v string, max int) bool { return strings.TrimSpace(v) != "" && len(v) <= max }
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, err error) {
	code := 500
	msg := "服务暂时无法完成操作，请保留当前工作并查看服务状态"
	switch {
	case errors.Is(err, domain.ErrNotFound):
		code = 404
		msg = err.Error()
	case errors.Is(err, domain.ErrConflict):
		code = 409
		msg = err.Error()
	case errors.Is(err, domain.ErrInvalid):
		code = 400
		msg = err.Error()
	case errors.Is(err, domain.ErrUnavailable):
		code = 503
		msg = err.Error()
	}
	// 不将含 SQL 参数、URL 凭据或用户材料的底层错误外发/写入日志。
	if code == 500 {
		slog.Error("API 持久化操作失败", "error_type", fmt.Sprintf("%T", err))
	}
	body := map[string]string{"error": msg}
	if errors.Is(err, domain.ErrMaterialSelectionStale) {
		body["code"] = "material_selection_stale"
	}
	writeJSON(w, code, body)
}
func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, 200, v)
}

func (s *server) listMissions(w http.ResponseWriter, r *http.Request) {
	v, e := s.repo.ListMissions(r.Context())
	respond(w, v, e)
}
func (s *server) getMission(w http.ResponseWriter, r *http.Request) {
	v, e := s.repo.GetMission(r.Context(), r.PathValue("id"))
	respond(w, v, e)
}
func (s *server) createMission(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title          string   `json:"title"`
		Origin         string   `json:"origin"`
		Goal           string   `json:"goal"`
		AllowedOrigins []string `json:"allowed_origins"`
	}
	if err := decode(w, r, &in); err != nil {
		fail(w, err)
		return
	}
	if !required(in.Title, 200) || !required(in.Goal, 32000) || len(in.Origin) > 64000 || len(in.AllowedOrigins) == 0 || len(in.AllowedOrigins) > 64 {
		fail(w, fmt.Errorf("%w：请填写标题、目标和 1–64 个明确授权 origin", domain.ErrInvalid))
		return
	}
	origins := []string{}
	seen := map[string]bool{}
	for _, raw := range in.AllowedOrigins {
		o, err := execution.NormalizeOrigin(raw)
		if err != nil {
			fail(w, fmt.Errorf("%w：授权范围必须为精确 HTTP(S) origin，不含路径或通配符", domain.ErrInvalid))
			return
		}
		if !seen[o] {
			origins = append(origins, o)
			seen[o] = true
		}
	}
	v, e := s.repo.CreateMission(r.Context(), domain.Mission{Title: in.Title, Origin: in.Origin, Goal: in.Goal, AllowedOrigins: origins})
	if e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 201, v)
}
func (s *server) listThreads(w http.ResponseWriter, r *http.Request) {
	if _, e := s.repo.GetMission(r.Context(), r.PathValue("id")); e != nil {
		fail(w, e)
		return
	}
	v, e := s.repo.ListThreads(r.Context(), r.PathValue("id"))
	respond(w, v, e)
}
func (s *server) createThread(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title     string `json:"title"`
		Question  string `json:"question"`
		Rationale string `json:"rationale"`
		Priority  int    `json:"priority"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if !required(in.Title, 200) || !required(in.Question, 16000) || len(in.Rationale) > 32000 || in.Priority < 0 || in.Priority > 100 {
		fail(w, fmt.Errorf("%w：标题、问题必填，优先级为 0–100", domain.ErrInvalid))
		return
	}
	v, e := s.repo.CreateThread(r.Context(), domain.Thread{MissionID: r.PathValue("id"), Title: in.Title, Question: in.Question, Rationale: in.Rationale, Priority: in.Priority, Status: "ready", AutoRun: false})
	if e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 201, v)
}
func (s *server) snapshot(w http.ResponseWriter, r *http.Request) {
	v, e := s.repo.Snapshot(r.Context(), r.PathValue("id"))
	respond(w, v, e)
}
func (s *server) control(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Command string `json:"command"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	state := "ready"
	auto := false
	switch in.Command {
	case "run":
		if s.cfg.MissionModels != nil {
			thread, err := s.repo.GetThread(r.Context(), r.PathValue("id"))
			if err != nil {
				fail(w, err)
				return
			}
			if err := s.cfg.MissionModels.ValidateMissionModel(r.Context(), thread.MissionID); err != nil {
				fail(w, err)
				return
			}
		} else if !s.cfg.System.ModelConfigured {
			writeJSON(w, 503, map[string]string{"error": "模型尚未配置，可先使用人工研究与动作审批"})
			return
		}
		auto = true
	case "resume":
	case "pause":
		state = "paused"
	default:
		fail(w, domain.ErrInvalid)
		return
	}
	v, e := s.repo.SetThreadControl(r.Context(), r.PathValue("id"), state, auto)
	respond(w, v, e)
}
func (s *server) observe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Content string `json:"content"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if !required(in.Content, 64000) {
		fail(w, fmt.Errorf("%w：观察不能为空或超过 64KB", domain.ErrInvalid))
		return
	}
	v, e := s.repo.AddObservation(r.Context(), domain.Observation{ThreadID: r.PathValue("id"), Kind: "operator_note", Content: in.Content})
	if e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 201, v)
}
func (s *server) propose(w http.ResponseWriter, r *http.Request) {
	var in domain.ActionInput
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	t, e := s.repo.GetThread(r.Context(), r.PathValue("id"))
	if e != nil {
		fail(w, e)
		return
	}
	m, e := s.repo.GetMission(r.Context(), t.MissionID)
	if e != nil {
		fail(w, e)
		return
	}
	if e = s.exec.Validate(in, m.AllowedOrigins); e != nil {
		fail(w, fmt.Errorf("%w：%s", domain.ErrInvalid, e.Error()))
		return
	}
	v, e := s.repo.ProposeAction(r.Context(), t.ID, "", in)
	if e != nil {
		fail(w, e)
		return
	}
	writeJSON(w, 201, v)
}
func (s *server) listActions(w http.ResponseWriter, r *http.Request) {
	v, e := s.repo.ListActions(r.Context(), r.URL.Query().Get("mission_id"))
	respond(w, v, e)
}
func (s *server) decide(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if (in.Decision != "approved" && in.Decision != "rejected") || len(in.Reason) > 8000 {
		fail(w, domain.ErrInvalid)
		return
	}
	v, e := s.repo.DecideAction(r.Context(), r.PathValue("id"), in.Decision, in.Reason)
	respond(w, v, e)
}
func (s *server) resolve(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Note string `json:"note"`
	}
	if e := decode(w, r, &in); e != nil {
		fail(w, e)
		return
	}
	if !required(in.Note, 8000) {
		fail(w, fmt.Errorf("%w：必须填写人工核对记录", domain.ErrInvalid))
		return
	}
	if e := s.repo.ResolveUnknownAction(r.Context(), r.PathValue("id"), in.Note); e != nil {
		fail(w, e)
		return
	}
	v, e := s.repo.GetAction(r.Context(), r.PathValue("id"))
	respond(w, v, e)
}
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	var after int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		n, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || n < 0 {
			fail(w, domain.ErrInvalid)
			return
		}
		after = n
	}
	v, e := s.repo.ListEvents(r.Context(), r.URL.Query().Get("mission_id"), after)
	respond(w, v, e)
}
func (s *server) static(w http.ResponseWriter, r *http.Request) {
	clean := filepath.Clean("/" + r.URL.Path)
	path := filepath.Join(s.cfg.WebDir, clean)
	if fi, e := os.Stat(path); e == nil && fi.Mode().IsRegular() {
		http.ServeFile(w, r, path)
		return
	}
	if filepath.Ext(clean) != "" {
		http.NotFound(w, r)
		return
	}
	path = filepath.Join(s.cfg.WebDir, "index.html")
	if fi, e := os.Stat(path); e != nil || !fi.Mode().IsRegular() {
		http.Error(w, "前端未构建，请在 web 运行 npm ci && npm run build", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, path)
}
