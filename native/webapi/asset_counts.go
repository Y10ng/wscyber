package webapi

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/uptrace/bun"
)

// AssetListItem 的计数仅用于列表，不在资产详情上返回未经计算的零值。
type AssetListItem struct {
	*Asset
	OverdueVulns int `json:"overdue_vulns"`
}

// assetOverdueCounts 批量读本次列表涉及资产的候选与结果，复用 C3 状态派生及总览
// vulnTodo。尚未同步入库的候选按 C3 的 auto ID 在内存归组，不为只读计数逐资产同步。
// 已有人工归并优先；这与 syncAssetVulns 的“已有成员不挪动”规则相同。
func (c *Capability) assetOverdueCounts(ctx context.Context, assets []*Asset) (map[string]int, error) {
	counts := map[string]int{}
	if len(assets) == 0 {
		return counts, nil
	}
	ids := make([]string, 0, len(assets))
	for _, asset := range assets {
		ids = append(ids, asset.ID)
	}
	triage, err := c.loadTriageContext(ctx)
	if err != nil {
		return nil, err
	}
	members := map[string][]vulnMemberRow{}
	models := map[string]assetVulnModel{}
	err = c.store.queryRows(ctx, func(rows *sql.Rows) error {
		var asset, key, payload string
		var m vulnMemberRow
		if err := rows.Scan(&asset, &m.VulnID, &m.FindingID, &m.TaskID, &m.Link, &m.Number, &m.Title, &m.RevisionID, &m.CreatedAt, &m.InitialSessionID, &payload); err != nil {
			return err
		}
		if m.VulnID == "" {
			var stored storedFindingPayload
			key = "finding:" + m.FindingID
			if json.Unmarshal([]byte(payload), &stored) == nil {
				key = vulnEndpointKey(m.FindingID, stored.Evidence)
			}
			m.VulnID = assetVulnID(asset, key)
			m.Link = VulnLinkAuto
		}
		models[m.VulnID] = assetVulnModel{ID: m.VulnID, AssetID: asset, EndpointKey: key}
		members[m.VulnID] = append(members[m.VulnID], m)
		return nil
	}, `SELECT l.asset_id, COALESCE(m.vuln_id, ''), f.id, f.task_id, COALESCE(m.link, ''), f.number, r.title, f.current_revision_id, f.created_at, t.initial_session_id, r.payload_json
 FROM webapi_asset_tasks l JOIN webapi_findings f ON f.task_id = l.task_id
 JOIN webapi_finding_revisions r ON r.id = f.current_revision_id
 JOIN webapi_tasks t ON t.id = f.task_id
 LEFT JOIN webapi_asset_vuln_members m ON m.finding_id = f.id
 WHERE l.asset_id IN (?)`, bun.In(ids))
	if err != nil {
		return nil, err
	}
	events := map[string][]vulnEvent{}
	err = c.store.queryRows(ctx, func(rows *sql.Rows) error {
		var event vulnEvent
		if err := rows.Scan(&event.Kind, &event.FindingID, &event.Value, &event.At); err != nil {
			return err
		}
		event.at = parseAt(event.At)
		events[event.FindingID] = append(events[event.FindingID], event)
		return nil
	}, `SELECT 'verify', v.finding_id, COALESCE(r.payload_json::jsonb #>> '{payload,verdict}', ''), r.created_at
 FROM webapi_verifications v JOIN webapi_verification_results r ON r.verification_id = v.id
 JOIN webapi_asset_tasks l ON l.task_id = v.task_id WHERE l.asset_id IN (?)
 UNION ALL
 SELECT 'retest', v.finding_id, COALESCE(r.payload_json::jsonb #>> '{payload,conclusion}', ''), r.created_at
 FROM webapi_retests v JOIN webapi_retest_results r ON r.retest_id = v.id
 JOIN webapi_asset_tasks l ON l.task_id = v.task_id WHERE l.asset_id IN (?)`, bun.In(ids), bun.In(ids))
	if err != nil {
		return nil, err
	}
	for id, model := range models {
		// new 与 present 在 vulnTodo 中完全等价；无需为最新任务额外查询。
		view := buildAssetVuln(model, members[id], events, "")
		item := triage.view(id, model.AssetID, view.Status, view.firstConfirmed)
		if _, _, overdue := vulnTodo(view.Status, item); overdue {
			counts[model.AssetID]++
		}
	}
	return counts, nil
}
