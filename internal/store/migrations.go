package store

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// schema.sql 固定为版本 1；后续结构变化只追加迁移，不修改已发布版本。
//
//go:embed migrations/002_operations.sql
var operationsSchema string

//go:embed migrations/003_model_configs.sql
var modelConfigsSchema string

//go:embed migrations/004_model_tests.sql
var modelTestsSchema string

//go:embed migrations/005_mission_models.sql
var missionModelsSchema string

//go:embed migrations/006_model_calls.sql
var modelCallsSchema string

//go:embed migrations/007_research_materials.sql
var researchMaterialsSchema string

//go:embed migrations/008_automatic_skills.sql
var automaticSkillsSchema string

//go:embed migrations/009_research_briefs.sql
var researchBriefsSchema string

//go:embed migrations/010_research_controls.sql
var researchControlsSchema string

//go:embed migrations/011_context_assembly.sql
var contextAssemblySchema string

//go:embed migrations/012_findings.sql
var findingsSchema string

//go:embed migrations/013_verifications.sql
var verificationsSchema string

type migration struct {
	version int
	sql     string
}

var migrations = []migration{{1, schema}, {2, operationsSchema}, {3, modelConfigsSchema}, {4, modelTestsSchema}, {5, missionModelsSchema}, {6, modelCallsSchema}, {7, researchMaterialsSchema}, {8, automaticSkillsSchema}, {9, researchBriefsSchema}, {10, researchControlsSchema}, {11, contextAssemblySchema}, {12, findingsSchema}, {13, verificationsSchema}}

// migrate 在已有单实例锁下执行；所有待应用迁移同事务，失败不留部分结构。
// 无版本表的旧安装先采用幂等版本 1，不删除或重建业务表。
func (s *Store) migrate(ctx context.Context) error {
	return s.mutate(ctx, func(tx pgx.Tx) error { return applyMigrations(ctx, tx, migrations) })
}

func applyMigrations(ctx context.Context, tx pgx.Tx, list []migration) error {
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '2s'`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS wscyber_schema_migrations (
		version integer PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT clock_timestamp()
	)`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT version, checksum FROM wscyber_schema_migrations ORDER BY version`)
	if err != nil {
		return err
	}
	applied := 0
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			rows.Close()
			return err
		}
		if applied >= len(list) || version != list[applied].version || checksum != fmt.Sprintf("%x", sha256.Sum256([]byte(list[applied].sql))) {
			rows.Close()
			return fmt.Errorf("数据库迁移版本或校验值不匹配，拒绝启动；请使用匹配版本或从备份恢复")
		}
		applied++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, m := range list[applied:] {
		if _, err := tx.Exec(ctx, m.sql); err != nil {
			return fmt.Errorf("应用数据库迁移 %d: %w", m.version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO wscyber_schema_migrations(version, checksum) VALUES ($1, $2)`, m.version, fmt.Sprintf("%x", sha256.Sum256([]byte(m.sql)))); err != nil {
			return err
		}
	}
	return nil
}
