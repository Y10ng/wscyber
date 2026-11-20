package webapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"
)

// 资产漏洞库（C3）：把同一资产各轮任务的候选按端点键归并成“漏洞条目”。
// 状态读时由已有验证/复测结果派生，不存储；归并关系可人工修改（link=manual），同步永不覆盖。

// assetVulnModel 是漏洞条目。auto 条目 id = UUIDv5(固定命名空间, asset_id\nendpoint_key)；
// 人工条目 id 由客户端生成，endpoint_key 为空。
type assetVulnModel struct {
	bun.BaseModel `bun:"table:webapi_asset_vulns,alias:webapi_asset_vuln"`

	ID            string `bun:"id,pk"`
	AssetID       string `bun:"asset_id,notnull"`
	EndpointKey   string `bun:"endpoint_key,type:text,notnull"`
	TitleOverride string `bun:"title_override,type:text,notnull"`
	CreatedAt     string `bun:"created_at,notnull"`
	UpdatedAt     string `bun:"updated_at,notnull"`
}

// assetVulnMemberModel：一个候选最多属于一个条目（finding_id 主键）。
type assetVulnMemberModel struct {
	bun.BaseModel `bun:"table:webapi_asset_vuln_members,alias:webapi_asset_vuln_member"`

	FindingID string `bun:"finding_id,pk"`
	VulnID    string `bun:"vuln_id,notnull"`
	AssetID   string `bun:"asset_id,notnull"`
	TaskID    string `bun:"task_id,notnull"`
	Link      string `bun:"link,notnull"`
	CreatedAt string `bun:"created_at,notnull"`
	UpdatedAt string `bun:"updated_at,notnull"`
}

const (
	VulnLinkAuto   = "auto"
	VulnLinkManual = "manual"

	VulnNew         = "new"
	VulnPresent     = "present"
	VulnPending     = "pending"
	VulnUnconfirmed = "unconfirmed"
	VulnFixed       = "fixed"

	maxVulnTitleRunes = 200
)

// vulnStatusOrder 是列表排序：new, present, pending, unconfirmed, fixed。
var vulnStatusOrder = map[string]int{VulnNew: 0, VulnPresent: 1, VulnPending: 2, VulnUnconfirmed: 3, VulnFixed: 4}

// assetVulnNamespace 是 auto 条目 id 的 UUIDv5 命名空间（固定随机值，与计划任务命名空间不同）。
var assetVulnNamespace = [16]byte{0x8d, 0x21, 0x6f, 0x4a, 0xb3, 0x07, 0x4c, 0x52, 0x91, 0xe8, 0x3d, 0x5a, 0x0c, 0x77, 0xf4, 0x19}

func assetVulnID(assetID, endpointKey string) string {
	return namespacedUUID(assetVulnNamespace, assetID, endpointKey)
}

var (
	errVulnNotFound       = errors.New("漏洞条目不存在")
	errVulnFindingMissing = errors.New("候选不存在")
	errVulnFindingAsset   = errors.New("候选不属于该资产的任务")
	errVulnOtherAsset     = errors.New("条目 id 已被其他资产占用")
)

// ---- 端点键 ----

// vulnEndpointKey 取证据快照中第一条 supporting（无则第一条任意关系）的 `METHOD origin/规范化路径`；
// 无证据或 URL 无法解析为 http(s) 时为 `finding:<finding_id>`（自成一条）。
func vulnEndpointKey(findingID string, evidence []FindingEvidenceSnapshot) string {
	var chosen *FindingEvidenceSnapshot
	for index := range evidence {
		if evidence[index].Relation == "supporting" {
			chosen = &evidence[index]
			break
		}
	}
	if chosen == nil && len(evidence) > 0 {
		chosen = &evidence[0]
	}
	if chosen != nil {
		if key, ok := endpointKeyOf(chosen.Method, chosen.URL); ok {
			return key
		}
	}
	return "finding:" + findingID
}

