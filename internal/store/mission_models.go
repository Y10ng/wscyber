package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

var _ domain.MissionModelRepository = (*Store)(nil)

func selectMissionModel(ctx context.Context, tx pgx.Tx, explicit string, options domain.ExplorationModelOptions) (domain.MissionModelBinding, error) {
	var b domain.MissionModelBinding
	var defaultID string
	// 与保存/默认选择/B2一致的锁顺序；默认、启用与解密核对是原子创建的一部分。
	if err := tx.QueryRow(ctx, `SELECT COALESCE(default_config_id,'') FROM model_settings WHERE singleton FOR UPDATE`).Scan(&defaultID); err != nil {
		return b, err
	}
	id := explicit
	if id == "" {
		id = defaultID
	}
	if id == "" {
		if !options.EnvironmentConfigured {
			return b, domain.ErrUnavailable
		}
		b.Source, b.EnvironmentFingerprint = "environment", options.EnvironmentFingerprint
		return b, nil
	}
	r, err := scanModelConfig(tx.QueryRow(ctx, `SELECT `+modelConfigColumns+` FROM model_configs c JOIN model_config_versions v ON v.config_id=c.id AND v.revision=c.current_revision WHERE c.id=$1 FOR UPDATE OF c`, id))
	if err != nil {
		return b, err
	}
	if !r.Enabled {
		return b, conflict("所选模型配置已禁用，未启动探索")
	}
	if options.Validate == nil {
		return b, domain.ErrUnavailable
	}
	if err := options.Validate(r); err != nil {
		return b, err
	}
	b.Source, b.ConfigID, b.ConfigRevision = "managed", r.ID, r.Revision
	return b, nil
}

func saveMissionModel(ctx context.Context, tx pgx.Tx, b domain.MissionModelBinding) error {
	var fingerprint *string
	if b.Source == "environment" {
		fingerprint = &b.EnvironmentFingerprint
	}
	_, err := tx.Exec(ctx, `INSERT INTO mission_models(mission_id,source,config_id,config_revision,environment_fingerprint) VALUES($1,$2,NULLIF($3,''),NULLIF($4,0),$5)`, b.MissionID, b.Source, b.ConfigID, b.ConfigRevision, fingerprint)
	return err
}

func (s *Store) GetMissionModelBinding(ctx context.Context, id string) (*domain.MissionModelBinding, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var b domain.MissionModelBinding
	err := s.pool.QueryRow(ctx, `SELECT m.id,COALESCE(b.source,''),COALESCE(b.config_id,''),COALESCE(b.config_revision,0),COALESCE(b.environment_fingerprint,'') FROM missions m LEFT JOIN mission_models b ON b.mission_id=m.id WHERE m.id=$1`, id).Scan(&b.MissionID, &b.Source, &b.ConfigID, &b.ConfigRevision, &b.EnvironmentFingerprint)
	if err != nil {
		return nil, databaseError(err)
	}
	if b.Source == "" {
		return nil, nil
	}
	return &b, nil
}
