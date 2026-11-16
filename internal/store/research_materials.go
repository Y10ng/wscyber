package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

const materialColumns = `item_id,source_token,name,path,description,format,content_sha256,byte_size`

func scanMaterialReference(row scanner) (domain.MaterialReference, error) {
	var x domain.MaterialReference
	err := row.Scan(&x.ID, &x.SourceToken, &x.Name, &x.Path, &x.Description, &x.Format, &x.ContentSHA256, &x.ByteSize)
	return x, databaseError(err)
}
func scanTaskMaterial(row scanner) (domain.TaskMaterial, error) {
	var x domain.TaskMaterial
	err := row.Scan(&x.ID, &x.SourceToken, &x.Name, &x.Path, &x.Description, &x.Format, &x.ContentSHA256, &x.ByteSize, &x.Content)
	if err != nil {
		return x, databaseError(err)
	}
	if x.ByteSize != len(x.Content) || x.ContentSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(x.Content))) || x.ByteSize < 1 || x.ByteSize > domain.MaxMaterialBytes {
		return domain.TaskMaterial{}, fmt.Errorf("%w：任务资料完整性检查失败", domain.ErrUnavailable)
	}
	return x, nil
}

// 调用者已查提交历史；本事务持续持有库锁，选用与编辑/删除不会拼接两个状态。
func selectTaskMaterials(ctx context.Context, tx pgx.Tx, selection []domain.MaterialSelection) ([]domain.TaskMaterial, error) {
	out := make([]domain.TaskMaterial, 0, len(selection))
	if len(selection) == 0 {
		return out, nil
	}
	if err := lockLibrary(ctx, tx); err != nil {
		return nil, err
	}
	total := 0
	for _, ref := range selection {
		item, err := scanLibraryDetail(tx.QueryRow(ctx, `SELECT `+libraryColumns+`,content FROM library_items WHERE id=$1 AND deleted_at IS NULL`, ref.ID))
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrMaterialSelectionStale
		}
		if err != nil {
			return nil, err
		}
		if item.Kind != "file" || item.Token != ref.Token {
			return nil, domain.ErrMaterialSelectionStale
		}
		total += len(item.Content)
		if total > domain.MaxTaskMaterialBytes {
			return nil, invalid("选用资料正文合计最多65536字节")
		}
		parts := []string{item.Name}
		parent := item.ParentID
		seen := map[string]bool{item.ID: true}
		for parent != "" {
			if seen[parent] || len(parts) > domain.MaxLibraryDepth {
				return nil, fmt.Errorf("%w：资料目录完整性检查失败", domain.ErrUnavailable)
			}
			seen[parent] = true
			var name string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(parent_id,''),name FROM library_items WHERE id=$1 AND kind='folder' AND deleted_at IS NULL`, parent).Scan(&parent, &name); err != nil {
				return nil, err
			}
			parts = append([]string{name}, parts...)
		}
		out = append(out, domain.TaskMaterial{MaterialReference: domain.MaterialReference{ID: item.ID, SourceToken: item.Token, Name: item.Name, Path: strings.Join(parts, "/"), Description: item.Description, Format: item.Format, ContentSHA256: item.ContentSHA256, ByteSize: item.ByteSize}, Content: item.Content})
	}
	return out, nil
}
func saveResearchConfiguration(ctx context.Context, tx pgx.Tx, missionID string, in domain.ExplorationInput, materials []domain.TaskMaterial) error {
	if _, err := tx.Exec(ctx, `INSERT INTO mission_research_configs(mission_id,safety_prompt_enabled,safety_prompt) VALUES($1,$2,$3)`, missionID, in.SafetyPromptEnabled, in.SafetyPrompt); err != nil {
		return err
	}
	for _, m := range materials {
		if _, err := tx.Exec(ctx, `INSERT INTO mission_materials(mission_id,item_id,source_token,name,path,description,format,content_sha256,byte_size,content) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, missionID, m.ID, m.SourceToken, m.Name, m.Path, m.Description, m.Format, m.ContentSHA256, m.ByteSize, m.Content); err != nil {
			return err
		}
	}
	return saveResearchBrief(ctx, tx, missionID, in.ResearchBrief)
}
func readResearchConfiguration(ctx context.Context, q querier, id string) (domain.ResearchConfiguration, error) {
	out := domain.ResearchConfiguration{MissionID: id, Materials: []domain.MaterialReference{}}
	// 一个LEFT JOIN区分不存在的任务和C1之前没有配置行的历史任务。
	err := q.QueryRow(ctx, `SELECT COALESCE(c.safety_prompt_enabled,false),COALESCE(c.safety_prompt,'') FROM missions m LEFT JOIN mission_research_configs c ON c.mission_id=m.id WHERE m.id=$1`, id).Scan(&out.SafetyPromptEnabled, &out.SafetyPrompt)
	if err != nil {
		return out, databaseError(err)
	}
	out.Materials, err = collect(ctx, q, `SELECT `+materialColumns+` FROM mission_materials WHERE mission_id=$1 ORDER BY item_id LIMIT 17`, scanMaterialReference, id)
	if err == nil && len(out.Materials) > domain.MaxTaskMaterials {
		return out, fmt.Errorf("%w：任务资料数量超过上限", domain.ErrUnavailable)
	}
	if err == nil {
		out.ResearchBrief, err = readResearchBrief(ctx, q, id)
	}
	return out, err
}
func (s *Store) GetResearchConfiguration(ctx context.Context, id string) (domain.ResearchConfiguration, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.ResearchConfiguration, error) { return readResearchConfiguration(ctx, tx, id) })
}
func (s *Store) GetTaskMaterial(ctx context.Context, missionID, itemID string) (domain.TaskMaterial, error) {
	if !domain.ValidLibraryID(itemID) {
		return domain.TaskMaterial{}, invalid("资料ID无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// 归属条件直接作用于正文查询；不存在和未选用同为404，不先读全局资料。
	return scanTaskMaterial(s.pool.QueryRow(ctx, `SELECT `+materialColumns+`,content FROM mission_materials WHERE mission_id=$1 AND item_id=$2`, missionID, itemID))
}
func (s *Store) GetResearchContext(ctx context.Context, id string) (domain.ResearchContext, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.ResearchContext, error) { return readResearchContext(ctx, tx, id) })
}
func readResearchContext(ctx context.Context, tx pgx.Tx, id string) (domain.ResearchContext, error) {
	var out domain.ResearchContext
	cfg, err := readResearchConfiguration(ctx, tx, id)
	if err != nil {
		return out, err
	}
	out.SafetyPromptEnabled = cfg.SafetyPromptEnabled
	out.SafetyPrompt = cfg.SafetyPrompt
	if cfg.ResearchBrief.Shared() {
		out.ResearchBrief = cfg.ResearchBrief
	}
	if err = domain.ValidateResearchSelection(domain.ExplorationInput{SafetyPromptEnabled: cfg.SafetyPromptEnabled, SafetyPrompt: cfg.SafetyPrompt}); err != nil {
		return out, fmt.Errorf("%w：任务安全配置完整性检查失败", domain.ErrUnavailable)
	}
	out.Materials, err = collect(ctx, tx, `SELECT `+materialColumns+`,content FROM mission_materials WHERE mission_id=$1 ORDER BY item_id LIMIT 17`, scanTaskMaterial, id)
	if err != nil {
		return out, err
	}
	total := 0
	for _, m := range out.Materials {
		total += m.ByteSize
	}
	if len(out.Materials) > domain.MaxTaskMaterials || total > domain.MaxTaskMaterialBytes {
		return domain.ResearchContext{}, fmt.Errorf("%w：任务资料超过上限", domain.ErrUnavailable)
	}
	return out, nil
}
