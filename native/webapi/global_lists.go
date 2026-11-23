package webapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// 全局漏洞列表与全局报告列表（U2a）：只读。见 native/OVERVIEW.md。

const (
	VulnKindAsset   = "asset_vuln"
	VulnKindFinding = "task_finding"
)

// GlobalVuln 是 GET /api/webapi/vulns 的一项。asset_vuln 来自 C3 资产漏洞库（未归档资产）；
// task_finding 是未关联资产任务里的单个候选（id = finding:<finding_id>）。
type GlobalVuln struct {
	ID              string          `json:"id"`
	Code            string          `json:"code"`
	Kind            string          `json:"kind"`
	Title           string          `json:"title"`
	Status          string          `json:"status"`
	EndpointKey     string          `json:"endpoint_key"`
	Asset           *TaskAssetRef   `json:"asset"`
	TaskID          string          `json:"task_id"`
	FirstSeenAt     string          `json:"first_seen_at"`
	LastConfirmedAt *string         `json:"last_confirmed_at"`
	FixedAt         *string         `json:"fixed_at"`
	LastEvent       *AssetVulnEvent `json:"last_event"`
	MemberCount     int             `json:"member_count"`
	TaskCount       int             `json:"task_count"`
	Triage          *TriageView     `json:"triage"`
	// SuggestedSeverity：AI 建议等级（K1，同 AssetVuln）；无为 ""。
	SuggestedSeverity string `json:"suggested_severity"`

	sortAt time.Time
	// assetID/firstConfirmed 供处置视图计算（firstConfirmed 为条目第一条“存在”事件时间）。
	assetID        string
	firstConfirmed time.Time
	// events 是条目全部成员的验证/复测结果事件（总览按日统计用）；findingTasks 为成员候选所属任务。
	events       []vulnEvent
	findingTasks map[string]string
	// members 是条目成员（候选创建时间倒序），漏洞卡（K1）用。
	members []AssetVulnMember
}

func globalVulnFrom(view *AssetVuln, kind string, asset *TaskAssetRef, events map[string][]vulnEvent) *GlobalVuln {
	item := &GlobalVuln{ID: view.ID, Code: view.Code, SuggestedSeverity: view.SuggestedSeverity, members: view.Members, Kind: kind, Title: view.Title, Status: view.Status, EndpointKey: view.EndpointKey, Asset: asset,
		FirstSeenAt: view.FirstSeenAt, LastConfirmedAt: view.LastConfirmedAt, FixedAt: view.FixedAt, LastEvent: view.LastEvent,
		MemberCount: view.MemberCount, TaskCount: view.TaskCount, sortAt: view.sortAt, findingTasks: map[string]string{},
		firstConfirmed: view.firstConfirmed}
	if asset != nil {
		item.assetID = asset.ID
	}
	if len(view.Members) > 0 {
		item.TaskID = view.Members[0].TaskID // 成员按候选创建时间倒序：最新成员任务
	}
	for _, member := range view.Members {
		item.findingTasks[member.FindingID] = member.TaskID
		item.events = append(item.events, events[member.FindingID]...)
	}
	return item
}

// globalVulns 计算全局漏洞条目（未排序）。assetID 非空时只取该资产（归档资产为空），不含无资产任务候选。
// 每个未归档资产先按 C3 加锁同步，再复用 assetVulns 派生状态。
func (c *Capability) globalVulns(ctx context.Context, assetID string) ([]*GlobalVuln, error) {
	query, args := `SELECT id, name FROM webapi_assets WHERE NOT archived`, []any{}
	if assetID != "" {
		query += ` AND id = ?`
		args = append(args, assetID)
	}
	assets := []TaskAssetRef{}
	if err := c.store.queryRows(ctx, func(rows *sql.Rows) error {
		var ref TaskAssetRef
		if err := rows.Scan(&ref.ID, &ref.Name); err != nil {
			return err
		}
		assets = append(assets, ref)
		return nil
	}, query+` ORDER BY id COLLATE "C"`, args...); err != nil {
		return nil, err
	}
	out := []*GlobalVuln{}
	for index := range assets {
		asset := &assets[index]
		views, events, err := c.syncedAssetVulns(ctx, asset.ID)
		if err != nil {
			return nil, err
		}
		for _, view := range views {
			out = append(out, globalVulnFrom(view, VulnKindAsset, asset, events))
		}
	}
	if assetID == "" {
		standalone, err := c.store.standaloneFindingVulns(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, standalone...)
	}
	return out, nil
}