// endpointKeyOf 规范化：去 query/fragment；纯数字、UUID、≥16 位 hex 段替换为 {id}；去尾 /（根路径保留 /）。
func endpointKeyOf(method, raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", false
	}
	origin, err := normalizeOrigin(u.Scheme + "://" + u.Host)
	if err != nil {
		return "", false
	}
	segments := strings.Split(u.Path, "/")
	for index, segment := range segments {
		if isIDSegment(segment) {
			segments[index] = "{id}"
		}
	}
	path := strings.TrimRight(strings.Join(segments, "/"), "/")
	if path == "" {
		path = "/"
	} else if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "-"
	}
	return method + " " + origin + path, true
}

func isIDSegment(segment string) bool {
	if segment == "" {
		return false
	}
	digits, hexChars := true, true
	for _, c := range segment {
		if c < '0' || c > '9' {
			digits = false
		}
		if !isHexRune(c) {
			hexChars = false
		}
	}
	if digits || hexChars && len(segment) >= 16 {
		return true
	}
	if len(segment) != 36 {
		return false
	}
	for index, c := range segment {
		switch index {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !isHexRune(c) {
				return false
			}
		}
	}
	return true
}

func isHexRune(c rune) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// ---- 同步 ----

// lockAssetVulns 按资产串行漏洞库同步与人工操作（同进程）。
func (c *Capability) lockAssetVulns(assetID string) func() {
	value, _ := c.vulnLocks.LoadOrStore(assetID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

type unsyncedFinding struct {
	FindingID, TaskID, PayloadJSON string
}

// syncAssetVulns 把资产关联任务中尚未入库的候选按端点键加入 auto 条目（单事务、幂等）。
// 已入库的成员不再重新计算；调用方须持有 lockAssetVulns。
func (s *taskStore) syncAssetVulns(ctx context.Context, assetID string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT f.id, f.task_id, r.payload_json FROM webapi_findings f
		JOIN webapi_asset_tasks l ON l.task_id = f.task_id
		JOIN webapi_finding_revisions r ON r.id = f.current_revision_id
		WHERE l.asset_id = ? AND NOT EXISTS (SELECT 1 FROM webapi_asset_vuln_members m WHERE m.finding_id = f.id)
		ORDER BY f.number ASC`, assetID)
	if err != nil {
		return err
	}
	pending := []unsyncedFinding{}
	for rows.Next() {
		var item unsyncedFinding
		if err := rows.Scan(&item.FindingID, &item.TaskID, &item.PayloadJSON); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil || len(pending) == 0 {
		return err
	}
	now := nowRFC3339()
	// 事务首条 SQL 即写（INSERT ... DO NOTHING），避免 SQLite 读事务升级竞争；冲突即已入库，不挪动。
	return s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		for _, item := range pending {
			var payload storedFindingPayload
			key := "finding:" + item.FindingID
			if json.Unmarshal([]byte(item.PayloadJSON), &payload) == nil {
				key = vulnEndpointKey(item.FindingID, payload.Evidence)
			}
			vuln := &assetVulnModel{ID: assetVulnID(assetID, key), AssetID: assetID, EndpointKey: key, CreatedAt: now, UpdatedAt: now}
			if _, err := tx.NewInsert().Model(vuln).On("CONFLICT (id) DO NOTHING").Exec(ctx); err != nil {
				return err
			}
			member := &assetVulnMemberModel{FindingID: item.FindingID, VulnID: vuln.ID, AssetID: assetID, TaskID: item.TaskID, Link: VulnLinkAuto, CreatedAt: now, UpdatedAt: now}
			res, err := tx.NewInsert().Model(member).On("CONFLICT (finding_id) DO NOTHING").Exec(ctx)
			if err != nil {
				return err
			}
			if affected, err := res.RowsAffected(); err != nil {
				return err
			} else if affected > 0 {
				if _, err := tx.NewUpdate().Model((*assetVulnModel)(nil)).Set("updated_at = ?", now).Where("id = ?", vuln.ID).Exec(ctx); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// ---- 状态派生与视图 ----

// AssetVulnEvent 是条目最近一条结果事件（验证判读或复测结论，含非决定性）。
type AssetVulnEvent struct {
	Kind      string `json:"kind"`
	Value     string `json:"value"`
	At        string `json:"at"`
	TaskID    string `json:"task_id"`
	FindingID string `json:"finding_id"`
}

type AssetVulnMember struct {
	TaskID           string  `json:"task_id"`
	FindingID        string  `json:"finding_id"`
	Number           string  `json:"number"`
	Title            string  `json:"title"`
	Link             string  `json:"link"`
	LatestVerdict    *string `json:"latest_verdict"`
	LatestRetest     *string `json:"latest_retest"`
	CreatedAt        string  `json:"created_at"`
	InitialSessionID string  `json:"initial_session_id"`
}

type AssetVuln struct {
	ID              string            `json:"id"`
	Code            string            `json:"code"`
	Title           string            `json:"title"`
	Status          string            `json:"status"`
	EndpointKey     string            `json:"endpoint_key"`
	FirstSeenAt     string            `json:"first_seen_at"`
	LastConfirmedAt *string           `json:"last_confirmed_at"`
	FixedAt         *string           `json:"fixed_at"`
	LastEvent       *AssetVulnEvent   `json:"last_event"`
	MemberCount     int               `json:"member_count"`
	TaskCount       int               `json:"task_count"`
	Members         []AssetVulnMember `json:"members"`
	// Triage 是人工处置视图（V1，见 native/TRIAGE.md）；由 HTTP 层填入。
	Triage *TriageView `json:"triage"`
	// SuggestedSeverity 是成员中最新一条带建议等级的 supported 验证给出的 AI 建议等级（K1）；无为 ""。不写处置。
	SuggestedSeverity string `json:"suggested_severity"`

	// 内部：排序时间、复测代表（最新决定性“存在”事件所属候选）与第一条“存在”事件时间（SLA 起算）。
	sortAt         time.Time
	representative *vulnMemberRow
	firstConfirmed time.Time
}

type vulnMemberRow struct {
	VulnID, FindingID, TaskID, Link string
	Number                          int64
	Title, RevisionID, CreatedAt    string
	InitialSessionID                string
}

type vulnEvent struct {
	Kind, Value, At, FindingID string
	// Suggested 是验证结果的 AI 建议等级（K1，仅 supported 可能非空）；复测为空。
	Suggested string
	at        time.Time
}

// parseAt 解析 RFC3339Nano（存储时间去尾零，不能按字符串比较）；失败为零值。
func parseAt(value string) time.Time {
	at, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return at
}

// laterThan：a 是否晚于 b（时间相同按字符串兜底，保证确定性）。
func laterThan(a time.Time, aKey string, b time.Time, bKey string) bool {
	if !a.Equal(b) {
		return a.After(b)
	}
	return aKey > bKey
}

// assetLatestTask 返回资产最近一次任务（webapi_asset_tasks 按 created_at 最新，含手动发起）及各任务关联时间。
func (s *taskStore) assetTaskTimes(ctx context.Context, assetID string) (latest string, times map[string]time.Time, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT task_id, created_at FROM webapi_asset_tasks WHERE asset_id = ?`, assetID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	times = map[string]time.Time{}
	var latestAt time.Time
	for rows.Next() {
		var taskID, createdAt string
		if err := rows.Scan(&taskID, &createdAt); err != nil {
			return "", nil, err
		}
		at := parseAt(createdAt)
		times[taskID] = at
		if latest == "" || laterThan(at, taskID, latestAt, latest) {
			latest, latestAt = taskID, at
		}
	}
	return latest, times, rows.Err()
}

// assetVulns 计算资产全部非空条目的视图（未排序过滤），以及最近一次任务。调用方须已同步。
func (s *taskStore) assetVulns(ctx context.Context, assetID string) (string, []*AssetVuln, error) {
	latestTask, _, err := s.assetTaskTimes(ctx, assetID)
	if err != nil {
		return "", nil, err
	}
	var vulns []assetVulnModel
	if err := s.orm.NewSelect().Model(&vulns).Where("asset_id = ?", assetID).Scan(ctx); err != nil {
		return "", nil, err
	}
	members, err := s.vulnMemberRows(ctx, assetID)
	if err != nil {
		return "", nil, err
	}
	events, err := s.vulnEvents(ctx, assetID)
	if err != nil {
		return "", nil, err
	}
	byVuln := map[string][]vulnMemberRow{}
	for _, member := range members {
		byVuln[member.VulnID] = append(byVuln[member.VulnID], member)
	}
	out := []*AssetVuln{}
	for _, vuln := range vulns {
		if len(byVuln[vuln.ID]) == 0 {
			continue // 移空的 auto 条目保留行但不返回
		}
		out = append(out, buildAssetVuln(vuln, byVuln[vuln.ID], events, latestTask))
	}
	return latestTask, out, nil
}

func (s *taskStore) vulnMemberRows(ctx context.Context, assetID string) ([]vulnMemberRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.vuln_id, m.finding_id, m.task_id, m.link, f.number, r.title, f.current_revision_id, f.created_at, t.initial_session_id
		FROM webapi_asset_vuln_members m
		JOIN webapi_findings f ON f.id = m.finding_id
		JOIN webapi_finding_revisions r ON r.id = f.current_revision_id
		JOIN webapi_tasks t ON t.id = m.task_id
		WHERE m.asset_id = ?`, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []vulnMemberRow{}
	for rows.Next() {
		var row vulnMemberRow
		if err := rows.Scan(&row.VulnID, &row.FindingID, &row.TaskID, &row.Link, &row.Number, &row.Title, &row.RevisionID, &row.CreatedAt, &row.InitialSessionID); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// vulnEvents 返回资产全部成员的验证判读与复测结论（按候选分组），时间为结果保存时间。
func (s *taskStore) vulnEvents(ctx context.Context, assetID string) (map[string][]vulnEvent, error) {
	out := map[string][]vulnEvent{}
	queries := []struct{ kind, sql string }{
		{FollowupVerify, `SELECT v.finding_id, COALESCE(json_extract(r.payload_json, '$.payload.verdict'), ''), r.created_at,
			COALESCE(json_extract(r.payload_json, '$.payload.suggested_severity'), '')
			FROM webapi_verifications v JOIN webapi_verification_results r ON r.verification_id = v.id
			JOIN webapi_asset_vuln_members m ON m.finding_id = v.finding_id WHERE m.asset_id = ?`},
		{FollowupRetest, `SELECT t.finding_id, COALESCE(json_extract(r.payload_json, '$.payload.conclusion'), ''), r.created_at, ''
			FROM webapi_retests t JOIN webapi_retest_results r ON r.retest_id = t.id
			JOIN webapi_asset_vuln_members m ON m.finding_id = t.finding_id WHERE m.asset_id = ?`},
	}
	for _, query := range queries {
		rows, err := s.db.QueryContext(ctx, query.sql, assetID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			event := vulnEvent{Kind: query.kind}
			if err := rows.Scan(&event.FindingID, &event.Value, &event.At, &event.Suggested); err != nil {
				rows.Close()
				return nil, err
			}
			event.at = parseAt(event.At)
			out[event.FindingID] = append(out[event.FindingID], event)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func eventLater(a, b vulnEvent) bool {
	return laterThan(a.at, a.At+"\x00"+a.Kind+"\x00"+a.FindingID, b.at, b.At+"\x00"+b.Kind+"\x00"+b.FindingID)
}

// decisive：验证 supported / 复测 reproducible = 存在；复测 fixed_in_scope = 已修复。
func decisive(event vulnEvent) (present, fixed bool) {
	switch {
	case event.Kind == FollowupVerify && event.Value == "supported":
		return true, false
	case event.Kind == FollowupRetest && event.Value == "reproducible":
		return true, false
	case event.Kind == FollowupRetest && event.Value == "fixed_in_scope":
		return false, true
	}
	return false, false
}

func buildAssetVuln(vuln assetVulnModel, members []vulnMemberRow, events map[string][]vulnEvent, latestTask string) *AssetVuln {
	// 成员按候选创建时间倒序（最新在前）。
	sort.Slice(members, func(i, j int) bool {
		return laterThan(parseAt(members[i].CreatedAt), members[i].CreatedAt+members[i].FindingID, parseAt(members[j].CreatedAt), members[j].CreatedAt+members[j].FindingID)
	})
	view := &AssetVuln{ID: vuln.ID, Code: vulnCode(vuln.ID), EndpointKey: vuln.EndpointKey, MemberCount: len(members), Members: make([]AssetVulnMember, 0, len(members))}
	memberByFinding := map[string]*vulnMemberRow{}
	tasks := map[string]bool{}
	var last, lastDecisive, lastPresent, firstPresent, lastSuggested *vulnEvent
	anyVerify := false
	for index := range members {
		member := &members[index]
		memberByFinding[member.FindingID] = member
		tasks[member.TaskID] = true
		item := AssetVulnMember{TaskID: member.TaskID, FindingID: member.FindingID, Number: findingNumber(member.Number), Title: member.Title, Link: member.Link, CreatedAt: member.CreatedAt, InitialSessionID: member.InitialSessionID}
		var latestVerify, latestRetest *vulnEvent
		for eventIndex := range events[member.FindingID] {
			event := &events[member.FindingID][eventIndex]
			if event.Kind == FollowupVerify {
				anyVerify = true
				if latestVerify == nil || eventLater(*event, *latestVerify) {
					latestVerify = event
				}
			} else if latestRetest == nil || eventLater(*event, *latestRetest) {
				latestRetest = event
			}
			if last == nil || eventLater(*event, *last) {
				last = event
			}
			present, fixed := decisive(*event)
			if (present || fixed) && (lastDecisive == nil || eventLater(*event, *lastDecisive)) {
				lastDecisive = event
			}
			if present && (lastPresent == nil || eventLater(*event, *lastPresent)) {
				lastPresent = event
			}
			if present && (firstPresent == nil || eventLater(*firstPresent, *event)) {
				firstPresent = event
			}
			if event.Kind == FollowupVerify && event.Value == "supported" && event.Suggested != "" && (lastSuggested == nil || eventLater(*event, *lastSuggested)) {
				lastSuggested = event
			}
		}
		if latestVerify != nil {
			item.LatestVerdict = &latestVerify.Value
		}
		if latestRetest != nil {
			item.LatestRetest = &latestRetest.Value
		}
		view.Members = append(view.Members, item)
	}
	view.TaskCount = len(tasks)
	earliest := members[len(members)-1]
	view.FirstSeenAt = earliest.CreatedAt
	view.Title = members[0].Title
	if vuln.TitleOverride != "" {
		view.Title = vuln.TitleOverride
	}
	view.sortAt = parseAt(members[0].CreatedAt)
	if last != nil {
		view.LastEvent = &AssetVulnEvent{Kind: last.Kind, Value: last.Value, At: last.At, TaskID: memberByFinding[last.FindingID].TaskID, FindingID: last.FindingID}
		view.sortAt = last.at
	}
	if lastPresent != nil {
		at := lastPresent.At
		view.LastConfirmedAt = &at
	}
	if firstPresent != nil {
		view.firstConfirmed = firstPresent.at
	}
	if lastSuggested != nil {
		view.SuggestedSeverity = lastSuggested.Suggested
	}
	switch {
	case lastDecisive != nil:
		if _, fixed := decisive(*lastDecisive); fixed {
			at := lastDecisive.At
			view.Status, view.FixedAt = VulnFixed, &at
		} else {
			view.Status = VulnPresent
			if earliest.TaskID == latestTask {
				view.Status = VulnNew
			}
			view.representative = memberByFinding[lastDecisive.FindingID]
		}
	case anyVerify:
		view.Status = VulnUnconfirmed
	default:
		view.Status = VulnPending
	}
	return view
}

// sortAssetVulns：new, present, pending, unconfirmed, fixed；同状态按 last_event_at（无事件用最新成员时间）倒序。
func sortAssetVulns(vulns []*AssetVuln) {
	sort.SliceStable(vulns, func(i, j int) bool {
		a, b := vulns[i], vulns[j]
		if vulnStatusOrder[a.Status] != vulnStatusOrder[b.Status] {
			return vulnStatusOrder[a.Status] < vulnStatusOrder[b.Status]
		}
		if !a.sortAt.Equal(b.sortAt) {
			return a.sortAt.After(b.sortAt)
		}
		return a.ID < b.ID
	})
}

// assetVulnView 返回单个非空条目视图；不存在、移空或属于其他资产返回 nil。
func (s *taskStore) assetVulnView(ctx context.Context, assetID, vulnID string) (*AssetVuln, error) {
	_, vulns, err := s.assetVulns(ctx, assetID)
	if err != nil {
		return nil, err
	}
	for _, vuln := range vulns {
		if vuln.ID == vulnID {
			return vuln, nil
		}
	}
	return nil, nil
}

// ---- 人工操作（调用方须持有 lockAssetVulns 并已同步） ----

func (s *taskStore) setVulnTitle(ctx context.Context, assetID, vulnID, title string) error {
	_, err := s.orm.NewUpdate().Model((*assetVulnModel)(nil)).Set("title_override = ?", title).Set("updated_at = ?", nowRFC3339()).
		Where("id = ? AND asset_id = ?", vulnID, assetID).Exec(ctx)
	return err
}

// deleteEmptyManualVuln：成员被移空的人工条目（endpoint_key 为空）删除（连同其处置记录）；auto 条目保留行与处置。
func deleteEmptyManualVuln(ctx context.Context, tx bun.Tx, vulnID string) error {
	res, err := tx.NewDelete().Model((*assetVulnModel)(nil)).
		Where("id = ? AND endpoint_key = '' AND NOT EXISTS (SELECT 1 FROM webapi_asset_vuln_members m WHERE m.vuln_id = ?)", vulnID, vulnID).Exec(ctx)
	if err != nil {
		return err
	}
	if deleted, err := res.RowsAffected(); err != nil || deleted == 0 {
		return err
	}
	_, err = tx.NewDelete().Model((*vulnTriageModel)(nil)).Where("vuln_key = ?", vulnID).Exec(ctx)
	return err
}

// moveVulnMember 把候选移入 vulnID（不存在则新建人工条目 = 拆分），link=manual。返回是否新建条目。
func (s *taskStore) moveVulnMember(ctx context.Context, assetID, vulnID, findingID string) (bool, error) {
	var taskID string
	err := s.db.QueryRowContext(ctx, `SELECT task_id FROM webapi_findings WHERE id = ?`, findingID).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, errVulnFindingMissing
	}
	if err != nil {
		return false, err
	}
	member, err := findOptional[assetVulnMemberModel](ctx, s.orm, "finding_id = ?", findingID)
	if err != nil {
		return false, err
	}
	// 同步覆盖资产全部关联任务：未入库或入库于其他资产 = 不属于该资产的任务。
	if member == nil || member.AssetID != assetID {
		return false, errVulnFindingAsset
	}
	target, err := findOptional[assetVulnModel](ctx, s.orm, "id = ?", vulnID)
	if err != nil {
		return false, err
	}
	if target != nil && target.AssetID != assetID {
		return false, errVulnOtherAsset
	}
	if member.VulnID == vulnID {
		return false, nil
	}
	now := nowRFC3339()
	created := target == nil
	err = s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if created {
			if _, err := tx.NewInsert().Model(&assetVulnModel{ID: vulnID, AssetID: assetID, CreatedAt: now, UpdatedAt: now}).Exec(ctx); err != nil {
				return err
			}
		}
		if _, err := tx.NewUpdate().Model((*assetVulnMemberModel)(nil)).Set("vuln_id = ?", vulnID).Set("link = ?", VulnLinkManual).Set("updated_at = ?", now).
			Where("finding_id = ?", findingID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*assetVulnModel)(nil)).Set("updated_at = ?", now).Where("id IN (?)", bun.In([]string{vulnID, member.VulnID})).Exec(ctx); err != nil {
			return err
		}
		return deleteEmptyManualVuln(ctx, tx, member.VulnID)
	})
	return created, err
}

// mergeVulns 把 from 条目全部成员移入 into（link=manual）；两者须为本资产非空条目。
func (s *taskStore) mergeVulns(ctx context.Context, assetID, into, from string) error {
	for _, id := range []string{into, from} {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webapi_asset_vuln_members WHERE vuln_id = ? AND asset_id = ?`, id, assetID).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return errVulnNotFound
		}
	}
	now := nowRFC3339()
	return s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewUpdate().Model((*assetVulnMemberModel)(nil)).Set("vuln_id = ?", into).Set("link = ?", VulnLinkManual).Set("updated_at = ?", now).
			Where("vuln_id = ? AND asset_id = ?", from, assetID).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewUpdate().Model((*assetVulnModel)(nil)).Set("updated_at = ?", now).Where("id IN (?)", bun.In([]string{into, from})).Exec(ctx); err != nil {
			return err
		}
		if err := mergeTriage(ctx, tx, into, from); err != nil {
			return err
		}
		return deleteEmptyManualVuln(ctx, tx, from)
	})
}

