// wscyber overlay：整文件替换上游 pkg/web/service/store_schema.go（固定版本
// f4b1fb2f，上游源 sha256 见 native/patches/cyber/manifest.json）。
//
// PostgreSQL 版。表、索引、tenant_id 与触发器全部由 native/pgschema 的迁移
// 建出（Hub 启动时 pgschema.Start），存储层不建表、不迁移，只校验：
//   - wscyber_migrations 存在且至少有一条已应用版本；
//   - schemaUnion(modules) 声明的每张表、每一列都在 information_schema.columns
//     （⊇：允许 tenant_id 等多出的列与多出的表）。
//
// SchemaModule/SchemaIndex/coreSchema/schemaUnion 与上游一致（webapi 与
// scan_store.go 引用）；SchemaModule.Indexes 只作声明，索引由迁移负责。
// 文件名保留 SQLite 字样是为了让上游其它文件一行不改（补丁最小）。

package service

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/uptrace/bun"
)

// SchemaModule is one optional slice of the store schema. The core schema is
// always installed; a host adds modules (e.g. ScanSchema) at open time and the
// database must contain the union — tables are created by the wscyber
// migrations, never by the store.
type SchemaModule struct {
	Name    string
	Models  []any
	Indexes []SchemaIndex
	Tables  map[string][]string
}

// SchemaIndex is one declarative index. Expr carries expression indexes such
// as "created_at DESC"; otherwise Columns are indexed in order.
type SchemaIndex struct {
	Model   any
	Name    string
	Columns []string
	Expr    string
	Unique  bool
}

// createTableCustomizer lets a model declare constraints bun cannot infer,
// such as the composite-primary-key junction's foreign keys. Kept for the
// upstream scan_store.go implementation; the PostgreSQL store creates no tables.
type createTableCustomizer interface {
	customizeCreateTable(query *bun.CreateTableQuery) *bun.CreateTableQuery
}

// coreSchema is the host-neutral session/event/artifact storage every web
// host shares.
var coreSchema = SchemaModule{
	Name: "core",
	Models: []any{
		(*sessionModel)(nil),
		(*aopEventModel)(nil),
		(*requestLedgerModel)(nil),
		(*rawArtifactModel)(nil),
	},
	Indexes: []SchemaIndex{
		{Model: (*sessionModel)(nil), Name: "idx_sessions_updated", Expr: "updated_at DESC"},
		{Model: (*sessionModel)(nil), Name: "idx_sessions_node_id", Columns: []string{"node_id"}},
		{Model: (*aopEventModel)(nil), Name: "idx_aop_events_session", Columns: []string{"session_id", "cursor"}},
		{Model: (*aopEventModel)(nil), Name: "idx_aop_events_turn", Columns: []string{"turn_id"}},
		{Model: (*aopEventModel)(nil), Name: "idx_aop_events_event_id", Columns: []string{"session_id", "event_id"}, Unique: true},
		{Model: (*rawArtifactModel)(nil), Name: "idx_raw_artifacts_created", Columns: []string{"created_at"}},
	},
	Tables: map[string][]string{
		"aop_request_ledger": {"request_id", "method", "request_hash", "response_json", "created_at"},
		"chat_aop_events":    {"id", "session_id", "event_id", "cursor", "turn_id", "emitter", "sequence", "event_proto", "created_at"},
		"chat_sessions":      {"id", "node_id", "status", "archived", "title", "agent_name", "session_json", "created_at", "updated_at"},
		"raw_artifacts":      {"cursor", "event_id", "event_proto", "created_at"},
	},
}

// schemaUnion joins the core schema with the configured modules.
func schemaUnion(modules []SchemaModule) SchemaModule {
	union := SchemaModule{
		Name:    "core",
		Models:  slices.Clone(coreSchema.Models),
		Indexes: slices.Clone(coreSchema.Indexes),
		Tables:  map[string][]string{},
	}
	for table, columns := range coreSchema.Tables {
		union.Tables[table] = columns
	}
	for _, module := range modules {
		union.Name += "+" + module.Name
		union.Models = append(union.Models, module.Models...)
		union.Indexes = append(union.Indexes, module.Indexes...)
		for table, columns := range module.Tables {
			union.Tables[table] = columns
		}
	}
	return union
}

func (s SchemaModule) tableNames() []string {
	tables := make([]string, 0, len(s.Tables))
	for table := range s.Tables {
		tables = append(tables, table)
	}
	slices.Sort(tables)
	return tables
}

// schemaMigrationsTable 与 native/pgschema.MigrationsTable 一致（overlay 编译在
// 上游模块里，不能导入 wscyber/native/pgschema）。
const schemaMigrationsTable = "wscyber_migrations"

// requireMigrated 要求迁移表存在且至少有一条已应用版本。精确版本比对
// （库不比程序新、无未应用迁移）由 Hub 启动时的 pgschema.Start 负责。
func requireMigrated(ctx context.Context, db bun.IDB) error {
	var exists bool
	if err := db.NewRaw(`SELECT to_regclass(?) IS NOT NULL`, "public."+schemaMigrationsTable).Scan(ctx, &exists); err != nil {
		return fmt.Errorf("check wscyber migrations: %w", err)
	}
	if !exists {
		return fmt.Errorf("database is not migrated: table %s does not exist; run the wscyber migrations first", schemaMigrationsTable)
	}
	var applied int
	if err := db.NewRaw(`SELECT count(*) FROM ?`, bun.Ident(schemaMigrationsTable)).Scan(ctx, &applied); err != nil {
		return fmt.Errorf("check wscyber migrations: %w", err)
	}
	if applied == 0 {
		return fmt.Errorf("database is not migrated: %s has no applied migrations; run the wscyber migrations first", schemaMigrationsTable)
	}
	return nil
}

// validateSchema requires every declared table and column to exist in the
// public schema. Extra tables and columns (tenant_id, migration tables, …)
// are allowed: bun always writes explicit column lists.
func validateSchema(ctx context.Context, db bun.IDB, schema SchemaModule) error {
	actual, err := schemaColumns(ctx, db)
	if err != nil {
		return err
	}
	var missingTables []string
	for _, table := range schema.tableNames() {
		columns, ok := actual[table]
		if !ok {
			missingTables = append(missingTables, table)
			continue
		}
		var missing []string
		for _, column := range schema.Tables[table] {
			if !columns[column] {
				missing = append(missing, column)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("database does not match the latest schema: %s columns missing %v, want %v; apply the wscyber migrations", table, missing, schema.Tables[table])
		}
	}
	if len(missingTables) > 0 {
		return fmt.Errorf("database does not match the latest schema: tables %s missing (schema %s); apply the wscyber migrations", strings.Join(missingTables, ","), schema.Name)
	}
	return nil
}

// schemaColumns reads the public schema's base-table columns.
func schemaColumns(ctx context.Context, db bun.IDB) (map[string]map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `
SELECT c.table_name, c.column_name
FROM information_schema.columns c
JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
WHERE c.table_schema = 'public' AND t.table_type = 'BASE TABLE'`)
	if err != nil {
		return nil, fmt.Errorf("read database schema: %w", err)
	}
	defer rows.Close()
	columns := map[string]map[string]bool{}
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			return nil, fmt.Errorf("read database schema: %w", err)
		}
		if columns[table] == nil {
			columns[table] = map[string]bool{}
		}
		columns[table][column] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read database schema: %w", err)
	}
	return columns, nil
}
