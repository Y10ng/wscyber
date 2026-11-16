package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
	"wscyber/internal/modelledger"
)

const priceColumns = `id,source,model_config_id,protocol,model,revision,enabled,terms,created_at,updated_at`

func scanModelPrice(row scanner) (domain.ModelPrice, error) {
	var p domain.ModelPrice
	var terms []byte
	err := row.Scan(&p.ID, &p.Source, &p.ModelConfigID, &p.Protocol, &p.Model, &p.Revision, &p.Enabled, &terms, &p.CreatedAt, &p.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(terms, &p.PriceTerms)
	}
	return p, err
}
func (s *Store) ListModelPrices(ctx context.Context) ([]domain.ModelPrice, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return collect(ctx, s.pool, `SELECT `+priceColumns+` FROM model_prices ORDER BY created_at,id LIMIT 500`, scanModelPrice)
}
func (s *Store) SaveModelPrice(ctx context.Context, id string, in domain.ModelPriceWrite, match func(domain.SealedModelConfig) bool) (domain.ModelPrice, bool, error) {
	var zero domain.ModelPrice
	if !modelconfig.ValidID(id) {
		return zero, false, domain.ErrInvalid
	}
	if err := modelledger.ValidatePrice(in); err != nil {
		return zero, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	type saved struct {
		p       domain.ModelPrice
		created bool
	}
	r, err := transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (saved, error) {
		z := saved{}
		// 最多500个价格；同一锁同时约束容量、身份和CAS。
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1,$2)`, lockNamespace, int32(6)); err != nil {
			return z, err
		}
		old, err := scanModelPrice(tx.QueryRow(ctx, `SELECT `+priceColumns+` FROM model_prices WHERE id=$1 FOR UPDATE`, id))
		exists := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return z, err
		}
		if exists {
			if in.ExpectedRevision != old.Revision || old.ModelPriceKey != in.ModelPriceKey {
				return z, domain.ErrConflict
			}
		} else if in.ExpectedRevision != 0 {
			return z, domain.ErrConflict
		}
		if !exists {
			var count int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM model_prices`).Scan(&count); err != nil {
				return z, err
			}
			if count >= 500 {
				return z, conflict("价格条目已达500项上限")
			}
			if in.Source == "managed" {
				rows, err := tx.Query(ctx, `SELECT c.id,v.revision,c.enabled,v.ciphertext,v.created_at FROM model_configs c JOIN model_config_versions v ON v.config_id=c.id WHERE c.id=$1 ORDER BY v.revision DESC`, in.ModelConfigID)
				if err != nil {
					return z, err
				}
				matched := false
				for rows.Next() {
					r, e := scanModelConfig(rows)
					if e != nil {
						rows.Close()
						return z, e
					}
					if match != nil && match(r) {
						matched = true
						break
					}
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					return z, err
				}
				if !matched {
					return z, invalid("价格须匹配真实模型配置的当前或历史模型")
				}
			} else {
				var found bool
				err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM model_calls WHERE source='environment' AND protocol=$1 AND model=$2)`, in.Protocol, in.Model).Scan(&found)
				if err != nil {
					return z, err
				}
				if !found {
					return z, invalid("环境模型必须已有实际调用记录")
				}
			}
		}
		terms, _ := json.Marshal(in.PriceTerms)
		if exists {
			p, err := scanModelPrice(tx.QueryRow(ctx, `UPDATE model_prices SET terms=$2,enabled=$3,revision=revision+1,updated_at=clock_timestamp() WHERE id=$1 RETURNING `+priceColumns, id, terms, in.Enabled))
			return saved{p, false}, err
		}
		p, err := scanModelPrice(tx.QueryRow(ctx, `INSERT INTO model_prices(id,source,model_config_id,protocol,model,revision,enabled,terms) VALUES($1,$2,$3,$4,$5,1,$6,$7) RETURNING `+priceColumns, id, in.Source, in.ModelConfigID, in.Protocol, in.Model, in.Enabled, terms))
		return saved{p, true}, err
	})
	return r.p, r.created, err
}