// ---- 复测按条目去重（C2b 规划用） ----

// vulnRetestCandidates：同步资产漏洞库后，对状态 new/present、处置不是误报（V1）且本次任务没有成员的条目，
// 取代表成员（最新决定性“存在”事件所属候选，须属于本次任务之前关联的任务）按其当前修订复测（按编号）。
func (c *Capability) vulnRetestCandidates(ctx context.Context, assetID, taskID string) ([]followupCandidate, error) {
	unlock := c.lockAssetVulns(assetID)
	defer unlock()
	if err := c.store.syncAssetVulns(ctx, assetID); err != nil {
		return nil, err
	}
	_, taskTimes, err := c.store.assetTaskTimes(ctx, assetID)
	if err != nil {
		return nil, err
	}
	current, ok := taskTimes[taskID]
	if !ok {
		return []followupCandidate{}, nil
	}
	_, vulns, err := c.store.assetVulns(ctx, assetID)
	if err != nil {
		return nil, err
	}
	triage, err := c.store.triageRecords(ctx, assetID)
	if err != nil {
		return nil, err
	}
	type numbered struct {
		number int64
		item   followupCandidate
	}
	picked := []numbered{}
	for _, vuln := range vulns {
		if (vuln.Status != VulnNew && vuln.Status != VulnPresent) || vuln.representative == nil || triage[vuln.ID].Disposition == DispositionFalsePositive {
			continue
		}
		inCurrent := false
		for _, member := range vuln.Members {
			inCurrent = inCurrent || member.TaskID == taskID
		}
		rep := vuln.representative
		repAt, linked := taskTimes[rep.TaskID]
		if inCurrent || !linked || !repAt.Before(current) {
			continue
		}
		picked = append(picked, numbered{rep.Number, followupCandidate{TaskID: rep.TaskID, FindingID: rep.FindingID, RevisionID: rep.RevisionID}})
	}
	sort.Slice(picked, func(i, j int) bool { return picked[i].number < picked[j].number })
	out := make([]followupCandidate, 0, len(picked))
	for _, entry := range picked {
		out = append(out, entry.item)
	}
	return out, nil
}
