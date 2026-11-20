package webapi

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

// 资产（C1）：企业登记一次的固定测试环境。与上游浏览器“资产池”无关，独立服务端实体。
type assetModel struct {
	bun.BaseModel `bun:"table:webapi_assets,alias:webapi_asset"`

	ID          string `bun:"id,pk"`
	Name        string `bun:"name,notnull"`
	Owner       string `bun:"owner,type:text,notnull"`
	Environment string `bun:"environment,type:text,notnull"`
	KnownInfo   string `bun:"known_info,type:text,notnull"`
	Objective   string `bun:"objective,type:text,notnull"`
	OriginsJSON string `bun:"origins_json,type:text,notnull"`
	Forbidden   string `bun:"forbidden,type:text,notnull"`
	Scope       string `bun:"scope,notnull"`
	Approval    string `bun:"approval,notnull"`
	Archived    bool   `bun:"archived,notnull"`
	CreatedAt   string `bun:"created_at,notnull"`
	UpdatedAt   string `bun:"updated_at,notnull"`

	// 默认研究场景（M1）存 webapi_asset_scenarios，不是本表列；这里只供审计比较。
	ScenarioKey  string `bun:"-"`
	DoneCriteria string `bun:"-"`
}

// assetCredentialModel 的 secret 只以 AES-GCM 密文落库；空串表示未设置。
type assetCredentialModel struct {
	bun.BaseModel `bun:"table:webapi_asset_credentials,alias:webapi_asset_credential"`

	ID           string `bun:"id,pk"`
	AssetID      string `bun:"asset_id,notnull"`
	Label        string `bun:"label,notnull"`
	Username     string `bun:"username,notnull"`
	SecretCipher string `bun:"secret_cipher,type:text,notnull"`
	Note         string `bun:"note,type:text,notnull"`
	CreatedAt    string `bun:"created_at,notnull"`
	UpdatedAt    string `bun:"updated_at,notnull"`
}

// assetTaskModel 记录“从资产发起”的任务关联；任务本身仍是普通 webapi_tasks 行。
type assetTaskModel struct {
	bun.BaseModel `bun:"table:webapi_asset_tasks,alias:webapi_asset_task"`

	TaskID    string `bun:"task_id,pk"`
	AssetID   string `bun:"asset_id,notnull"`
	CreatedAt string `bun:"created_at,notnull"`
}

