package webapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/uptrace/bun"
	"google.golang.org/protobuf/proto"

	"github.com/chainreactors/cyber/aop/traffic"
)

// 任务黑板（M2）：地图（端点，自动从已发送且有响应的流量整理 + 模型补充）、事实（有证据的客观信息）、
// Oracle（具体可证伪的待证明问题）。Oracle 成立 ≠ 漏洞确认：成立必须关联一个候选，之后照旧独立验证。
// 契约见 native/BOARD.md。

const (
	boardSourceTraffic = "traffic"
	boardSourceModel   = "model"

	boardAuthNone = "none"
	boardAuthWith = "with"
	boardAuthBoth = "both"

	boardMaxParams      = 100
	boardMaxStatusCodes = 50
	boardMaxParamBytes  = 100
	boardIngestBatch    = 500
	// boardInflightGrace：已认领未完成（completed_at 为空）且创建不足该时长的请求挡住摄取游标，
	// 等其完成后再并入；超过该时长仍未完成的视为不会再有响应（进程中断遗留），跳过。
	boardInflightGrace = 2 * time.Minute

	boardDigestDefaultChars = 2000

	oracleOpen       = "open"
	oracleInProgress = "in_progress"
	oracleHolds      = "holds"
	oracleRefuted    = "refuted"
	oracleBlocked    = "blocked"
	oracleDropped    = "dropped"

	factConfirmed = "confirmed"
	factInferred  = "inferred"
	factActive    = "active"
	factRetracted = "retracted"
)

var (
	boardFactKinds       = []string{"endpoint", "auth", "role", "tech", "config", "data", "other"}
	boardOracleCategory  = []string{"authz", "authn", "injection", "ssrf", "file", "logic", "info_disclosure", "config", "other"}
	boardOraclePriority  = []string{"high", "medium", "low"}
	boardOracleStatuses  = []string{oracleOpen, oracleInProgress, oracleHolds, oracleRefuted, oracleBlocked, oracleDropped}
	boardFactNamespace   = [16]byte{0x5b, 0x0a, 0xd1, 0x3e, 0x27, 0x94, 0x4c, 0x1f, 0x8e, 0x62, 0xa9, 0x70, 0x3d, 0xc5, 0x18, 0xb4}
	boardEndpointNS      = [16]byte{0x9e, 0x41, 0x07, 0xc2, 0x6a, 0x3b, 0x45, 0xd8, 0xb1, 0x0f, 0x52, 0xe6, 0x84, 0x2d, 0x7a, 0x13}
	boardMethodPattern   = regexp.MustCompile(`^[A-Z][A-Z0-9_-]{0,15}$`)
	errBoardOracleAbsent = errors.New("Oracle 不存在")
)

type boardEndpointModel struct {
	bun.BaseModel `bun:"table:webapi_board_endpoints,alias:webapi_board_endpoint"`

	ID              string `bun:"id,pk"` // UUIDv5(task_id\nendpoint_key)
	TaskID          string `bun:"task_id,notnull"`
	EndpointKey     string `bun:"endpoint_key,notnull"`
	Method          string `bun:"method,notnull"`
	Origin          string `bun:"origin,notnull"`
	Path            string `bun:"path,notnull"`
	ParamsJSON      string `bun:"params_json,type:text,notnull"`
	StatusCodesJSON string `bun:"status_codes_json,type:text,notnull"`
	AuthSeen        string `bun:"auth_seen,notnull"` // none/with/both；未请求过为空串
	RequestCount    int64  `bun:"request_count,notnull"`
	FirstSeenAt     string `bun:"first_seen_at,notnull"`
	LastSeenAt      string `bun:"last_seen_at,notnull"`
	Source          string `bun:"source,notnull"`
	Note            string `bun:"note,type:text,notnull"`
}

type boardIngestModel struct {
	bun.BaseModel `bun:"table:webapi_board_ingest,alias:webapi_board_ingest"`

	TaskID        string `bun:"task_id,pk"`
	LastCreatedAt string `bun:"last_created_at,notnull"`
	LastID        string `bun:"last_id,notnull"`
}

type boardFactModel struct {
	bun.BaseModel `bun:"table:webapi_board_facts,alias:webapi_board_fact"`

	ID              string `bun:"id,pk"`
	TaskID          string `bun:"task_id,notnull"`
	Kind            string `bun:"kind,notnull"`
	Subject         string `bun:"subject,type:text,notnull"`
	Statement       string `bun:"statement,type:text,notnull"`
	Confidence      string `bun:"confidence,notnull"`
	EvidenceIDsJSON string `bun:"evidence_ids_json,type:text,notnull"`
	Status          string `bun:"status,notnull"`
	SessionID       string `bun:"session_id,notnull"`
	TurnID          string `bun:"turn_id,notnull"`
	CallID          string `bun:"call_id,notnull"`
	CreatedAt       string `bun:"created_at,notnull"`
	UpdatedAt       string `bun:"updated_at,notnull"`
}

type boardOracleModel struct {
	bun.BaseModel `bun:"table:webapi_board_oracles,alias:webapi_board_oracle"`

	ID               string `bun:"id,pk"`
	TaskID           string `bun:"task_id,notnull"`
	Number           int64  `bun:"number,notnull"` // 呈现为 O-001
	Question         string `bun:"question,type:text,notnull"`
	Category         string `bun:"category,notnull"`
	EndpointKeysJSON string `bun:"endpoint_keys_json,type:text,notnull"`
	Priority         string `bun:"priority,notnull"`
	Status           string `bun:"status,notnull"`
	Origin           string `bun:"origin,notnull"`
	ClaimedSessionID string `bun:"claimed_session_id,notnull"`
	ResultSummary    string `bun:"result_summary,type:text,notnull"`
	EvidenceIDsJSON  string `bun:"evidence_ids_json,type:text,notnull"`
	FindingID        string `bun:"finding_id,notnull"`
	DedupeKey        string `bun:"dedupe_key,notnull"` // sha256(category\n端点集合\n规范化问题)
	CreatedAt        string `bun:"created_at,notnull"`
	UpdatedAt        string `bun:"updated_at,notnull"`
}

