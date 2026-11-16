package store

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
)

var _ domain.ModelConfigRepository = (*Store)(nil)

const modelConfigColumns = `c.id, v.revision, c.enabled, v.ciphertext, v.created_at`

func scanModelConfig(row scanner) (domain.SealedModelConfig, error) {
	var r domain.SealedModelConfig
	err := row.Scan(&r.ID, &r.Revision, &r.Enabled, &r.Ciphertext, &r.CreatedAt)
	return r, databaseError(err)
}
func (s *Store) ListModelConfigs(ctx context.Context) ([]domain.SealedModelConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return collect(ctx, s.pool, `SELECT `+modelConfigColumns+` FROM model_configs c
		JOIN model_config_versions v ON v.config_id=c.id AND v.revision=c.current_revision ORDER BY c.created_at,c.id`, scanModelConfig)
}
func (s *Store) GetModelConfig(ctx context.Context, id string, revision int64) (domain.SealedModelConfig, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return scanModelConfig(s.pool.QueryRow(ctx, `SELECT `+modelConfigColumns+` FROM model_configs c
		JOIN model_config_versions v ON v.config_id=c.id AND v.revision=CASE WHEN $2::bigint=0 THEN c.current_revision ELSE $2::bigint END WHERE c.id=$1`, id, revision))
}

func (s *Store) SaveModelConfig(ctx context.Context, in domain.SealedModelConfig, expected int64) (domain.SealedModelConfig, error) {
	var zero domain.SealedModelConfig
	decoded, err := hex.DecodeString(in.ID)
	if err != nil || len(decoded) != 16 || hex.EncodeToString(decoded) != in.ID || expected < 0 || expected >= 1<<53-1 || in.Revision != expected+1 || len(in.Ciphertext) < 30 || len(in.Ciphertext) > 98333 {
		return zero, invalid("模型配置ID、版本或密文无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.SealedModelConfig, error) {
		// 保存/默认选择同序锁定设置行，避免并发禁用与选择、数量上限产生竞态。
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
			return zero, err
		}
		if _, err := tx.Exec(ctx, `SELECT singleton FROM model_settings WHERE singleton FOR UPDATE`); err != nil {
			return zero, err
		}
		var revision int64
		err := tx.QueryRow(ctx, `SELECT current_revision FROM model_configs WHERE id=$1 FOR UPDATE`, in.ID).Scan(&revision)
		if errors.Is(err, pgx.ErrNoRows) {
			if expected != 0 {
				return zero, domain.ErrNotFound
			}
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM model_configs`).Scan(&count); err != nil {
				return zero, err
			}
			if count >= 100 {
				return zero, conflict("模型配置已达100项上限")
			}
			if _, err := tx.Exec(ctx, `INSERT INTO model_configs(id,current_revision,enabled) VALUES($1,$2,$3)`, in.ID, in.Revision, in.Enabled); err != nil {
				return zero, err
			}
		} else if err != nil {
			return zero, err
		} else {
			if revision != expected {
				return zero, conflict("配置版本已变化，请重新读取核对")
			}
			if _, err := tx.Exec(ctx, `UPDATE model_configs SET current_revision=$2,enabled=$3 WHERE id=$1`, in.ID, in.Revision, in.Enabled); err != nil {
				return zero, err
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO model_config_versions(config_id,revision,ciphertext) VALUES($1,$2,$3) RETURNING created_at`, in.ID, in.Revision, in.Ciphertext).Scan(&in.CreatedAt); err != nil {
			return zero, err
		}
		return in, nil
	})
}

func (s *Store) GetModelSettings(ctx context.Context) (domain.ModelSettings, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result domain.ModelSettings
	err := s.pool.QueryRow(ctx, `SELECT revision,COALESCE(default_config_id,'') FROM model_settings WHERE singleton`).Scan(&result.Revision, &result.DefaultConfigID)
	return result, databaseError(err)
}
func (s *Store) SetModelDefault(ctx context.Context, id string, expected int64) (domain.ModelSettings, error) {
	var zero domain.ModelSettings
	if expected < 0 || expected >= 1<<53-1 {
		return zero, invalid("默认配置版本无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.ModelSettings, error) {
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
			return zero, err
		}
		var current int64
		if err := tx.QueryRow(ctx, `SELECT revision FROM model_settings WHERE singleton FOR UPDATE`).Scan(&current); err != nil {
			return zero, err
		}
		if current != expected {
			return zero, conflict("默认模型已变化，请重新读取核对")
		}
		if id != "" {
			var enabled bool
			if err := tx.QueryRow(ctx, `SELECT enabled FROM model_configs WHERE id=$1 FOR UPDATE`, id).Scan(&enabled); err != nil {
				return zero, err
			}
			if !enabled {
				return zero, conflict("不能选择已禁用配置")
			}
		}
		var result domain.ModelSettings
		err := tx.QueryRow(ctx, `UPDATE model_settings SET revision=revision+1,default_config_id=NULLIF($1,'') WHERE singleton RETURNING revision,COALESCE(default_config_id,'')`, id).Scan(&result.Revision, &result.DefaultConfigID)
		return result, err
	})
}
