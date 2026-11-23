package pgschema_test

import (
	"context"
	"database/sql"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"

	"wscyber/native/internal/testdb"
	"wscyber/native/webapi"
)

// 不是租户业务表、因此不要求 tenant_id 的表（§4.2）。
var tenantWhitelist = []string{"tenants", "user_sessions", "wscyber_migrations", "wscyber_migration_locks"}

// 基线新增的非模型表。
var extraTables = []string{"tenants", "users", "user_sessions", "webapi_counters", "wscyber_migrations", "wscyber_migration_locks"}

// tenant_id 允许为空的表：users（管理员不属租户）、审计（全局配置事件，§4.6）。
var nullableTenant = []string{"users", "webapi_audit_events"}

type columnInfo struct {
	dataType string
	nullable bool
	def      string
}

func loadColumns(t *testing.T, db *bun.DB) map[string]map[string]columnInfo {
	t.Helper()
	rows, err := db.QueryContext(context.Background(), `
SELECT c.table_name, c.column_name, c.data_type, c.is_nullable = 'YES', COALESCE(c.column_default, '')
FROM information_schema.columns c
JOIN information_schema.tables tb ON tb.table_schema = c.table_schema AND tb.table_name = c.table_name
WHERE c.table_schema = 'public' AND tb.table_type = 'BASE TABLE'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]map[string]columnInfo{}
	for rows.Next() {
		var table, column string
		var info columnInfo
		if err := rows.Scan(&table, &column, &info.dataType, &info.nullable, &info.def); err != nil {
			t.Fatal(err)
		}
		if out[table] == nil {
			out[table] = map[string]columnInfo{}
		}
		out[table][column] = info
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// upstreamCoreTables 从固定上游源码 pkg/web/service/store_schema.go 解析
// coreSchema.Tables（该变量未导出，测试直接读源码而不是手抄清单）。
func upstreamCoreTables(t *testing.T) map[string][]string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/chainreactors/cyber").Output()
	if err != nil {
		t.Fatalf("定位上游模块: %v", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatal("上游模块目录为空（模块未下载？）")
	}
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "pkg/web/service/store_schema.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	tables := map[string][]string{}
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "coreSchema" || len(spec.Values) != 1 {
			return true
		}
		lit := spec.Values[0].(*ast.CompositeLit)
		for _, elt := range lit.Elts {
			kv := elt.(*ast.KeyValueExpr)
			if key, ok := kv.Key.(*ast.Ident); !ok || key.Name != "Tables" {
				continue
			}
			for _, entry := range kv.Value.(*ast.CompositeLit).Elts {
				pair := entry.(*ast.KeyValueExpr)
				table, _ := strconv.Unquote(pair.Key.(*ast.BasicLit).Value)
				for _, c := range pair.Value.(*ast.CompositeLit).Elts {
					column, _ := strconv.Unquote(c.(*ast.BasicLit).Value)
					tables[table] = append(tables[table], column)
				}
			}
		}
		return false
	})
	if len(tables) != 4 {
		t.Fatalf("上游 coreSchema.Tables 解析出 %d 张表，期望 4: %v", len(tables), tables)
	}
	return tables
}

func TestBaselineMatchesModelDeclarations(t *testing.T) {
	db := testdb.New(t).Open(t, "schema-test")
	actual := loadColumns(t, db)

	declared := upstreamCoreTables(t)
	module := webapi.SchemaModule()
	if len(module.Tables) != 44 {
		t.Fatalf("webapi.SchemaModule 声明 %d 张表", len(module.Tables))
	}
	for table, columns := range module.Tables {
		declared[table] = columns
	}

	// 列 ⊇ 声明（§3：允许多出 tenant_id 等列）。
	for table, columns := range declared {
		got, ok := actual[table]
		if !ok {
			t.Errorf("基线缺表 %s", table)
			continue
		}
		for _, column := range columns {
			if _, ok := got[column]; !ok {
				t.Errorf("基线 %s 缺列 %s", table, column)
			}
		}
	}
	// 表集合 = 声明 ∪ 新增表，不多不少（防止基线里残留无人声明的表）。
	want := slices.Collect(func(yield func(string) bool) {
		for table := range declared {
			if !yield(table) {
				return
			}
		}
	})
	want = append(want, extraTables...)
	slices.Sort(want)
	var got []string
	for table := range actual {
		got = append(got, table)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("表集合不一致:\n got %v\nwant %v", got, want)
	}

	// 每张业务表都有 tenant_id（白名单除外），除允许为空的表外 NOT NULL。
	for table, columns := range actual {
		if slices.Contains(tenantWhitelist, table) {
			continue
		}
		info, ok := columns["tenant_id"]
		if !ok {
			t.Errorf("%s 缺 tenant_id", table)
			continue
		}
		if info.nullable != slices.Contains(nullableTenant, table) {
			t.Errorf("%s.tenant_id nullable=%v", table, info.nullable)
		}
	}
}

// webapi 模型的列类型与可空性须与基线一致：string→text、int/int64→bigint、
// bool→boolean、[]byte→bytea、sql.Null* 可空、其余 NOT NULL。
func TestBaselineColumnTypesMatchWebAPIModels(t *testing.T) {
	db := testdb.New(t).Open(t, "schema-types")
	actual := loadColumns(t, db)
	dialect := bun.NewDB(&sql.DB{}, pgdialect.New())
	for _, model := range webapi.SchemaModule().Models {
		table := dialect.Table(reflect.TypeOf(model).Elem())
		for _, field := range table.Fields {
			info, ok := actual[table.Name][field.Name]
			if !ok {
				t.Errorf("%s 缺列 %s", table.Name, field.Name)
				continue
			}
			want := goTypeToPG(field.IndirectType)
			if want == "" {
				t.Errorf("%s.%s 未知 Go 类型 %s", table.Name, field.Name, field.IndirectType)
				continue
			}
			if info.dataType != want {
				t.Errorf("%s.%s 类型 %s，模型 %s 期望 %s", table.Name, field.Name, info.dataType, field.IndirectType, want)
			}
			wantNullable := !field.NotNull && !field.IsPK
			if info.nullable != wantNullable {
				t.Errorf("%s.%s nullable=%v，期望 %v", table.Name, field.Name, info.nullable, wantNullable)
			}
		}
	}
}

func goTypeToPG(typ reflect.Type) string {
	switch typ {
	case reflect.TypeFor[sql.NullString]():
		return "text"
	case reflect.TypeFor[sql.NullInt64]():
		return "bigint"
	case reflect.TypeFor[sql.NullBool]():
		return "boolean"
	}
	switch typ.Kind() {
	case reflect.String:
		return "text"
	case reflect.Int, reflect.Int64:
		return "bigint"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice:
		if typ.Elem().Kind() == reflect.Uint8 {
			return "bytea"
		}
	}
	return ""
}

func TestBaselineTenantMachinery(t *testing.T) {
	ctx := context.Background()
	db := testdb.New(t).Open(t, "schema-tenant")
	actual := loadColumns(t, db)

	// 每张有 tenant_id 的表：根表（默认值取 app.tenant_id）或子表（继承触发器），二者恰居其一。
	var inherit []string
	rows, err := db.QueryContext(ctx, `SELECT event_object_table FROM information_schema.triggers WHERE trigger_name = 'wscyber_tenant_inherit' AND event_manipulation = 'INSERT'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		inherit = append(inherit, table)
	}
	rows.Close()
	for table, columns := range actual {
		info, ok := columns["tenant_id"]
		if !ok || table == "users" || table == "webapi_counters" {
			continue
		}
		hasDefault := strings.Contains(info.def, "wscyber_require_tenant()") || strings.Contains(info.def, "wscyber_current_tenant()")
		hasTrigger := slices.Contains(inherit, table)
		if hasDefault == hasTrigger {
			t.Errorf("%s: 默认值=%v 继承触发器=%v，须恰有其一", table, hasDefault, hasTrigger)
		}
	}

	// RLS：策略已建、但 P5 前不启用。
	var enabled []string
	if err := db.NewRaw(`SELECT COALESCE(array_agg(relname::text), '{}') FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relkind = 'r' AND (relrowsecurity OR relforcerowsecurity)`).Scan(ctx, pgdialect.Array(&enabled)); err != nil {
		t.Fatal(err)
	}
	if len(enabled) != 0 {
		t.Errorf("P1 不应启用 RLS: %v", enabled)
	}
	var unpolicied []string
	if err := db.NewRaw(`
SELECT COALESCE(array_agg(c.relname::text), '{}') FROM pg_class c
WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r'
  AND c.relname NOT IN ('wscyber_migrations', 'wscyber_migration_locks')
  AND NOT EXISTS (SELECT 1 FROM pg_policies p WHERE p.schemaname = 'public' AND p.tablename = c.relname)`).Scan(ctx, pgdialect.Array(&unpolicied)); err != nil {
		t.Fatal(err)
	}
	if len(unpolicied) != 0 {
		t.Errorf("这些表没有 RLS 策略: %v", unpolicied)
	}
}