type boardOracleEventModel struct {
	bun.BaseModel `bun:"table:webapi_board_oracle_events,alias:webapi_board_oracle_event"`

	ID         string `bun:"id,pk"`
	OracleID   string `bun:"oracle_id,notnull"`
	TaskID     string `bun:"task_id,notnull"`
	FromStatus string `bun:"from_status,notnull"` // 创建为空串
	ToStatus   string `bun:"to_status,notnull"`
	Actor      string `bun:"actor,notnull"` // model|user|ai（子 Agent 生命周期，M5a）
	SessionID  string `bun:"session_id,notnull"`
	At         string `bun:"at,notnull"`
	Summary    string `bun:"summary,type:text,notnull"`
}

// ---- 视图 ----

type BoardEndpoint struct {
	ID           string   `json:"id"`
	EndpointKey  string   `json:"endpoint_key"`
	Method       string   `json:"method"`
	Origin       string   `json:"origin"`
	Path         string   `json:"path"`
	Params       []string `json:"params"`
	StatusCodes  []int    `json:"status_codes"`
	AuthSeen     string   `json:"auth_seen"`
	RequestCount int64    `json:"request_count"`
	FirstSeenAt  string   `json:"first_seen_at"`
	LastSeenAt   string   `json:"last_seen_at"`
	Source       string   `json:"source"`
	Note         string   `json:"note"`
	Oracles      []string `json:"oracles"` // 引用该端点的 Oracle 编号（任意状态）
}