// Asset 是资产接口视图；凭据只给 has_secret，永不返回 secret。
type Asset struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Owner       string            `json:"owner"`
	Environment string            `json:"environment"`
	KnownInfo   string            `json:"known_info"`
	Objective   string            `json:"objective"`
	Origins     []string          `json:"origins"`
	Forbidden   string            `json:"forbidden"`
	Scope       string            `json:"scope"`
	Approval    string            `json:"approval"`
	Archived    bool              `json:"archived"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
	Credentials []AssetCredential `json:"credentials"`
	// Scenario 是资产默认研究场景（M1），发起任务与计划发起时拷贝到任务。
	Scenario TaskScenario `json:"scenario"`
}

type AssetCredential struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Username  string `json:"username"`
	HasSecret bool   `json:"has_secret"`
	Note      string `json:"note"`
	UpdatedAt string `json:"updated_at"`
}

// TaskAssetRef 附在任务响应上：从资产发起的任务为 {id,name}，否则 null。
type TaskAssetRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AssetTaskSummary 是资产关联任务列表行。
type AssetTaskSummary struct {
	TaskID           string `json:"task_id"`
	Objective        string `json:"objective"`
	InitialSessionID string `json:"initial_session_id"`
	CreatedAt        string `json:"created_at"`
	FindingCount     int    `json:"finding_count"`
}

var (
	errCredentialOtherAsset = errors.New("凭据 id 已属于其他资产")
	errAssetTaskOther       = errors.New("任务 id 已被占用（不是从该资产发起的任务）")
)

func (m *assetModel) toAsset(credentials []assetCredentialModel) (*Asset, error) {
	origins, err := decodeOrigins(m.OriginsJSON)
	if err != nil {
		return nil, err
	}
	asset := &Asset{
		ID: m.ID, Name: m.Name, Owner: m.Owner, Environment: m.Environment, KnownInfo: m.KnownInfo,
		Objective: m.Objective, Origins: origins, Forbidden: m.Forbidden, Scope: m.Scope, Approval: m.Approval,
		Archived: m.Archived, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt, Credentials: []AssetCredential{},
	}
	for _, credential := range credentials {
		asset.Credentials = append(asset.Credentials, AssetCredential{
			ID: credential.ID, Label: credential.Label, Username: credential.Username,
			HasSecret: credential.SecretCipher != "", Note: credential.Note, UpdatedAt: credential.UpdatedAt,
		})
	}
	return asset, nil
}

func (s *taskStore) assetModel(ctx context.Context, id string) (*assetModel, error) {
	var model assetModel
	err := s.orm.NewSelect().Model(&model).Where("id = ?", id).Limit(1).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &model, nil
}

func (s *taskStore) assetCredentials(ctx context.Context, assetIDs ...string) (map[string][]assetCredentialModel, error) {
	out := map[string][]assetCredentialModel{}
	if len(assetIDs) == 0 {
		return out, nil
	}
	var models []assetCredentialModel
	if err := s.orm.NewSelect().Model(&models).Where("asset_id IN (?)", bun.In(assetIDs)).OrderExpr("created_at ASC, id ASC").Scan(ctx); err != nil {
		return nil, err
	}
	for _, model := range models {
		out[model.AssetID] = append(out[model.AssetID], model)
	}
	return out, nil
}

// asset 读一个资产视图；不存在返回 (nil, nil)。
func (s *taskStore) asset(ctx context.Context, id string) (*Asset, error) {
	model, err := s.assetModel(ctx, id)
	if err != nil || model == nil {
		return nil, err
	}
	credentials, err := s.assetCredentials(ctx, id)
	if err != nil {
		return nil, err
	}
	scenarios, err := s.assetScenarios(ctx, id)
	if err != nil {
		return nil, err
	}
	asset, err := model.toAsset(credentials[id])
	if err != nil {
		return nil, err
	}
	asset.Scenario = scenarios[id].view()
	return asset, nil
}

// listAssets 按 archived 过滤，最近更新在前。
func (s *taskStore) listAssets(ctx context.Context, archived bool) ([]*Asset, error) {
	var models []assetModel
	if err := s.orm.NewSelect().Model(&models).Where("archived = ?", archived).OrderExpr("updated_at DESC, id ASC").Scan(ctx); err != nil {
		return nil, err
	}
	ids := make([]string, len(models))
	for index := range models {
		ids[index] = models[index].ID
	}
	credentials, err := s.assetCredentials(ctx, ids...)
	if err != nil {
		return nil, err
	}
	scenarios, err := s.assetScenarios(ctx, ids...)
	if err != nil {
		return nil, err
	}
	out := make([]*Asset, 0, len(models))
	for index := range models {
		asset, err := models[index].toAsset(credentials[models[index].ID])
		if err != nil {
			return nil, err
		}
		asset.Scenario = scenarios[models[index].ID].view()
		out = append(out, asset)
	}
	return out, nil
}

// putAsset 保存即生效：新 id 插入，已有 id 覆盖全部可编辑字段（保留 created_at）。
// 首条 SQL 即写（INSERT … DO NOTHING），避免读事务升级 BUSY。
func (s *taskStore) putAsset(ctx context.Context, model assetModel) (created bool, err error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	model.CreatedAt, model.UpdatedAt = now, now
	err = s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewInsert().Model(&model).On("CONFLICT (id) DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected > 0 {
			created = true
			return nil
		}
		_, err = tx.NewUpdate().Model(&model).
			Column("name", "owner", "environment", "known_info", "objective", "origins_json", "forbidden", "scope", "approval", "archived", "updated_at").
			WherePK().Exec(ctx)
		return err
	})
	return created, err
}

// hasStoredSecrets 判断库里是否已有密文（决定密钥缺失时能否生成新密钥）。
func (s *taskStore) hasStoredSecrets(ctx context.Context) (bool, error) {
	return s.orm.NewSelect().Model((*assetCredentialModel)(nil)).Where("secret_cipher <> ''").Exists(ctx)
}

// putCredential upsert 凭据；secretCipher=nil 表示保留原值（新建则为未设置）。
func (s *taskStore) putCredential(ctx context.Context, assetID, credentialID, label, username, note string, secretCipher *string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	model := assetCredentialModel{ID: credentialID, AssetID: assetID, Label: label, Username: username, Note: note, CreatedAt: now, UpdatedAt: now}
	if secretCipher != nil {
		model.SecretCipher = *secretCipher
	}
	return s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewInsert().Model(&model).On("CONFLICT (id) DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		if affected, err := res.RowsAffected(); err != nil || affected > 0 {
			return err
		}
		columns := []string{"label", "username", "note", "updated_at"}
		if secretCipher != nil {
			columns = append(columns, "secret_cipher")
		}
		res, err = tx.NewUpdate().Model(&model).Column(columns...).Where("id = ? AND asset_id = ?", credentialID, assetID).Exec(ctx)
		if err != nil {
			return err
		}
		if affected, err := res.RowsAffected(); err != nil {
			return err
		} else if affected == 0 {
			return errCredentialOtherAsset
		}
		return nil
	})
}

// deleteCredential 删除一条凭据；不存在返回 false。
func (s *taskStore) deleteCredential(ctx context.Context, assetID, credentialID string) (bool, error) {
	res, err := s.orm.NewDelete().Model((*assetCredentialModel)(nil)).Where("id = ? AND asset_id = ?", credentialID, assetID).Exec(ctx)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

// assetForTask 返回任务关联资产引用；非资产任务返回 nil。
func (s *taskStore) assetForTask(ctx context.Context, taskID string) (*TaskAssetRef, error) {
	var ref TaskAssetRef
	err := s.orm.NewSelect().TableExpr("webapi_asset_tasks AS t").Join("JOIN webapi_assets AS a ON a.id = t.asset_id").
		ColumnExpr("a.id AS id, a.name AS name").Where("t.task_id = ?", taskID).Limit(1).Scan(ctx, &ref.ID, &ref.Name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ref, nil
}

// listAssetTasks 列出资产关联任务（最近在前），带候选数。
func (s *taskStore) listAssetTasks(ctx context.Context, assetID string) ([]AssetTaskSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.objective, t.initial_session_id, l.created_at,
		(SELECT COUNT(*) FROM webapi_findings f WHERE f.task_id = t.id)
		FROM webapi_asset_tasks l JOIN webapi_tasks t ON t.id = l.task_id
		WHERE l.asset_id = ? ORDER BY l.created_at DESC, t.id DESC`, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssetTaskSummary{}
	for rows.Next() {
		var item AssetTaskSummary
		if err := rows.Scan(&item.TaskID, &item.Objective, &item.InitialSessionID, &item.CreatedAt, &item.FindingCount); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// createAssetTask 在一个事务内建任务、写策略（含变更记录）、研究场景与资产关联。按 task_id 幂等：
// 已是该资产的任务则原样返回（created=false），被其他用途占用返回 errAssetTaskOther。
// scenario 非空时（资产有默认场景或发起时覆盖）在同一事务内拷贝为任务场景；为空＝任务用默认场景。
func (s *taskStore) createAssetTask(ctx context.Context, assetID, taskID, knownInfo, objective string, origins []string, scope, approval string, scenario *taskScenarioModel) (*Task, bool, error) {
	originsJSON, err := marshalOrigins(origins)
	if err != nil {
		return nil, false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	task := taskModel{ID: taskID, KnownInfo: knownInfo, Objective: objective, OriginsJSON: originsJSON, InitialSessionID: newSessionID(), CreatedAt: now}
	created := false
	var saved *Task
	err = s.orm.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		res, err := tx.NewInsert().Model(&task).On("CONFLICT (id) DO NOTHING").Exec(ctx)
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 0 {
			var link assetTaskModel
			err := tx.NewSelect().Model(&link).Where("task_id = ?", taskID).Limit(1).Scan(ctx)
			if errors.Is(err, sql.ErrNoRows) || err == nil && link.AssetID != assetID {
				return errAssetTaskOther
			}
			if err != nil {
				return err
			}
			var existing taskModel
			if err := tx.NewSelect().Model(&existing).Where("id = ?", taskID).Limit(1).Scan(ctx); err != nil {
				return err
			}
			saved, err = existing.toTask()
			return err
		}
		created = true
		policy := &taskPolicyModel{TaskID: taskID, Scope: scope, Approval: approval, UpdatedAt: now}
		if _, err := tx.NewInsert().Model(policy).Exec(ctx); err != nil {
			return err
		}
		event := &taskPolicyEventModel{ID: "wp_" + newSessionID() + newSessionID(), TaskID: taskID, Scope: scope, Approval: approval, CreatedAt: now}
		if _, err := tx.NewInsert().Model(event).Exec(ctx); err != nil {
			return err
		}
		if _, err := tx.NewInsert().Model(&assetTaskModel{TaskID: taskID, AssetID: assetID, CreatedAt: now}).Exec(ctx); err != nil {
			return err
		}
		if scenario != nil {
			if err := putTaskScenarioTx(ctx, tx, taskID, scenario.Scenario, scenario.DoneCriteria); err != nil {
				return err
			}
		}
		saved, err = task.toTask()
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return saved, created, nil
}

// composeAssetKnownInfo：资产已知信息 + 环境/负责人（空值省略）；三者均空时用资产名称。
func composeAssetKnownInfo(asset *assetModel) string {
	if strings.TrimSpace(asset.KnownInfo) == "" && strings.TrimSpace(asset.Environment) == "" && strings.TrimSpace(asset.Owner) == "" {
		return "资产：" + asset.Name
	}
	parts := []string{}
	if text := strings.TrimSpace(asset.KnownInfo); text != "" {
		parts = append(parts, text)
	}
	meta := []string{}
	if text := strings.TrimSpace(asset.Environment); text != "" {
		meta = append(meta, "环境："+text)
	}
	if text := strings.TrimSpace(asset.Owner); text != "" {
		meta = append(meta, "负责人："+text)
	}
	if len(meta) > 0 {
		parts = append(parts, strings.Join(meta, "\n"))
	}
	return strings.Join(parts, "\n\n")
}
