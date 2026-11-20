package webapi

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"time"

	"github.com/chainreactors/cyber/agent"
)

// saveSchedule 是 PUT /assets/{id}/schedule 请求体；enabled、auto_verify、auto_retest 省略视为 true。
type saveSchedule struct {
	Cron       string `json:"cron"`
	Enabled    *bool  `json:"enabled"`
	AutoVerify *bool  `json:"auto_verify"`
	AutoRetest *bool  `json:"auto_retest"`
}

// ScheduleRun 是运行记录视图；task_id 只有已发起或已建任务时非空。
type ScheduleRun struct {
	ID           string `json:"id"`
	AssetID      string `json:"asset_id"`
	ScheduledFor string `json:"scheduled_for"`
	TaskID       string `json:"task_id"`
	Status       string `json:"status"`
	Error        string `json:"error"`
	CreatedAt    string `json:"created_at"`
	// Followups 只对 launched 运行非空。
	Followups *ScheduleFollowupSummary `json:"followups"`
}

// AssetSchedule 是计划视图。时间均为 UTC RFC3339；cron 按 timezone（Hub 本地时区）解释。
type AssetSchedule struct {
	AssetID string `json:"asset_id"`
	Cron    string `json:"cron"`
	Enabled bool   `json:"enabled"`
	// AutoVerify/AutoRetest：研究结束后自动验证本次候选 / 自动复测已知漏洞。
	AutoVerify bool         `json:"auto_verify"`
	AutoRetest bool         `json:"auto_retest"`
	NextRunAt  *string      `json:"next_run_at"`
	Upcoming   []string     `json:"upcoming"`
	Timezone   string       `json:"timezone"`
	UpdatedAt  string       `json:"updated_at"`
	LastRun    *ScheduleRun `json:"last_run"`
}

func runView(model scheduleRunModel) ScheduleRun {
	return ScheduleRun{ID: model.ID, AssetID: model.AssetID, ScheduledFor: model.ScheduledFor, TaskID: model.TaskID, Status: model.Status, Error: model.Error, CreatedAt: model.CreatedAt}
}

// timezoneLabel 给出时区名与当前偏移，如 "Local (+08:00)"。
func (c *Capability) timezoneLabel() string {
	loc := c.scheduleLocation()
	return loc.String() + " (" + c.scheduleClock().In(loc).Format("-07:00") + ")"
}

func (c *Capability) scheduleView(model *assetScheduleModel, last *ScheduleRun, autoVerify, autoRetest bool) AssetSchedule {
	view := AssetSchedule{AssetID: model.AssetID, Cron: model.Cron, Enabled: model.Enabled, AutoVerify: autoVerify, AutoRetest: autoRetest, Upcoming: []string{}, Timezone: c.timezoneLabel(), UpdatedAt: model.UpdatedAt, LastRun: last}
	if model.Enabled && model.NextRunAt != "" {
		next := model.NextRunAt
		view.NextRunAt = &next
		view.Upcoming = append(view.Upcoming, next)
		if cron, err := agent.ParseCron(model.Cron); err == nil {
			if at, err := parseScheduleTime(next); err == nil {
				at = at.In(c.scheduleLocation())
				for len(view.Upcoming) < scheduleUpcomingCount {
					at = cron.Next(at)
					if at.IsZero() {
						break
					}
					view.Upcoming = append(view.Upcoming, formatScheduleTime(at))
				}
			}
		}
	}
	return view
}

// scheduleAsset 校验路径资产存在；失败已写响应。
func (c *Capability) scheduleAsset(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, err := pathID(r, "id", "资产")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	asset, err := c.store.assetModel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取资产失败")
		return "", false
	}
	if asset == nil {
		writeError(w, http.StatusNotFound, "资产不存在")
		return "", false
	}
	return id, true
}

func (c *Capability) writeSchedule(w http.ResponseWriter, r *http.Request, assetID string, status int) {
	model, err := c.store.schedule(r.Context(), assetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取计划失败")
		return
	}
	if model == nil {
		writeError(w, http.StatusNotFound, "资产未设置周期计划")
		return
	}
	runs, err := c.store.scheduleRuns(r.Context(), assetID, 1, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取计划运行记录失败")
		return
	}
	var last *ScheduleRun
	if len(runs) > 0 {
		view, err := c.runViewWithFollowups(r.Context(), runs[0])
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取计划后续失败")
			return
		}
		last = &view
	}
	autoVerify, autoRetest, err := c.store.scheduleOptions(r.Context(), assetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取计划失败")
		return
	}
	writeJSON(w, status, c.scheduleView(model, last, autoVerify, autoRetest))
}

