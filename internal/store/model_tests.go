package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"wscyber/internal/domain"
	"wscyber/internal/modelconfig"
)

var _ domain.ModelTestRepository = (*Store)(nil)

const modelTestColumns = `id,config_id,config_revision,status,created_at,started_at,completed_at,COALESCE(outcome,''),http_status,attempted`

func scanModelTest(row scanner) (domain.ModelTest, error) {
	var v domain.ModelTest
	err := row.Scan(&v.ID, &v.ConfigID, &v.ConfigRevision, &v.Status, &v.CreatedAt, &v.StartedAt, &v.CompletedAt, &v.Outcome, &v.HTTPStatus, &v.Attempted)
	return v, databaseError(err)
}

func (s *Store) GetModelTest(ctx context.Context, id string) (domain.ModelTest, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return scanModelTest(s.pool.QueryRow(ctx, `SELECT `+modelTestColumns+` FROM model_tests WHERE id=$1`, id))
}

func (s *Store) AcceptModelTest(ctx context.Context, configID, id string, revision int64, validate func(domain.SealedModelConfig) error) (domain.ModelTest, bool, error) {
	if !modelconfig.ValidID(configID) || !modelconfig.ValidID(id) || revision < 1 || revision > 1<<53-1 {
		return domain.ModelTest{}, false, invalid("测试身份无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	type acceptance struct {
		receipt domain.ModelTest
		created bool
	}
	v, err := transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (acceptance, error) {
		var zero acceptance
		if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
			return zero, err
		}
		// 与保存/禁用同序。亦串行所有测试身份与容量检查，包括跨配置同ID。
		if _, err := tx.Exec(ctx, `SELECT singleton FROM model_settings WHERE singleton FOR UPDATE`); err != nil {
			return zero, err
		}
		old, err := scanModelTest(tx.QueryRow(ctx, `SELECT `+modelTestColumns+` FROM model_tests WHERE id=$1`, id))
		if err == nil {
			if old.ConfigID != configID || old.ConfigRevision != revision {
				return zero, conflict("测试ID已用于其他配置或版本")
			}
			return acceptance{receipt: old}, nil
		}
		if !errors.Is(err, domain.ErrNotFound) {
			return zero, err
		}
		current, err := scanModelConfig(tx.QueryRow(ctx, `SELECT `+modelConfigColumns+` FROM model_configs c
			JOIN model_config_versions v ON v.config_id=c.id AND v.revision=c.current_revision WHERE c.id=$1 FOR UPDATE OF c`, configID))
		if err != nil {
			return zero, err
		}
		if current.Revision != revision || !current.Enabled {
			return zero, conflict("配置版本已变化或已禁用，请重新读取")
		}
		if validate == nil {
			return zero, domain.ErrUnavailable
		}
		if err := validate(current); err != nil {
			return zero, err
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM model_tests WHERE status <> 'completed'`).Scan(&count); err != nil {
			return zero, err
		}
		if count >= domain.ModelTestCapacity {
			return zero, domain.ErrUnavailable
		}
		receipt, err := scanModelTest(tx.QueryRow(ctx, `INSERT INTO model_tests(id,config_id,config_revision) VALUES($1,$2,$3) RETURNING `+modelTestColumns, id, configID, revision))
		return acceptance{receipt: receipt, created: true}, err
	})
	return v.receipt, v.created, err
}

func (s *Store) ClaimModelTest(ctx context.Context) (domain.ModelTest, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (domain.ModelTest, error) {
		return scanModelTest(tx.QueryRow(ctx, `UPDATE model_tests SET status='running',started_at=clock_timestamp()
			WHERE id=(SELECT id FROM model_tests WHERE status='pending' ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
			AND status='pending' RETURNING `+modelTestColumns))
	})
}

func (s *Store) CompleteModelTest(ctx context.Context, id, outcome string, httpStatus *int, attempted *bool) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.mutate(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE model_tests SET status='completed',completed_at=clock_timestamp(),outcome=$2,http_status=$3,attempted=$4
			WHERE id=$1 AND status='running'`, id, outcome, httpStatus, attempted)
		if err == nil && tag.RowsAffected() != 1 {
			return conflict("测试已结束或不在执行状态，不能覆盖回执")
		}
		return err
	})
}

func interruptModelTests(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE model_tests SET status='completed',completed_at=clock_timestamp(),
		outcome='transport_unknown',http_status=NULL,attempted=NULL WHERE status <> 'completed'`)
	return err
}

// 仅在worker已退出或启动恢复时调用；不重放任何未完成请求。
func (s *Store) InterruptModelTests(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.mutate(ctx, func(tx pgx.Tx) error { return interruptModelTests(ctx, tx) })
}
