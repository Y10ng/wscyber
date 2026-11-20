package webapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

// 资产漏洞库 HTTP（C3）：每个请求先按资产加锁并同步，再读/改。

// vulnRequest 校验资产（存在）与路径 ID，加锁并同步；失败已写响应。返回解锁函数。
func (c *Capability) vulnRequest(w http.ResponseWriter, r *http.Request, keys ...string) (assetID string, ids map[string]string, unlock func(), ok bool) {
	assetID, err := pathID(r, "id", "资产")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", nil, nil, false
	}
	ids = map[string]string{}
	labels := map[string]string{"vid": "漏洞条目", "finding_id": "候选"}
	for _, key := range keys {
		if ids[key], err = pathID(r, key, labels[key]); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return "", nil, nil, false
		}
	}
	asset, err := c.store.assetModel(r.Context(), assetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取资产失败")
		return "", nil, nil, false
	}
	if asset == nil {
		writeError(w, http.StatusNotFound, "资产不存在")
		return "", nil, nil, false
	}
	unlock = c.lockAssetVulns(assetID)
	if err := c.store.syncAssetVulns(r.Context(), assetID); err != nil {
		unlock()
		writeError(w, http.StatusInternalServerError, "同步漏洞库失败")
		return "", nil, nil, false
	}
	return assetID, ids, unlock, true
}

func (c *Capability) writeVuln(w http.ResponseWriter, r *http.Request, assetID, vulnID string, status int) {
	view, err := c.store.assetVulnView(r.Context(), assetID, vulnID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞条目失败")
		return
	}
	if view == nil {
		writeError(w, http.StatusNotFound, errVulnNotFound.Error())
		return
	}
	if err := c.attachAssetTriage(r.Context(), assetID, view); err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞处置失败")
		return
	}
	writeJSON(w, status, view)
}

func (c *Capability) handleVulnList(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("status")
	if _, known := vulnStatusOrder[filter]; filter != "" && !known {
		writeError(w, http.StatusBadRequest, "status 只能是 new、present、pending、unconfirmed、fixed")
		return
	}
	assetID, _, unlock, ok := c.vulnRequest(w, r)
	if !ok {
		return
	}
	defer unlock()
	latestTask, vulns, err := c.store.assetVulns(r.Context(), assetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞库失败")
		return
	}
	sortAssetVulns(vulns)
	if err := c.attachAssetTriage(r.Context(), assetID, vulns...); err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞处置失败")
		return
	}
	counts := map[string]int{VulnNew: 0, VulnPresent: 0, VulnPending: 0, VulnUnconfirmed: 0, VulnFixed: 0}
	items := make([]*AssetVuln, 0, len(vulns))
	for _, vuln := range vulns {
		counts[vuln.Status]++
		if filter == "" || vuln.Status == filter {
			items = append(items, vuln)
		}
	}
	var latest *string
	if latestTask != "" {
		latest = &latestTask
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset_id": assetID, "latest_task_id": latest, "counts": counts, "vulns": items})
}

func (c *Capability) handleVulnGet(w http.ResponseWriter, r *http.Request) {
	assetID, ids, unlock, ok := c.vulnRequest(w, r, "vid")
	if !ok {
		return
	}
	defer unlock()
	c.writeVuln(w, r, assetID, ids["vid"], http.StatusOK)
}

// handleVulnTitle：PUT {title}；"" 清除覆盖。
func (c *Capability) handleVulnTitle(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Title *string `json:"title"`
	}
	if err := decodeJSONBody(r, &body, true, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Title == nil {
		writeError(w, http.StatusBadRequest, "title 必填（\"\" 清除覆盖）")
		return
	}
	title := strings.TrimSpace(*body.Title)
	if utf8.RuneCountInString(title) > maxVulnTitleRunes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("title 不能超过 %d 字", maxVulnTitleRunes))
		return
	}
	assetID, ids, unlock, ok := c.vulnRequest(w, r, "vid")
	if !ok {
		return
	}
	defer unlock()
	view, err := c.store.assetVulnView(r.Context(), assetID, ids["vid"])
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取漏洞条目失败")
		return
	}
	if view == nil {
		writeError(w, http.StatusNotFound, errVulnNotFound.Error())
		return
	}
	if err := c.store.setVulnTitle(r.Context(), assetID, ids["vid"], title); err != nil {
		writeError(w, http.StatusInternalServerError, "保存漏洞条目失败")
		return
	}
	c.auditVulnRename(r.Context(), assetID, ids["vid"], view.Title)
	c.writeVuln(w, r, assetID, ids["vid"], http.StatusOK)
}

// handleVulnMember：PUT（空体或 {}）把候选移入条目；条目不存在则新建人工条目（201）。
func (c *Capability) handleVulnMember(w http.ResponseWriter, r *http.Request) {
	var body struct{}
	if err := decodeJSONBody(r, &body, true, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	assetID, ids, unlock, ok := c.vulnRequest(w, r, "vid", "finding_id")
	if !ok {
		return
	}
	defer unlock()
	fromVuln := c.vulnMemberBefore(r.Context(), ids["finding_id"])
	created, err := c.store.moveVulnMember(r.Context(), assetID, ids["vid"], ids["finding_id"])
	switch {
	case errors.Is(err, errVulnFindingMissing):
		writeError(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, errVulnFindingAsset), errors.Is(err, errVulnOtherAsset):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "移动候选失败")
		return
	}
	c.auditVulnMove(r.Context(), assetID, ids["vid"], ids["finding_id"], fromVuln, created)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.writeVuln(w, r, assetID, ids["vid"], status)
}

// handleVulnMerge：POST {from} 把 from 条目全部成员并入 vid。
func (c *Capability) handleVulnMerge(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From string `json:"from"`
	}
	if err := decodeJSONBody(r, &body, true, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if validPathID(body.From) != nil {
		writeError(w, http.StatusBadRequest, "from 条目 id 非法")
		return
	}
	if body.From == r.PathValue("vid") {
		writeError(w, http.StatusBadRequest, "from 不能与目标条目相同")
		return
	}
	assetID, ids, unlock, ok := c.vulnRequest(w, r, "vid")
	if !ok {
		return
	}
	defer unlock()
	fromTitle := c.vulnTitle(r.Context(), assetID, body.From)
	err := c.store.mergeVulns(r.Context(), assetID, ids["vid"], body.From)
	if errors.Is(err, errVulnNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "合并漏洞条目失败")
		return
	}
	c.auditVulnMerge(r.Context(), assetID, ids["vid"], body.From, fromTitle)
	c.writeVuln(w, r, assetID, ids["vid"], http.StatusOK)
}