type BoardFact struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Subject     string   `json:"subject"`
	Statement   string   `json:"statement"`
	Confidence  string   `json:"confidence"`
	EvidenceIDs []string `json:"evidence_ids"`
	Status      string   `json:"status"`
	SessionID   string   `json:"session_id"`
	TurnID      string   `json:"turn_id"`
	CallID      string   `json:"call_id"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type BoardOracleEvent struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Actor     string `json:"actor"`
	SessionID string `json:"session_id"`
	At        string `json:"at"`
	Summary   string `json:"summary"`
}

type BoardOracle struct {
	ID               string             `json:"id"`
	Number           string             `json:"number"`
	Question         string             `json:"question"`
	Category         string             `json:"category"`
	EndpointKeys     []string           `json:"endpoint_keys"`
	Priority         string             `json:"priority"`
	Status           string             `json:"status"`
	Origin           string             `json:"origin"`
	ClaimedSessionID string             `json:"claimed_session_id"`
	ResultSummary    string             `json:"result_summary"`
	EvidenceIDs      []string           `json:"evidence_ids"`
	FindingID        string             `json:"finding_id"`
	FindingNumber    string             `json:"finding_number"` // WSC-000001；无关联为空串
	CreatedAt        string             `json:"created_at"`
	UpdatedAt        string             `json:"updated_at"`
	Events           []BoardOracleEvent `json:"events,omitempty"`
}

type BoardCounts struct {
	Endpoints         int64            `json:"endpoints"`
	TrafficEndpoints  int64            `json:"traffic_endpoints"`
	ModelEndpoints    int64            `json:"model_endpoints"`
	UntestedEndpoints int64            `json:"untested_endpoints"`
	Facts             int64            `json:"facts"` // 有效（active）事实
	ConfirmedFacts    int64            `json:"confirmed_facts"`
	Oracles           map[string]int64 `json:"oracles"`
}

// ---- 通用 ----

func oracleNumber(number int64) string { return fmt.Sprintf("O-%03d", number) }

// parseOracleNumber 接受 O-001 / o-1 / 1。
func parseOracleNumber(raw string) (int64, bool) {
	text := strings.TrimSpace(raw)
	if len(text) > 2 && (text[0] == 'O' || text[0] == 'o') && text[1] == '-' {
		text = text[2:]
	}
	if text == "" || len(text) > 9 || strings.TrimFunc(text, func(r rune) bool { return r >= '0' && r <= '9' }) != "" {
		return 0, false
	}
	value, err := strconv.ParseInt(text, 10, 64)
	return value, err == nil && value > 0
}

func boardNow(now time.Time) string { return notifyTime(now) }

func boardEndpointID(taskID, key string) string { return namespacedUUID(boardEndpointNS, taskID, key) }

func newBoardOracleID() string { return "bo_" + newSessionID() + newSessionID() }
func newBoardEventID() string  { return "boe_" + newSessionID() + newSessionID() }

func marshalStrings(values []string) string {
	if values == nil {
		values = []string{}
	}
	raw, _ := json.Marshal(values)
	return string(raw)
}

func decodeStrings(raw string) []string {
	out := []string{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

func decodeInts(raw string) []int {
	out := []int{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// splitEndpointKey 把 `METHOD origin/path` 拆成三段（键由 endpointKeyOf 生成，path 以 / 开头）。
func splitEndpointKey(key string) (method, origin, path string) {
	method, rest, _ := strings.Cut(key, " ")
	start := strings.Index(rest, "://")
	if start < 0 {
		return method, rest, "/"
	}
	slash := strings.IndexByte(rest[start+3:], '/')
	if slash < 0 {
		return method, rest, "/"
	}
	return method, rest[:start+3+slash], rest[start+3+slash:]
}

// normalizeEndpointRef 把 `METHOD URL`（含地图已有键）按 endpointKeyOf 归一。
func normalizeEndpointRef(raw string) (string, error) {
	method, rawURL, found := strings.Cut(strings.TrimSpace(raw), " ")
	rawURL = strings.TrimSpace(rawURL)
	if !found || rawURL == "" {
		return "", errors.New("须为 `METHOD URL` 形式（如 GET https://host/api/items/{id}）")
	}
	return normalizeEndpoint(method, rawURL)
}

func normalizeEndpoint(method, rawURL string) (string, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	if !boardMethodPattern.MatchString(method) {
		return "", errors.New("method 须为 HTTP 方法名（字母开头，≤16 个大写字母/数字/-/_）")
	}
	if len(rawURL) > 2048 || !utf8.ValidString(rawURL) {
		return "", errors.New("url 超过 2048 字节或不是有效 UTF8")
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" || u.User != nil || (strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") {
		return "", errors.New("url 须为无 userinfo 的绝对 http(s) URL")
	}
	key, ok := endpointKeyOf(method, rawURL)
	if !ok {
		return "", errors.New("url 无法解析为端点")
	}
	return key, nil
}

// normalizeOracleQuestion：小写、去空白与标点，用于去重。
func normalizeOracleQuestion(question string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(question) {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func oracleDedupeKey(category string, keys []string, question string) string {
	sum := sha256.Sum256([]byte(category + "\n" + strings.Join(keys, "\x1f") + "\n" + normalizeOracleQuestion(question)))
	return hex.EncodeToString(sum[:])
}

func oracleClosed(status string) bool {
	return status == oracleHolds || status == oracleRefuted || status == oracleBlocked || status == oracleDropped
}

// boardTx 首条 SQL 即写（确保摄取游标行存在），避免与原生 Hub 其他连接并发写时读事务升级失败。
func (s *taskStore) boardTx(ctx context.Context, taskID string, fn func(ctx context.Context, tx bun.Tx) error) error {
	return s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(&boardIngestModel{TaskID: taskID}).On("CONFLICT (task_id) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
		return fn(ctx, tx)
	})
}

// ---- 摄取 ----

type boardObservation struct {
	key, at    string
	params     []string
	statusCode int
	auth       bool
}

// boardIngest 把 webapi_requests 中已发送且有响应的记录增量并入地图（按 created_at,id 游标，幂等）。
func (s *taskStore) boardIngest(ctx context.Context, taskID string, now time.Time) error {
	for range 1000 {
		done, err := s.boardIngestBatch(ctx, taskID, now)
		if err != nil || done {
			return err
		}
	}
	return nil
}

func (s *taskStore) boardIngestCursor(ctx context.Context, db bun.IDB, taskID string) (boardIngestModel, error) {
	cursor := boardIngestModel{TaskID: taskID}
	err := db.NewSelect().Model(&cursor).Where("task_id = ?", taskID).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return boardIngestModel{TaskID: taskID}, nil
	}
	return cursor, err
}

const boardAfterCursor = "task_id = ? AND (created_at > ? OR (created_at = ? AND id > ?))"

func (s *taskStore) boardIngestBatch(ctx context.Context, taskID string, now time.Time) (bool, error) {
	cursor, err := s.boardIngestCursor(ctx, s.orm, taskID)
	if err != nil {
		return false, err
	}
	pending, err := s.orm.NewSelect().Model((*requestModel)(nil)).Where(boardAfterCursor, taskID, cursor.LastCreatedAt, cursor.LastCreatedAt, cursor.LastID).Exists(ctx)
	if err != nil || !pending {
		return true, err
	}
	done := true
	err = s.boardTx(ctx, taskID, func(ctx context.Context, tx bun.Tx) error {
		cursor, err := s.boardIngestCursor(ctx, tx, taskID)
		if err != nil {
			return err
		}
		var rows []requestModel
		if err := tx.NewSelect().Model(&rows).Column("id", "method", "url", "sent", "status_code", "flow_pb", "created_at", "completed_at").
			Where(boardAfterCursor, taskID, cursor.LastCreatedAt, cursor.LastCreatedAt, cursor.LastID).
			Order("created_at ASC", "id ASC").Limit(boardIngestBatch).Scan(ctx); err != nil {
			return err
		}
		done = len(rows) < boardIngestBatch
		endpoints := map[string]*boardEndpointModel{}
		existing := map[string]bool{}
		advanced := false
		for _, row := range rows {
			if !row.CompletedAt.Valid {
				created, parseErr := time.Parse(time.RFC3339Nano, row.CreatedAt)
				if parseErr == nil && now.Sub(created) < boardInflightGrace {
					done = true
					break
				}
			}
			cursor.LastCreatedAt, cursor.LastID, advanced = row.CreatedAt, row.ID, true
			if !row.CompletedAt.Valid || !row.Sent.Valid || !row.Sent.Bool || !row.StatusCode.Valid {
				continue
			}
			observation, ok := observeRequest(row)
			if !ok {
				continue
			}
			endpoint := endpoints[observation.key]
			if endpoint == nil {
				endpoint = &boardEndpointModel{}
				err := tx.NewSelect().Model(endpoint).Where("id = ?", boardEndpointID(taskID, observation.key)).Limit(1).Scan(ctx)
				switch {
				case err == nil:
					existing[observation.key] = true
				case errors.Is(err, sql.ErrNoRows):
					endpoint = newBoardEndpoint(taskID, observation.key, boardSourceTraffic, observation.at)
				default:
					return err
				}
				endpoints[observation.key] = endpoint
			}
			mergeObservation(endpoint, observation)
		}
		if !advanced {
			return nil
		}
		keys := make([]string, 0, len(endpoints))
		for key := range endpoints {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			endpoint := endpoints[key]
			if existing[key] {
				if _, err := tx.NewUpdate().Model(endpoint).WherePK().Exec(ctx); err != nil {
					return err
				}
			} else if _, err := tx.NewInsert().Model(endpoint).Exec(ctx); err != nil {
				return err
			}
		}
		_, err = tx.NewUpdate().Model(&cursor).WherePK().Exec(ctx)
		return err
	})
	return done, err
}

func newBoardEndpoint(taskID, key, source, at string) *boardEndpointModel {
	method, origin, path := splitEndpointKey(key)
	return &boardEndpointModel{ID: boardEndpointID(taskID, key), TaskID: taskID, EndpointKey: key, Method: method, Origin: origin, Path: path,
		ParamsJSON: "[]", StatusCodesJSON: "[]", FirstSeenAt: at, LastSeenAt: at, Source: source}
}

func mergeObservation(endpoint *boardEndpointModel, observation boardObservation) {
	params := decodeStrings(endpoint.ParamsJSON)
	seen := map[string]bool{}
	for _, name := range params {
		seen[name] = true
	}
	for _, name := range observation.params {
		if len(params) >= boardMaxParams {
			break
		}
		if !seen[name] {
			seen[name] = true
			params = append(params, name)
		}
	}
	sort.Strings(params)
	endpoint.ParamsJSON = marshalStrings(params)
	codes := decodeInts(endpoint.StatusCodesJSON)
	if !slices.Contains(codes, observation.statusCode) && len(codes) < boardMaxStatusCodes {
		codes = append(codes, observation.statusCode)
		sort.Ints(codes)
	}
	raw, _ := json.Marshal(codes)
	endpoint.StatusCodesJSON = string(raw)
	auth := boardAuthNone
	if observation.auth {
		auth = boardAuthWith
	}
	switch {
	case endpoint.AuthSeen == "" || endpoint.RequestCount == 0:
		endpoint.AuthSeen = auth
	case endpoint.AuthSeen != auth:
		endpoint.AuthSeen = boardAuthBoth
	}
	if endpoint.FirstSeenAt == "" || observation.at < endpoint.FirstSeenAt {
		endpoint.FirstSeenAt = observation.at
	}
	if observation.at > endpoint.LastSeenAt {
		endpoint.LastSeenAt = observation.at
	}
	endpoint.RequestCount++
	endpoint.Source = boardSourceTraffic // 已被实际请求过的模型补充端点转为流量来源
}

// observeRequest 取端点键、参数名（query + JSON/表单顶层字段）、状态码、是否带 Authorization/Cookie。
func observeRequest(row requestModel) (boardObservation, bool) {
	key, ok := endpointKeyOf(row.Method, row.URL)
	if !ok {
		return boardObservation{}, false
	}
	at := row.CreatedAt
	if parsed, err := time.Parse(time.RFC3339Nano, row.CreatedAt); err == nil {
		at = boardNow(parsed)
	}
	observation := boardObservation{key: key, at: at, statusCode: int(row.StatusCode.Int64)}
	names := []string{}
	if u, err := url.Parse(row.URL); err == nil {
		if values, err := url.ParseQuery(u.RawQuery); err == nil || len(values) > 0 {
			for name := range values {
				names = append(names, name)
			}
		}
	}
	var flow traffic.Flow
	if len(row.FlowPB) > 0 && proto.Unmarshal(row.FlowPB, &flow) == nil && flow.GetRequest() != nil {
		contentType := ""
		for _, header := range flow.GetRequest().GetHeaders() {
			name := strings.ToLower(header.GetName())
			switch {
			case (name == "authorization" || name == "cookie") && strings.TrimSpace(header.GetValue()) != "":
				observation.auth = true
			case name == "content-type" && contentType == "":
				contentType = strings.ToLower(header.GetValue())
			}
		}
		names = append(names, bodyFieldNames(contentType, flow.GetRequest().GetBody())...)
	}
	for _, name := range names {
		if name == "" || len(name) > boardMaxParamBytes || !utf8.ValidString(name) || strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			continue
		}
		observation.params = append(observation.params, name)
	}
	sort.Strings(observation.params)
	observation.params = slices.Compact(observation.params)
	return observation, true
}

func bodyFieldNames(contentType string, body []byte) []string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil
	}
	names := []string{}
	switch {
	case strings.Contains(contentType, "json") || contentType == "" && strings.HasPrefix(trimmed, "{"):
		var object map[string]json.RawMessage
		if json.Unmarshal(body, &object) == nil {
			for name := range object {
				names = append(names, name)
			}
		}
	case strings.Contains(contentType, "application/x-www-form-urlencoded"):
		if values, err := url.ParseQuery(trimmed); err == nil || len(values) > 0 {
			for name := range values {
				names = append(names, name)
			}
		}
	}
	return names
}

// ---- 地图（模型补充） ----

type boardMapResult struct {
	EndpointKey string `json:"endpoint_key"`
	Created     bool   `json:"created"`
	Source      string `json:"source"`
}

// boardEnsureEndpoints 在事务内补入缺失端点（source=model）；note 非空时覆盖备注。
func boardEnsureEndpoints(ctx context.Context, tx bun.Tx, taskID string, keys, notes []string, at string) ([]boardMapResult, error) {
	results := make([]boardMapResult, 0, len(keys))
	for index, key := range keys {
		endpoint := newBoardEndpoint(taskID, key, boardSourceModel, at)
		if notes != nil {
			endpoint.Note = notes[index]
		}
		res, err := tx.NewInsert().Model(endpoint).On("CONFLICT (id) DO NOTHING").Exec(ctx)
		if err != nil {
			return nil, err
		}
		affected, _ := res.RowsAffected()
		if affected == 0 && notes != nil && notes[index] != "" {
			if _, err := tx.NewUpdate().Model((*boardEndpointModel)(nil)).Set("note = ?", notes[index]).Where("id = ?", endpoint.ID).Exec(ctx); err != nil {
				return nil, err
			}
		}
		var source string
		if err := tx.NewSelect().Model((*boardEndpointModel)(nil)).Column("source").Where("id = ?", endpoint.ID).Scan(ctx, &source); err != nil {
			return nil, err
		}
		results = append(results, boardMapResult{EndpointKey: key, Created: affected > 0, Source: source})
	}
	return results, nil
}

func (s *taskStore) boardMapEndpoints(ctx context.Context, taskID string, keys, notes []string, now time.Time) ([]boardMapResult, error) {
	var results []boardMapResult
	err := s.boardTx(ctx, taskID, func(ctx context.Context, tx bun.Tx) error {
		var err error
		results, err = boardEnsureEndpoints(ctx, tx, taskID, keys, notes, boardNow(now))
		return err
	})
	return results, err
}

// ---- 证据与候选核对 ----

// boardCheckEvidence：每条须为本任务已发送的 webapi_http 证据。
func (s *taskStore) boardCheckEvidence(ctx context.Context, taskID, path string, ids []string) error {
	for index, id := range ids {
		var sent sql.NullBool
		err := s.orm.NewSelect().Model((*requestModel)(nil)).Column("sent").Where("task_id = ? AND id = ?", taskID, id).Limit(1).Scan(ctx, &sent)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%s[%d]=%q 不是本任务的证据（只能引用本任务 webapi_http 返回的 evidence_id）", path, index, id)
		}
		if err != nil {
			return errors.New("核对证据失败")
		}
		if !sent.Valid || !sent.Bool {
			return fmt.Errorf("%s[%d]=%q 未实际发送（拒绝/待审批/结果未知），不能作为证据", path, index, id)
		}
	}
	return nil
}

func (s *taskStore) boardFindingNumber(ctx context.Context, taskID, findingID string) (string, error) {
	var number int64
	err := s.orm.NewSelect().Model((*findingModel)(nil)).Column("number").Where("task_id = ? AND id = ?", taskID, findingID).Limit(1).Scan(ctx, &number)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return findingNumber(number), nil
}

// ---- 事实 ----

type boardFactInput struct {
	Kind        string   `json:"kind" jsonschema:"required,enum=endpoint,enum=auth,enum=role,enum=tech,enum=config,enum=data,enum=other"`
	Subject     string   `json:"subject" jsonschema:"required,description=对象，≤200字"`
	Statement   string   `json:"statement" jsonschema:"required,description=客观陈述，≤2000字"`
	Confidence  string   `json:"confidence" jsonschema:"required,enum=confirmed,enum=inferred,description=confirmed必须带≥1条本任务已发送evidence_id"`
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
}

type boardFactWrite struct {
	Added            []string `json:"added"`
	Retracted        []string `json:"retracted"`
	AlreadyRetracted []string `json:"already_retracted"`
}

func boardFactID(taskID, sessionID, turnID, callID string, index int) string {
	return "bf_" + namespacedUUID(boardFactNamespace, taskID, sessionID, turnID, callID, strconv.Itoa(index))
}

// boardWriteFacts 在一个事务内新增与撤回；同一 call 重放时按确定性 id 不重复新增。
func (s *taskStore) boardWriteFacts(ctx context.Context, taskID, sessionID, turnID, callID string, add []boardFactInput, retract []string, now time.Time) (*boardFactWrite, error) {
	at := boardNow(now)
	out := &boardFactWrite{Added: []string{}, Retracted: []string{}, AlreadyRetracted: []string{}}
	err := s.boardTx(ctx, taskID, func(ctx context.Context, tx bun.Tx) error {
		for index, item := range add {
			model := boardFactModel{ID: boardFactID(taskID, sessionID, turnID, callID, index), TaskID: taskID, Kind: item.Kind, Subject: item.Subject,
				Statement: item.Statement, Confidence: item.Confidence, EvidenceIDsJSON: marshalStrings(item.EvidenceIDs), Status: factActive,
				SessionID: sessionID, TurnID: turnID, CallID: callID, CreatedAt: at, UpdatedAt: at}
			if _, err := tx.NewInsert().Model(&model).On("CONFLICT (id) DO NOTHING").Exec(ctx); err != nil {
				return err
			}
			out.Added = append(out.Added, model.ID)
		}
		for index, id := range retract {
			var status string
			err := tx.NewSelect().Model((*boardFactModel)(nil)).Column("status").Where("task_id = ? AND id = ?", taskID, id).Limit(1).Scan(ctx, &status)
			if errors.Is(err, sql.ErrNoRows) {
				return boardInputError{fmt.Sprintf("retract[%d]=%q 不是本任务的事实 id", index, id)}
			}
			if err != nil {
				return err
			}
			if status == factRetracted {
				out.AlreadyRetracted = append(out.AlreadyRetracted, id)
				continue
			}
			if _, err := tx.NewUpdate().Model((*boardFactModel)(nil)).Set("status = ?", factRetracted).Set("updated_at = ?", at).Where("id = ?", id).Exec(ctx); err != nil {
				return err
			}
			out.Retracted = append(out.Retracted, id)
		}
		return nil
	})
	return out, err
}

// boardInputError 是可直接返回给模型/用户的输入错误。
type boardInputError struct{ message string }

func (e boardInputError) Error() string { return e.message }

type boardFactFilter struct {
	kind, status     string
	afterAt, afterID string
	limit            int
}

func (s *taskStore) boardFacts(ctx context.Context, taskID string, filter boardFactFilter) ([]BoardFact, error) {
	var models []boardFactModel
	query := s.orm.NewSelect().Model(&models).Where("task_id = ?", taskID)
	if filter.kind != "" {
		query = query.Where("kind = ?", filter.kind)
	}
	if filter.status != "" {
		query = query.Where("status = ?", filter.status)
	}
	if filter.afterAt != "" {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", filter.afterAt, filter.afterAt, filter.afterID)
	}
	if err := query.Order("created_at DESC", "id DESC").Limit(int64(filter.limit)).Scan(ctx); err != nil {
		return nil, err
	}
	out := make([]BoardFact, 0, len(models))
	for _, model := range models {
		out = append(out, BoardFact{ID: model.ID, Kind: model.Kind, Subject: model.Subject, Statement: model.Statement, Confidence: model.Confidence,
			EvidenceIDs: decodeStrings(model.EvidenceIDsJSON), Status: model.Status, SessionID: model.SessionID, TurnID: model.TurnID, CallID: model.CallID,
			CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt})
	}
	return out, nil
}

// ---- 端点列表 ----

type boardEndpointFilter struct {
	needle   string
	untested bool
	afterKey string
	limit    int
}

const boardUntestedClause = `NOT EXISTS (SELECT 1 FROM webapi_board_oracles AS o, json_each(o.endpoint_keys_json) AS j WHERE o.task_id = webapi_board_endpoint.task_id AND j.value = webapi_board_endpoint.endpoint_key)`

func (s *taskStore) boardEndpoints(ctx context.Context, taskID string, filter boardEndpointFilter) ([]BoardEndpoint, error) {
	var models []boardEndpointModel
	query := s.orm.NewSelect().Model(&models).Where("task_id = ?", taskID)
	if filter.needle != "" {
		query = query.Where("(instr(lower(endpoint_key), ?) > 0 OR instr(lower(note), ?) > 0)", filter.needle, filter.needle)
	}
	if filter.untested {
		query = query.Where(boardUntestedClause)
	}
	if filter.afterKey != "" {
		query = query.Where("endpoint_key > ?", filter.afterKey)
	}
	if err := query.Order("endpoint_key ASC").Limit(int64(filter.limit)).Scan(ctx); err != nil {
		return nil, err
	}
	refs, err := s.boardEndpointOracleRefs(ctx, taskID)
	if err != nil {
		return nil, err
	}
	out := make([]BoardEndpoint, 0, len(models))
	for _, model := range models {
		out = append(out, boardEndpointView(model, refs[model.EndpointKey]))
	}
	return out, nil
}

func boardEndpointView(model boardEndpointModel, oracles []string) BoardEndpoint {
	if oracles == nil {
		oracles = []string{}
	}
	return BoardEndpoint{ID: model.ID, EndpointKey: model.EndpointKey, Method: model.Method, Origin: model.Origin, Path: model.Path,
		Params: decodeStrings(model.ParamsJSON), StatusCodes: decodeInts(model.StatusCodesJSON), AuthSeen: model.AuthSeen, RequestCount: model.RequestCount,
		FirstSeenAt: model.FirstSeenAt, LastSeenAt: model.LastSeenAt, Source: model.Source, Note: model.Note, Oracles: oracles}
}

// boardEndpointOracleRefs：端点键 → 引用它的 Oracle 编号（任意状态，按编号）。
func (s *taskStore) boardEndpointOracleRefs(ctx context.Context, taskID string) (map[string][]string, error) {
	var oracles []boardOracleModel
	if err := s.orm.NewSelect().Model(&oracles).Column("number", "endpoint_keys_json").Where("task_id = ?", taskID).Order("number ASC").Scan(ctx); err != nil {
		return nil, err
	}
	refs := map[string][]string{}
	for _, oracle := range oracles {
		for _, key := range decodeStrings(oracle.EndpointKeysJSON) {
			refs[key] = append(refs[key], oracleNumber(oracle.Number))
		}
	}
	return refs, nil
}

// ---- Oracle ----

type boardOracleInput struct {
	Question     string   `json:"question" jsonschema:"required,description=具体可证伪的问题，≤500字；例：用户A的token能否读取用户B的订单 GET /api/orders/{id}"`
	Category     string   `json:"category" jsonschema:"required,enum=authz,enum=authn,enum=injection,enum=ssrf,enum=file,enum=logic,enum=info_disclosure,enum=config,enum=other"`
	EndpointKeys []string `json:"endpoint_keys" jsonschema:"description=0..20个端点：地图中的键或METHOD URL"`
	Priority     string   `json:"priority" jsonschema:"required,enum=high,enum=medium,enum=low"`
}

// boardOracleProposal 是校验并归一后的提出项。
type boardOracleProposal struct {
	question, category, priority string
	keys                         []string
}

type boardProposeResult struct {
	Number  string `json:"number"`
	Created bool   `json:"created"`
	Status  string `json:"status"`
	ID      string `json:"id"`
}

// validateOracleInput 逐字段校验并归一端点（去重排序）。
func validateOracleInput(path string, item boardOracleInput) (boardOracleProposal, error) {
	if err := boardTextError(path+".question", item.Question, 500, true); err != nil {
		return boardOracleProposal{}, err
	}
	if !slices.Contains(boardOracleCategory, item.Category) {
		return boardOracleProposal{}, fmt.Errorf("%s.category 仅支持 %s", path, strings.Join(boardOracleCategory, "/"))
	}
	if !slices.Contains(boardOraclePriority, item.Priority) {
		return boardOracleProposal{}, fmt.Errorf("%s.priority 仅支持 high/medium/low", path)
	}
	if len(item.EndpointKeys) > 20 {
		return boardOracleProposal{}, fmt.Errorf("%s.endpoint_keys 数量过多：实际 %d，上限 20", path, len(item.EndpointKeys))
	}
	keys := make([]string, 0, len(item.EndpointKeys))
	for index, raw := range item.EndpointKeys {
		key, err := normalizeEndpointRef(raw)
		if err != nil {
			return boardOracleProposal{}, fmt.Errorf("%s.endpoint_keys[%d] %v", path, index, err)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	keys = slices.Compact(keys)
	return boardOracleProposal{question: strings.TrimSpace(item.Question), category: item.Category, priority: item.Priority, keys: keys}, nil
}

func boardTextError(field, value string, maximum int, required bool) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s 必须是有效UTF8", field)
	}
	if strings.ContainsRune(value, 0) {
		return fmt.Errorf("%s 不能包含NUL", field)
	}
	if count := utf8.RuneCountInString(value); count > maximum {
		return fmt.Errorf("%s 超长：实际 %d 字，上限 %d 字", field, count, maximum)
	}
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s 必填为空", field)
	}
	return nil
}

// boardProposeOracles：同任务同 category + 同端点集合 + 规范化问题已存在时返回已有项（任意状态）。
func (s *taskStore) boardProposeOracles(ctx context.Context, taskID, origin, sessionID string, items []boardOracleProposal, now time.Time) ([]boardProposeResult, error) {
	at := boardNow(now)
	results := make([]boardProposeResult, 0, len(items))
	err := s.boardTx(ctx, taskID, func(ctx context.Context, tx bun.Tx) error {
		for _, item := range items {
			if _, err := boardEnsureEndpoints(ctx, tx, taskID, item.keys, nil, at); err != nil {
				return err
			}
			dedupe := oracleDedupeKey(item.category, item.keys, item.question)
			var existing boardOracleModel
			err := tx.NewSelect().Model(&existing).Where("task_id = ? AND dedupe_key = ?", taskID, dedupe).Limit(1).Scan(ctx)
			if err == nil {
				results = append(results, boardProposeResult{Number: oracleNumber(existing.Number), Status: existing.Status, ID: existing.ID})
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			var maxNumber sql.NullInt64
			if err := tx.NewSelect().Model((*boardOracleModel)(nil)).ColumnExpr("MAX(number)").Where("task_id = ?", taskID).Scan(ctx, &maxNumber); err != nil {
				return err
			}
			model := boardOracleModel{ID: newBoardOracleID(), TaskID: taskID, Number: maxNumber.Int64 + 1, Question: item.question, Category: item.category,
				EndpointKeysJSON: marshalStrings(item.keys), Priority: item.priority, Status: oracleOpen, Origin: origin, EvidenceIDsJSON: "[]",
				DedupeKey: dedupe, CreatedAt: at, UpdatedAt: at}
			if _, err := tx.NewInsert().Model(&model).Exec(ctx); err != nil {
				return err
			}
			if err := insertOracleEvent(ctx, tx, model, "", oracleOpen, origin, sessionID, at, "提出"); err != nil {
				return err
			}
			results = append(results, boardProposeResult{Number: oracleNumber(model.Number), Created: true, Status: oracleOpen, ID: model.ID})
		}
		return nil
	})
	return results, err
}

func insertOracleEvent(ctx context.Context, tx bun.Tx, oracle boardOracleModel, from, to, actor, sessionID, at, summary string) error {
	event := boardOracleEventModel{ID: newBoardEventID(), OracleID: oracle.ID, TaskID: oracle.TaskID, FromStatus: from, ToStatus: to, Actor: actor,
		SessionID: sessionID, At: at, Summary: capRunes(summary, 500)}
	_, err := tx.NewInsert().Model(&event).Exec(ctx)
	return err
}

// boardOracleChange 描述一次 Oracle 改动；apply 在事务内基于当前行校验并修改，返回事件摘要（空=无状态变化）。
type boardOracleChange struct {
	actor, sessionID string
	apply            func(model *boardOracleModel) (summary string, err error)
}

type boardOracleUpdate struct {
	before, after boardOracleModel
	changed       bool
}

func (s *taskStore) boardChangeOracle(ctx context.Context, taskID string, number int64, change boardOracleChange, now time.Time) (*boardOracleUpdate, error) {
	at := boardNow(now)
	var out boardOracleUpdate
	err := s.boardTx(ctx, taskID, func(ctx context.Context, tx bun.Tx) error {
		var model boardOracleModel
		err := tx.NewSelect().Model(&model).Where("task_id = ? AND number = ?", taskID, number).Limit(1).Scan(ctx)
		if errors.Is(err, sql.ErrNoRows) {
			return errBoardOracleAbsent
		}
		if err != nil {
			return err
		}
		out.before = model
		summary, err := change.apply(&model)
		if err != nil {
			return err
		}
		out.after = model
		if model == out.before {
			return nil
		}
		out.changed = true
		model.UpdatedAt = at
		out.after = model
		if _, err := tx.NewUpdate().Model(&model).WherePK().Exec(ctx); err != nil {
			return err
		}
		if model.Status != out.before.Status {
			return insertOracleEvent(ctx, tx, model, out.before.Status, model.Status, change.actor, change.sessionID, at, summary)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

type boardOracleFilter struct {
	status, category string
	afterNumber      int64
	limit            int
	modelOrder       bool // 工具：进行中/待办在前、按优先级；HTTP：按编号
}

func (s *taskStore) boardOracles(ctx context.Context, taskID string, filter boardOracleFilter, withEvents bool) ([]BoardOracle, error) {
	var models []boardOracleModel
	query := s.orm.NewSelect().Model(&models).Where("task_id = ?", taskID)
	if filter.status != "" {
		query = query.Where("status = ?", filter.status)
	}
	if filter.category != "" {
		query = query.Where("category = ?", filter.category)
	}
	if filter.afterNumber > 0 {
		query = query.Where("number > ?", filter.afterNumber)
	}
	if filter.modelOrder {
		query = query.OrderExpr("CASE status WHEN 'in_progress' THEN 0 WHEN 'open' THEN 1 WHEN 'blocked' THEN 2 WHEN 'holds' THEN 3 WHEN 'refuted' THEN 4 ELSE 5 END").
			OrderExpr("CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END")
	}
	if err := query.Order("number ASC").Limit(int64(filter.limit)).Scan(ctx); err != nil {
		return nil, err
	}
	return s.boardOracleViews(ctx, taskID, models, withEvents)
}

func (s *taskStore) boardOracleViews(ctx context.Context, taskID string, models []boardOracleModel, withEvents bool) ([]BoardOracle, error) {
	out := make([]BoardOracle, 0, len(models))
	ids := make([]string, 0, len(models))
	for _, model := range models {
		view := BoardOracle{ID: model.ID, Number: oracleNumber(model.Number), Question: model.Question, Category: model.Category,
			EndpointKeys: decodeStrings(model.EndpointKeysJSON), Priority: model.Priority, Status: model.Status, Origin: model.Origin,
			ClaimedSessionID: model.ClaimedSessionID, ResultSummary: model.ResultSummary, EvidenceIDs: decodeStrings(model.EvidenceIDsJSON),
			FindingID: model.FindingID, CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt}
		if model.FindingID != "" {
			number, err := s.boardFindingNumber(ctx, taskID, model.FindingID)
			if err != nil {
				return nil, err
			}
			view.FindingNumber = number
		}
		if withEvents {
			view.Events = []BoardOracleEvent{}
		}
		out = append(out, view)
		ids = append(ids, model.ID)
	}
	if !withEvents || len(ids) == 0 {
		return out, nil
	}
	var events []boardOracleEventModel
	if err := s.orm.NewSelect().Model(&events).Where("oracle_id IN (?)", bun.In(ids)).OrderExpr("at ASC, rowid ASC").Scan(ctx); err != nil {
		return nil, err
	}
	index := map[string]int{}
	for position, id := range ids {
		index[id] = position
	}
	for _, event := range events {
		position := index[event.OracleID]
		out[position].Events = append(out[position].Events, BoardOracleEvent{From: event.FromStatus, To: event.ToStatus, Actor: event.Actor,
			SessionID: event.SessionID, At: event.At, Summary: event.Summary})
	}
	return out, nil
}

func (s *taskStore) boardOracle(ctx context.Context, taskID string, number int64) (*BoardOracle, error) {
	var models []boardOracleModel
	if err := s.orm.NewSelect().Model(&models).Where("task_id = ? AND number = ?", taskID, number).Limit(1).Scan(ctx); err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, nil
	}
	views, err := s.boardOracleViews(ctx, taskID, models, true)
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

// ---- 计数与摘要 ----

func (s *taskStore) boardCounts(ctx context.Context, taskID string) (*BoardCounts, error) {
	counts := &BoardCounts{Oracles: map[string]int64{}}
	for _, status := range boardOracleStatuses {
		counts.Oracles[status] = 0
	}
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(source = 'traffic'), 0), COALESCE(SUM(source = 'model'), 0),
		COALESCE(SUM(`+strings.ReplaceAll(boardUntestedClause, "webapi_board_endpoint.", "e.")+`), 0)
		FROM webapi_board_endpoints AS e WHERE e.task_id = ?`, taskID)
	if err := row.Scan(&counts.Endpoints, &counts.TrafficEndpoints, &counts.ModelEndpoints, &counts.UntestedEndpoints); err != nil {
		return nil, err
	}
	row = s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(status = 'active'), 0), COALESCE(SUM(status = 'active' AND confidence = 'confirmed'), 0) FROM webapi_board_facts WHERE task_id = ?`, taskID)
	if err := row.Scan(&counts.Facts, &counts.ConfirmedFacts); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT status, COUNT(*) FROM webapi_board_oracles WHERE task_id = ? GROUP BY status`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		counts.Oracles[status] = count
	}
	return counts, rows.Err()
}

