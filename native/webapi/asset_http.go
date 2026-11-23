package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/uptrace/bun"
)

const (
	maxAssetNameRunes   = 128
	maxCredentialField  = 256
	maxCredentialSecret = 4096
	maxCredentialNote   = 2000
)

// validPathID 是普通安全路径 ID：非空、≤128、不含分隔符/控制字符（同任务 id 规则）。
func validPathID(id string) error {
	if id == "" || len(id) > 128 || strings.TrimSpace(id) != id {
		return errors.New("id 非法")
	}
	if strings.ContainsAny(id, "/\\?#%") || strings.ContainsFunc(id, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
		return errors.New("id 非法")
	}
	return nil
}

func pathID(r *http.Request, key, label string) (string, error) {
	id := r.PathValue(key)
	if validPathID(id) != nil {
		return "", fmt.Errorf("%s id 非法", label)
	}
	return id, nil
}

// decodeJSONBody 解析恰好一个 JSON 对象；allowEmpty 时空请求体视为 {}。
func decodeJSONBody(r *http.Request, target any, strict, allowEmpty bool) error {
	defer r.Body.Close()
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, maxRequestBytes))
	if err != nil {
		return errors.New("请求体过大或不可读")
	}
	if allowEmpty && strings.TrimSpace(string(raw)) == "" {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	if strict {
		decoder.DisallowUnknownFields()
	}
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("请求体不是合法 JSON 对象：%v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("请求体只能包含一个 JSON 对象")
	}
	return nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// saveAsset 是 PUT /assets/{id} 请求体：整份替换可编辑字段；scope/approval 空＝默认。
type saveAsset struct {
	Name        string   `json:"name"`
	Owner       string   `json:"owner"`
	Environment string   `json:"environment"`
	KnownInfo   string   `json:"known_info"`
	Objective   string   `json:"objective"`
	Origins     []string `json:"origins"`
	Forbidden   string   `json:"forbidden"`
	Scope       string   `json:"scope"`
	Approval    string   `json:"approval"`
	Archived    bool     `json:"archived"`
	// 默认研究场景（M1）：未提供＝保留当前值（新资产为默认）；提供则校验后保存，空串＝默认。
	Scenario     *string `json:"scenario"`
	DoneCriteria *string `json:"done_criteria"`
	// 持续探索最大轮数（M3）：未提供＝保留当前值；提供须为 1..20。
	MaxRounds *int `json:"max_rounds"`
}

func validateSaveAsset(id string, body *saveAsset) (*assetModel, error) {
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return nil, errors.New("name 不能为空")
	}
	if utf8.RuneCountInString(name) > maxAssetNameRunes {
		return nil, fmt.Errorf("name 不能超过 %d 字", maxAssetNameRunes)
	}
	seen := map[string]bool{}
	origins := []string{}
	for index, raw := range body.Origins {
		norm, err := normalizeOrigin(raw)
		if err != nil {
			return nil, fmt.Errorf("origins[%d]：%v", index, err)
		}
		if !seen[norm] {
			seen[norm] = true
			origins = append(origins, norm)
		}
	}
	if len(origins) == 0 {
		return nil, errors.New("origins 至少需要一个合法 http/https origin")
	}
	sort.Strings(origins)
	scope, approval := strings.TrimSpace(body.Scope), strings.TrimSpace(body.Approval)
	if scope == "" {
		scope = ScopeOrigins
	}
	if approval == "" {
		approval = ApprovalHighRisk
	}
	if err := validPolicy(scope, approval); err != nil {
		return nil, err
	}
	originsJSON, err := marshalOrigins(origins)
	if err != nil {
		return nil, err
	}
	return &assetModel{
		ID: id, Name: name, Owner: body.Owner, Environment: body.Environment, KnownInfo: body.KnownInfo,
		Objective: body.Objective, OriginsJSON: originsJSON, Forbidden: body.Forbidden,
		Scope: scope, Approval: approval, Archived: body.Archived,
	}, nil
}

func (c *Capability) handleAssetList(w http.ResponseWriter, r *http.Request) {
	archived := false
	switch r.URL.Query().Get("archived") {
	case "", "0":
	case "1":
		archived = true
	default:
		writeError(w, http.StatusBadRequest, "archived 只能是 0 或 1")
		return
	}
	assets, err := c.store.listAssets(r.Context(), archived)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取资产失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"assets": assets})
}

func (c *Capability) handleAssetGet(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id", "资产")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c.writeAsset(w, r, id, http.StatusOK)
}

func (c *Capability) writeAsset(w http.ResponseWriter, r *http.Request, id string, status int) {
	asset, err := c.store.asset(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取资产失败")
		return
	}
	if asset == nil {
		writeError(w, http.StatusNotFound, "资产不存在")
		return
	}
	writeJSON(w, status, asset)
}