func (c *Capability) syncedAssetVulns(ctx context.Context, assetID string) ([]*AssetVuln, map[string][]vulnEvent, error) {
	unlock := c.lockAssetVulns(assetID)
	defer unlock()
	if err := c.store.syncAssetVulns(ctx, assetID); err != nil {
		return nil, nil, err
	}
	_, views, err := c.store.assetVulns(ctx, assetID)
	if err != nil {
		return nil, nil, err
	}
	events, err := c.store.vulnEvents(ctx, assetID)
	return views, events, err
}

// standaloneFindingVulns：未关联资产的任务中每个候选单独一条，状态用 C3 的 buildAssetVuln 对单成员计算；
// “最近一次任务”取候选自己的任务（独立任务即其唯一一轮），故存在类结论为 new。
func (s *taskStore) standaloneFindingVulns(ctx context.Context) ([]*GlobalVuln, error) {
	type standalone struct {
		member  vulnMemberRow
		payload string
	}
	found := []standalone{}
	if err := s.queryRows(ctx, func(rows *sql.Rows) error {
		var item standalone
		m := &item.member
		if err := rows.Scan(&m.FindingID, &m.TaskID, &m.Number, &m.Title, &m.RevisionID, &m.CreatedAt, &m.InitialSessionID, &item.payload); err != nil {
			return err
		}
		m.Link = VulnLinkAuto
		found = append(found, item)
		return nil
	}, `SELECT f.id, f.task_id, f.number, r.title, f.current_revision_id, f.created_at, t.initial_session_id, r.payload_json
		FROM webapi_findings f
		JOIN webapi_finding_revisions r ON r.id = f.current_revision_id
		JOIN webapi_tasks t ON t.id = f.task_id
		WHERE NOT EXISTS (SELECT 1 FROM webapi_asset_tasks l WHERE l.task_id = f.task_id)`); err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return []*GlobalVuln{}, nil
	}
	events, err := s.standaloneEvents(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*GlobalVuln, 0, len(found))
	for _, item := range found {
		id := "finding:" + item.member.FindingID
		key := id
		var payload storedFindingPayload
		if json.Unmarshal([]byte(item.payload), &payload) == nil {
			key = vulnEndpointKey(item.member.FindingID, payload.Evidence)
		}
		item.member.VulnID = id
		view := buildAssetVuln(assetVulnModel{ID: id, EndpointKey: key}, []vulnMemberRow{item.member}, events, item.member.TaskID)
		out = append(out, globalVulnFrom(view, VulnKindFinding, nil, events))
	}
	return out, nil
}

// standaloneEvents：无资产任务候选的验证判读与复测结论（同 vulnEvents，时间为结果保存时间）。
func (s *taskStore) standaloneEvents(ctx context.Context) (map[string][]vulnEvent, error) {
	out := map[string][]vulnEvent{}
	queries := []struct{ kind, sql string }{
		{FollowupVerify, `SELECT v.finding_id, COALESCE(r.payload_json::jsonb #>> '{payload,verdict}', ''), r.created_at,
			COALESCE(r.payload_json::jsonb #>> '{payload,suggested_severity}', '')
			FROM webapi_verifications v JOIN webapi_verification_results r ON r.verification_id = v.id
			WHERE NOT EXISTS (SELECT 1 FROM webapi_asset_tasks l WHERE l.task_id = v.task_id)`},
		{FollowupRetest, `SELECT t.finding_id, COALESCE(r.payload_json::jsonb #>> '{payload,conclusion}', ''), r.created_at, ''
			FROM webapi_retests t JOIN webapi_retest_results r ON r.retest_id = t.id
			WHERE NOT EXISTS (SELECT 1 FROM webapi_asset_tasks l WHERE l.task_id = t.task_id)`},
	}
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		for _, query := range queries {
			if err := scanRows(db.QueryContext(ctx, query.sql))(func(rows *sql.Rows) error {
				event := vulnEvent{Kind: query.kind}
				if err := rows.Scan(&event.FindingID, &event.Value, &event.At, &event.Suggested); err != nil {
					return err
				}
				event.at = parseAt(event.At)
				out[event.FindingID] = append(out[event.FindingID], event)
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// sortGlobalVulns 同 C3：new, present, pending, unconfirmed, fixed；同状态按 last_event 时间（无事件用最新成员创建时间）倒序。
func sortGlobalVulns(items []*GlobalVuln) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if vulnStatusOrder[a.Status] != vulnStatusOrder[b.Status] {
			return vulnStatusOrder[a.Status] < vulnStatusOrder[b.Status]
		}
		if !a.sortAt.Equal(b.sortAt) {
			return a.sortAt.After(b.sortAt)
		}
		return a.ID < b.ID
	})
}

func newVulnCounts() map[string]int {
	return map[string]int{VulnNew: 0, VulnPresent: 0, VulnPending: 0, VulnUnconfirmed: 0, VulnFixed: 0}
}

// 偏移游标：base64url("o:<offset>")，内存排序后切片。
func decodeOffsetCursor(encoded string) (int, error) {
	if encoded == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) < 3 || string(decoded[:2]) != "o:" {
		return 0, fmt.Errorf("cursor非法")
	}
	offset, err := strconv.Atoi(string(decoded[2:]))
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("cursor非法")
	}
	return offset, nil
}

func encodeOffsetCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(offset)))
}

// parseTriageFilter 解析逗号分隔的多值筛选（severity/disposition）；空串返回 nil。
func parseTriageFilter(raw, name string, known map[string]bool, allowed string) (map[string]bool, error) {
	if raw == "" {
		return nil, nil
	}
	values := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if !known[value] {
			return nil, fmt.Errorf("%s 只能是 %s", name, allowed)
		}
		values[value] = true
	}
	return values, nil
}

// handleGlobalVulns: GET /api/webapi/vulns?status=&severity=&disposition=&overdue=&asset_id=&q=&limit=&cursor=
func (c *Capability) handleGlobalVulns(w http.ResponseWriter, r *http.Request) {
	limit, needle, assetID, err := listParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query := r.URL.Query()
	status := query.Get("status")
	if _, known := vulnStatusOrder[status]; status != "" && !known {
		writeError(w, http.StatusBadRequest, "status 只能是 new、present、pending、unconfirmed、fixed")
		return
	}
	severities, err := parseTriageFilter(query.Get("severity"), "severity", severityFilterKeys, `critical、high、medium、low、info、none（逗号分隔）`)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	dispositions, err := parseTriageFilter(query.Get("disposition"), "disposition", triageDisposition, `open、fixing、risk_accepted、false_positive（逗号分隔）`)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	overdueOnly := false
	switch query.Get("overdue") {
	case "", "0":
	case "1":
		overdueOnly = true
	default:
		writeError(w, http.StatusBadRequest, "overdue 只能是 1")
		return
	}
	offset, err := decodeOffsetCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	all, err := c.globalVulns(r.Context(), assetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞列表失败")
		return
	}
	triage, err := c.loadTriageContext(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞处置失败")
		return
	}
	for _, item := range all {
		item.Triage = triage.view(item.ID, item.assetID, item.Status, item.firstConfirmed)
	}
	sortGlobalVulns(all)
	counts := newVulnCounts()
	bySeverity := newSeverityCounts()
	overdueCount := 0
	filtered := make([]*GlobalVuln, 0, len(all))
	for _, item := range all {
		if needle != "" && !containsFold(item.Title, needle) && !containsFold(item.EndpointKey, needle) && (item.Asset == nil || !containsFold(item.Asset.Name, needle)) {
			continue
		}
		counts[item.Status]++
		// by_severity 与 overdue 只计“待修复”（new/present 且处置 open/fixing）条目。
		if triageOpen(item.Status, item.Triage.Disposition) {
			bySeverity[severityBucket(item.Triage.Severity)]++
			if item.Triage.Overdue {
				overdueCount++
			}
		}
		if status != "" && item.Status != status {
			continue
		}
		if severities != nil && !severities[severityBucket(item.Triage.Severity)] {
			continue
		}
		if dispositions != nil && !dispositions[item.Triage.Disposition] {
			continue
		}
		if overdueOnly && !item.Triage.Overdue {
			continue
		}
		filtered = append(filtered, item)
	}
	page := []*GlobalVuln{}
	if offset < len(filtered) {
		page = filtered[offset:]
	}
	hasMore := len(page) > limit
	if hasMore {
		page = page[:limit]
	}
	var next any
	if hasMore {
		next = encodeOffsetCursor(offset + limit)
	}
	out := map[string]any{"overdue": overdueCount, "by_severity": bySeverity}
	for key, value := range counts {
		out[key] = value
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": page, "has_more": hasMore, "next_cursor": next, "counts": out})
}

// ---- 全局报告列表 ----

// GlobalReport 是 GET /api/webapi/report-list 的一项；updated_at = 草稿保存时间（无草稿为创建时间）。
type GlobalReport struct {
	ReportID  string        `json:"report_id"`
	TaskID    string        `json:"task_id"`
	TaskTitle string        `json:"task_title"`
	Asset     *TaskAssetRef `json:"asset"`
	SessionID string        `json:"session_id"`
	Title     string        `json:"title"`
	HasDraft  bool          `json:"has_draft"`
	ItemCount int           `json:"item_count"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`

	objective string
	createdAt time.Time
}

func (s *taskStore) globalReports(ctx context.Context, assetID string) ([]*GlobalReport, error) {
	query := `SELECT r.id, r.task_id, t.objective, COALESCE(a.id, ''), COALESCE(a.name, ''), r.session_id,
		COALESCE(jsonb_array_length(r.items_json::jsonb), 0), r.created_at, d.report_id IS NOT NULL,
		COALESCE(d.payload_json::jsonb #>> '{payload,title}', ''), COALESCE(d.created_at, '')
		FROM webapi_reports r
		JOIN webapi_tasks t ON t.id = r.task_id
		LEFT JOIN webapi_asset_tasks l ON l.task_id = r.task_id
		LEFT JOIN webapi_assets a ON a.id = l.asset_id
		LEFT JOIN webapi_report_drafts d ON d.report_id = r.id`
	args := []any{}
	if assetID != "" {
		query += ` WHERE l.asset_id = ?`
		args = append(args, assetID)
	}
	out := []*GlobalReport{}
	err := s.queryRows(ctx, func(rows *sql.Rows) error {
		item := &GlobalReport{}
		var assetRefID, assetName, draftAt string
		if err := rows.Scan(&item.ReportID, &item.TaskID, &item.objective, &assetRefID, &assetName, &item.SessionID,
			&item.ItemCount, &item.CreatedAt, &item.HasDraft, &item.Title, &draftAt); err != nil {
			return err
		}
		item.TaskTitle = usageTitle(item.objective)
		if assetRefID != "" {
			item.Asset = &TaskAssetRef{ID: assetRefID, Name: assetName}
		}
		item.UpdatedAt = item.CreatedAt
		if draftAt != "" {
			item.UpdatedAt = draftAt
		}
		item.createdAt = parseAt(item.CreatedAt)
		out = append(out, item)
		return nil
	}, query, args...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// handleGlobalReportList: GET /api/webapi/report-list?q=&asset_id=&limit=&cursor=，按 created_at 倒序。
func (c *Capability) handleGlobalReportList(w http.ResponseWriter, r *http.Request) {
	limit, needle, assetID, err := listParams(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	afterAt, afterID, hasCursor, err := decodeKeyCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	all, err := c.store.globalReports(r.Context(), assetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取报告列表失败")
		return
	}
	filtered := make([]*GlobalReport, 0, len(all))
	for _, item := range all {
		if needle == "" || containsFold(item.Title, needle) || containsFold(item.objective, needle) || item.Asset != nil && containsFold(item.Asset.Name, needle) {
			filtered = append(filtered, item)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return createdDescLess(filtered[i].createdAt, filtered[i].ReportID, filtered[j].createdAt, filtered[j].ReportID)
	})
	start := 0
	if hasCursor {
		start = sort.Search(len(filtered), func(i int) bool {
			return createdDescLess(afterAt, afterID, filtered[i].createdAt, filtered[i].ReportID)
		})
	}
	page := filtered[start:]
	hasMore := len(page) > limit
	if hasMore {
		page = page[:limit]
	}
	var next any
	if hasMore {
		last := page[len(page)-1]
		next = encodeKeyCursor(last.createdAt.UTC().Format(time.RFC3339Nano), last.ReportID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": page, "has_more": hasMore, "next_cursor": next, "total": len(filtered)})
}