func boardEndpointBrief(endpoint BoardEndpoint) string {
	if endpoint.RequestCount == 0 {
		return endpoint.EndpointKey + "（未请求，模型补充）"
	}
	codes := make([]string, 0, len(endpoint.StatusCodes))
	for _, code := range endpoint.StatusCodes {
		codes = append(codes, strconv.Itoa(code))
	}
	return fmt.Sprintf("%s（请求 %d 次；状态 %s）", endpoint.EndpointKey, endpoint.RequestCount, strings.Join(codes, ","))
}

func boardKeysBrief(keys []string, limit int) string {
	if len(keys) == 0 {
		return "无端点"
	}
	if len(keys) <= limit {
		return strings.Join(keys, "；")
	}
	return strings.Join(keys[:limit], "；") + fmt.Sprintf("；+%d", len(keys)-limit)
}

const boardDigestTruncated = "…（摘要已截断，用 webapi_board_read 分区查看）"

// boardDigest 生成黑板紧凑摘要（≤maxChars 字，按行截断）：先摄取最新流量。M3 Goal 评估器复用，签名稳定。
func (s *taskStore) boardDigest(ctx context.Context, taskID string, maxChars int) (string, error) {
	if maxChars <= 0 {
		maxChars = boardDigestDefaultChars
	}
	if err := s.boardIngest(ctx, taskID, time.Now()); err != nil {
		return "", err
	}
	counts, err := s.boardCounts(ctx, taskID)
	if err != nil {
		return "", err
	}
	lines := []string{fmt.Sprintf("地图：%d 个端点（流量 %d / 模型补充 %d），其中 %d 个尚无任何 Oracle 引用", counts.Endpoints, counts.TrafficEndpoints, counts.ModelEndpoints, counts.UntestedEndpoints)}
	if counts.UntestedEndpoints > 0 {
		var models []boardEndpointModel
		if err := s.orm.NewSelect().Model(&models).Where("task_id = ?", taskID).Where(boardUntestedClause).
			OrderExpr("request_count = 0 ASC, endpoint_key ASC").Limit(15).Scan(ctx); err != nil {
			return "", err
		}
		lines = append(lines, "未被 Oracle 引用的端点（前 15）：")
		for _, model := range models {
			lines = append(lines, "- "+boardEndpointBrief(boardEndpointView(model, nil)))
		}
	}
	oracleCounts := make([]string, 0, len(boardOracleStatuses))
	for _, status := range boardOracleStatuses {
		oracleCounts = append(oracleCounts, fmt.Sprintf("%s %d", status, counts.Oracles[status]))
	}
	lines = append(lines, "Oracle："+strings.Join(oracleCounts, " / "))
	if counts.Oracles[oracleOpen]+counts.Oracles[oracleInProgress] > 0 {
		var models []boardOracleModel
		if err := s.orm.NewSelect().Model(&models).Where("task_id = ? AND status IN (?, ?)", taskID, oracleOpen, oracleInProgress).
			OrderExpr("CASE priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END, number ASC").Limit(10).Scan(ctx); err != nil {
			return "", err
		}
		lines = append(lines, "待证明（open/in_progress 按优先级，前 10）：")
		for _, model := range models {
			lines = append(lines, fmt.Sprintf("- %s [%s·%s·%s] %s → %s", oracleNumber(model.Number), model.Priority, model.Status, model.Category,
				capRunes(oneLine(model.Question), 160), boardKeysBrief(decodeStrings(model.EndpointKeysJSON), 3)))
		}
	}
	if counts.Oracles[oracleHolds]+counts.Oracles[oracleRefuted] > 0 {
		var models []boardOracleModel
		if err := s.orm.NewSelect().Model(&models).Where("task_id = ? AND status IN (?, ?)", taskID, oracleHolds, oracleRefuted).
			Order("updated_at DESC", "number DESC").Limit(5).Scan(ctx); err != nil {
			return "", err
		}
		views, err := s.boardOracleViews(ctx, taskID, models, false)
		if err != nil {
			return "", err
		}
		lines = append(lines, "最近结论（前 5）：")
		for _, view := range views {
			verdict := "不成立"
			if view.Status == oracleHolds {
				verdict = "成立（候选 " + view.FindingNumber + "，待独立验证）"
			}
			lines = append(lines, fmt.Sprintf("- %s %s：%s｜%s", view.Number, verdict, capRunes(oneLine(view.Question), 100), capRunes(oneLine(view.ResultSummary), 160)))
		}
	}
	lines = append(lines, fmt.Sprintf("事实：%d 条有效（confirmed %d / inferred %d）", counts.Facts, counts.ConfirmedFacts, counts.Facts-counts.ConfirmedFacts))
	if counts.ConfirmedFacts > 0 {
		var models []boardFactModel
		if err := s.orm.NewSelect().Model(&models).Where("task_id = ? AND status = ? AND confidence = ?", taskID, factActive, factConfirmed).
			Order("created_at DESC", "id DESC").Limit(10).Scan(ctx); err != nil {
			return "", err
		}
		lines = append(lines, "已确认事实（最近 10）：")
		for _, model := range models {
			lines = append(lines, fmt.Sprintf("- [%s] %s：%s", model.Kind, capRunes(oneLine(model.Subject), 60), capRunes(oneLine(model.Statement), 200)))
		}
	}
	return truncateDigest(lines, maxChars), nil
}

// truncateDigest 按整行保留，超出时以截断标记结尾；总长（字）不超过 maxChars。
func truncateDigest(lines []string, maxChars int) string {
	full := strings.Join(lines, "\n")
	if utf8.RuneCountInString(full) <= maxChars {
		return full
	}
	budget := maxChars - utf8.RuneCountInString(boardDigestTruncated) - 1
	kept := []string{}
	used := 0
	for _, line := range lines {
		length := utf8.RuneCountInString(line) + 1
		if used+length > budget {
			break
		}
		kept = append(kept, line)
		used += length
	}
	if budget < 0 {
		return capRunes(full, maxChars)
	}
	return strings.Join(append(kept, boardDigestTruncated), "\n")
}