func (c *Capability) handleAssetPut(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id", "资产")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var body saveAsset
	if err := decodeJSONBody(r, &body, false, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	model, err := validateSaveAsset(id, &body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	before, err := c.store.assetModel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取资产失败")
		return
	}
	beforeScenario, err := readValue(r.Context(), c.store, func(ctx context.Context, db bun.IDB) (*assetScenarioModel, error) {
		return assetScenarioModelOf(ctx, db, id)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取资产研究场景失败")
		return
	}
	currentScenario := beforeScenario.view()
	if before != nil {
		before.ScenarioKey, before.DoneCriteria, before.MaxRounds = currentScenario.Key, currentScenario.DoneCriteria, currentScenario.MaxRounds
	}
	input := scenarioInput{Scenario: body.Scenario, DoneCriteria: body.DoneCriteria, MaxRounds: body.MaxRounds}
	model.ScenarioKey, model.DoneCriteria, err = input.resolve(currentScenario.Key, currentScenario.DoneCriteria)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rounds, err := input.rounds()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	savedRounds := rounds
	if savedRounds == 0 && beforeScenario != nil {
		savedRounds = beforeScenario.MaxRounds
	}
	model.MaxRounds = newTaskScenario(model.ScenarioKey, model.DoneCriteria, false).withMaxRounds(savedRounds).MaxRounds
	created, err := c.store.putAsset(r.Context(), *model)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存资产失败")
		return
	}
	if input.provided() {
		if err := c.store.putAssetScenario(r.Context(), id, model.ScenarioKey, model.DoneCriteria, rounds); err != nil {
			writeError(w, http.StatusInternalServerError, "保存资产研究场景失败")
			return
		}
	}
	c.auditAssetSave(r.Context(), before, *model)
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	c.writeAsset(w, r, id, status)
}

// saveCredential：secret 省略或空串＝保留原值（新建则未设置）。
type saveCredential struct {
	Label    string  `json:"label"`
	Username string  `json:"username"`
	Secret   *string `json:"secret"`
	Note     string  `json:"note"`
}

func (c *Capability) assetForWrite(w http.ResponseWriter, r *http.Request) (assetID, credentialID string, ok bool) {
	assetID, err := pathID(r, "id", "资产")
	if err == nil {
		credentialID, err = pathID(r, "cid", "凭据")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", "", false
	}
	asset, err := c.store.assetModel(r.Context(), assetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取资产失败")
		return "", "", false
	}
	if asset == nil {
		writeError(w, http.StatusNotFound, "资产不存在")
		return "", "", false
	}
	return assetID, credentialID, true
}

func (c *Capability) handleCredentialPut(w http.ResponseWriter, r *http.Request) {
	assetID, credentialID, ok := c.assetForWrite(w, r)
	if !ok {
		return
	}
	var body saveCredential
	if err := decodeJSONBody(r, &body, true, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	switch {
	case utf8.RuneCountInString(body.Label) > maxCredentialField:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("label 不能超过 %d 字", maxCredentialField))
		return
	case utf8.RuneCountInString(body.Username) > maxCredentialField:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("username 不能超过 %d 字", maxCredentialField))
		return
	case body.Secret != nil && len(*body.Secret) > maxCredentialSecret:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("secret 不能超过 %d 字节", maxCredentialSecret))
		return
	case utf8.RuneCountInString(body.Note) > maxCredentialNote:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("note 不能超过 %d 字", maxCredentialNote))
		return
	}
	var sealed *string
	if body.Secret != nil && *body.Secret != "" {
		existing, err := c.store.hasStoredSecrets(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取测试账号失败")
			return
		}
		aead, err := c.secrets.load(!existing)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		value, err := sealSecret(aead, assetID, credentialID, *body.Secret)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "加密测试账号失败")
			return
		}
		sealed = &value
	}
	before := c.credentialBefore(r.Context(), assetID, credentialID)
	err := c.store.putCredential(r.Context(), assetID, credentialID, body.Label, body.Username, body.Note, sealed)
	if errors.Is(err, errCredentialOtherAsset) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存测试账号失败")
		return
	}
	c.auditCredentialSave(r.Context(), assetID, credentialID, before, body, sealed != nil)
	c.writeAsset(w, r, assetID, http.StatusOK)
}

func (c *Capability) handleCredentialDelete(w http.ResponseWriter, r *http.Request) {
	assetID, credentialID, ok := c.assetForWrite(w, r)
	if !ok {
		return
	}
	before := c.credentialBefore(r.Context(), assetID, credentialID)
	deleted, err := c.store.deleteCredential(r.Context(), assetID, credentialID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除测试账号失败")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "测试账号不存在")
		return
	}
	c.auditCredentialDelete(r.Context(), assetID, credentialID, before)
	c.writeAsset(w, r, assetID, http.StatusOK)
}

func (c *Capability) handleAssetTasks(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id", "资产")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	asset, err := c.store.assetModel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取资产失败")
		return
	}
	if asset == nil {
		writeError(w, http.StatusNotFound, "资产不存在")
		return
	}
	tasks, err := c.store.listAssetTasks(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取资产任务失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"asset_id": id, "tasks": tasks})
}

// handleAssetLaunch 是 LaunchAssetTask 的 HTTP 包装：201 新建并启动，200 续完或已完成；
// 失败按阶段返回 {error, stage, progress, task, session_id}。
func (c *Capability) handleAssetLaunch(w http.ResponseWriter, r *http.Request) {
	var body AssetLaunchOptions
	if err := decodeJSONBody(r, &body, true, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := c.LaunchAssetTask(r.Context(), r.PathValue("id"), r.PathValue("task_id"), body)
	var launchErr *AssetLaunchError
	if errors.As(err, &launchErr) {
		response := map[string]any{"error": launchErr.Msg, "stage": launchErr.Stage}
		if launchErr.Result != nil {
			response["progress"] = launchErr.Result.Progress
			response["task"] = launchErr.Result.Task
			response["session_id"] = launchErr.Result.SessionID
			response["node_id"] = launchErr.Result.NodeID
		}
		writeJSON(w, launchErr.Status, response)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发起任务失败")
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	writeJSON(w, status, result)
}