func (c *Capability) handleScheduleGet(w http.ResponseWriter, r *http.Request) {
	if id, ok := c.scheduleAsset(w, r); ok {
		c.writeSchedule(w, r, id, http.StatusOK)
	}
}

// handleSchedulePut 保存即生效：校验 cron，按当前时间重算 next_run_at（停用时为空）。
func (c *Capability) handleSchedulePut(w http.ResponseWriter, r *http.Request) {
	id, ok := c.scheduleAsset(w, r)
	if !ok {
		return
	}
	var body saveSchedule
	if err := decodeJSONBody(r, &body, true, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := c.scheduleClock().In(c.scheduleLocation())
	cron, err := parseScheduleCron(body.Cron, now)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled := body.Enabled == nil || *body.Enabled
	model := assetScheduleModel{AssetID: id, Cron: cron.Raw, Enabled: enabled, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if enabled {
		model.NextRunAt = formatScheduleTime(cron.Next(now))
	}
	options := scheduleOptionsModel{AssetID: id, AutoVerify: body.AutoVerify == nil || *body.AutoVerify, AutoRetest: body.AutoRetest == nil || *body.AutoRetest, UpdatedAt: model.UpdatedAt}
	before := c.scheduleSnapshot(r.Context(), id)
	created, err := c.store.putSchedule(r.Context(), model, options)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存计划失败")
		return
	}
	c.auditScheduleSave(r.Context(), id, before, model, options)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.writeSchedule(w, r, id, status)
}

// handleScheduleDelete 删除计划；历史运行记录保留。
func (c *Capability) handleScheduleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := c.scheduleAsset(w, r)
	if !ok {
		return
	}
	before := c.scheduleSnapshot(r.Context(), id)
	deleted, err := c.store.deleteSchedule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除计划失败")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "资产未设置周期计划")
		return
	}
	c.auditScheduleDelete(r.Context(), id, before)
	writeJSON(w, http.StatusOK, map[string]any{"asset_id": id, "deleted": true})
}

// handleScheduleRuns 按 scheduled_for 倒序分页：limit 1-200（默认 50），cursor 为上页 next_cursor。
func (c *Capability) handleScheduleRuns(w http.ResponseWriter, r *http.Request) {
	id, ok := c.scheduleAsset(w, r)
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			writeError(w, http.StatusBadRequest, "limit须为1至200")
			return
		}
		limit = value
	}
	before := ""
	if encoded := r.URL.Query().Get("cursor"); encoded != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			writeError(w, http.StatusBadRequest, "cursor非法")
			return
		}
		if _, err := parseScheduleTime(string(decoded)); err != nil {
			writeError(w, http.StatusBadRequest, "cursor非法")
			return
		}
		before = string(decoded)
	}
	models, err := c.store.scheduleRuns(r.Context(), id, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取计划运行记录失败")
		return
	}
	hasMore := len(models) > limit
	if hasMore {
		models = models[:limit]
	}
	items := make([]ScheduleRun, 0, len(models))
	for _, model := range models {
		view, err := c.runViewWithFollowups(r.Context(), model)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取计划后续失败")
			return
		}
		items = append(items, view)
	}
	response := map[string]any{"asset_id": id, "items": items, "limit": limit, "has_more": hasMore, "next_cursor": nil}
	if hasMore {
		response["next_cursor"] = base64.RawURLEncoding.EncodeToString([]byte(models[len(models)-1].ScheduledFor))
	}
	writeJSON(w, http.StatusOK, response)
}

// ScheduleListItem 是全部计划列表行。
type ScheduleListItem struct {
	AssetSchedule
	AssetName     string `json:"asset_name"`
	AssetArchived bool   `json:"asset_archived"`
}

func (c *Capability) handleScheduleList(w http.ResponseWriter, r *http.Request) {
	rows, err := c.store.listSchedules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取计划失败")
		return
	}
	latest, err := c.store.latestRuns(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取计划运行记录失败")
		return
	}
	items := make([]ScheduleListItem, 0, len(rows))
	for index := range rows {
		var last *ScheduleRun
		if run, ok := latest[rows[index].AssetID]; ok {
			view, err := c.runViewWithFollowups(r.Context(), run)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "读取计划后续失败")
				return
			}
			last = &view
		}
		items = append(items, ScheduleListItem{AssetSchedule: c.scheduleView(&rows[index].assetScheduleModel, last, rows[index].AutoVerify, rows[index].AutoRetest), AssetName: rows[index].AssetName, AssetArchived: rows[index].AssetArchived})
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": items, "timezone": c.timezoneLabel()})
}
