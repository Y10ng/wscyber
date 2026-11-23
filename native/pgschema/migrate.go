package pgschema

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/migrate"
)

// 迁移表名（§3）。wscyber_migration_locks 由 bun Migrator.Init 建出但不使用：
// 迁移互斥用 pg_advisory_lock（插行锁在崩溃时会残留）。
const (
	MigrationsTable     = "wscyber_migrations"
	MigrationLocksTable = "wscyber_migration_locks"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// ErrDatabaseNewer 表示库里已应用了本二进制不认识的迁移（库比二进制新），拒绝启动。
var ErrDatabaseNewer = errors.New("数据库 schema 比本程序新")

// ErrSchemaNotCurrent 表示库里还有未应用的迁移（RequireCurrent 使用）。
var ErrSchemaNotCurrent = errors.New("数据库 schema 不是本程序期望的版本")

// Migrations 返回内嵌迁移集合。每个文件都必须是 .tx.up.sql：bun 对它整体开事务，
// 运行器在同一事务里登记版本，迁移要么完整生效并登记、要么都不发生。
func Migrations() (*migrate.Migrations, error) {
	sub, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".down.sql") {
			return nil, fmt.Errorf("迁移 %s：不提供 down 迁移（回滚＝恢复备份）", name)
		}
		if strings.HasSuffix(name, ".up.sql") && !strings.HasSuffix(name, ".tx.up.sql") {
			return nil, fmt.Errorf("迁移 %s 必须命名为 .tx.up.sql（整体在一个事务内执行）", name)
		}
	}
	migrations := migrate.NewMigrations()
	if err := migrations.Discover(sub); err != nil {
		return nil, err
	}
	if len(migrations.Sorted()) == 0 {
		return nil, errors.New("没有内嵌迁移")
	}
	return migrations, nil
}

// KnownMigrations 返回本二进制内嵌的迁移版本（升序）。
func KnownMigrations() ([]string, error) {
	migrations, err := Migrations()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, m := range migrations.Sorted() {
		names = append(names, m.Name)
	}
	return names, nil
}

// SchemaVersion 返回本二进制内嵌迁移的最大版本号（文件名数字前缀，0001 → 1），
// 即 `wscyber-hub version` 的 schema 字段。内嵌迁移无效时返回 0
// （此时 Start/Migrate 会以明确错误失败，不会带着错误版本运行）。
func SchemaVersion() int {
	names, err := KnownMigrations()
	if err != nil || len(names) == 0 {
		return 0
	}
	version, err := strconv.Atoi(names[len(names)-1])
	if err != nil || version < 0 {
		return 0
	}
	return version
}

// MigrateResult 记录一次 Migrate 实际应用的迁移。
type MigrateResult struct {
	Applied []string
}

// Migrate 在 db 上应用未应用的内嵌迁移：
//  1. 占用一条连接取会话级 pg_advisory_lock（多个进程同时迁移时串行等待，ctx 可取消）；
//  2. 建迁移表（IF NOT EXISTS）；
//  3. 库里有本二进制不认识的版本 → ErrDatabaseNewer；
//  4. 逐个在事务内执行迁移并在同一事务里登记版本。
//
// 已是最新时什么都不做（幂等）。db 的 MaxOpenConns 须为 0 或 ≥2（锁连接 + 迁移连接）。
func Migrate(ctx context.Context, db *bun.DB) (MigrateResult, error) {
	if max := db.Stats().MaxOpenConnections; max == 1 {
		return MigrateResult{}, errors.New("迁移需要连接池至少 2 条连接（锁连接 + 迁移连接）")
	}
	lockConn, err := db.DB.Conn(ctx)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("取得迁移锁连接: %w", err)
	}
	// discardConn 关闭会话即释放锁（即使显式解锁失败）。
	defer discardConn(lockConn)
	return migrateLocked(ctx, db, lockConn, "")
}

// migrateLocked 在 lockConn 上取会话级迁移锁后应用迁移，返回前显式解锁；
// lockConn 由调用方提供并负责关闭（Start 复用单实例锁连接，使控制池只需 2 条连接）。
// 迁移本身在 db 池的另一条连接上执行。upTo 非空时只应用版本 ≤ upTo 的迁移（仅测试用：
// 构造“停在旧版本、已有数据”的库，再验证后续迁移在其上应用）。
func migrateLocked(ctx context.Context, db *bun.DB, lockConn *sql.Conn, upTo string) (MigrateResult, error) {
	migrations, err := Migrations()
	if err != nil {
		return MigrateResult{}, err
	}
	if _, err := lockConn.ExecContext(ctx, `SELECT pg_advisory_lock($1, $2)`, lockNamespace, lockMigrateKey); err != nil {
		return MigrateResult{}, fmt.Errorf("获取迁移锁: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = lockConn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock($1, $2)`, lockNamespace, lockMigrateKey)
	}()

	migrator := migrate.NewMigrator(db, migrations,
		migrate.WithTableName(MigrationsTable),
		migrate.WithLocksTableName(MigrationLocksTable),
		migrate.AfterMigration(recordMigration),
	)
	if err := migrator.Init(ctx); err != nil {
		return MigrateResult{}, fmt.Errorf("建迁移表: %w", err)
	}
	all, err := migrator.MigrationsWithStatus(ctx)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("读取迁移状态: %w", err)
	}
	unknown, err := migrator.MissingMigrations(ctx)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("读取迁移状态: %w", err)
	}
	if len(unknown) > 0 {
		return MigrateResult{}, fmt.Errorf("%w：库里已应用 %s，本程序只认识到 %s；请换用更新的程序或从备份恢复",
			ErrDatabaseNewer, migrationNames(unknown), lastName(all))
	}
	var result MigrateResult
	groupID := all.LastGroupID() + 1
	for i := range all {
		migration := &all[i]
		if migration.IsApplied() || upTo != "" && migration.Name > upTo {
			continue
		}
		migration.GroupID = groupID
		if migration.Up == nil {
			return result, fmt.Errorf("迁移 %s 缺少 up 文件", migration.Name)
		}
		if err := migration.Up(ctx, migrator, migration); err != nil {
			return result, fmt.Errorf("应用迁移 %s_%s: %w", migration.Name, migration.Comment, err)
		}
		result.Applied = append(result.Applied, migration.Name)
	}
	return result, nil
}

// recordMigration 在迁移自己的事务里登记版本（bun 默认在事务外先登记/后登记，
// 崩溃时会出现“已登记未执行”或“已执行未登记”）。
func recordMigration(ctx context.Context, conn bun.IConn, migration *migrate.Migration) error {
	tx, ok := conn.(bun.Tx)
	if !ok {
		return fmt.Errorf("迁移 %s 不在事务内执行；文件须命名为 .tx.up.sql", migration.Name)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO `+MigrationsTable+` (name, group_id, migrated_at) VALUES (?, ?, now())`,
		migration.Name, migration.GroupID)
	return err
}

func migrationNames(ms migrate.MigrationSlice) string {
	names := make([]string, 0, len(ms))
	for _, m := range ms {
		names = append(names, m.Name)
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

func lastName(ms migrate.MigrationSlice) string {
	if len(ms) == 0 {
		return "(无)"
	}
	return ms[len(ms)-1].Name
}

// AppliedMigration 是迁移表中的一行。
type AppliedMigration struct {
	Name       string
	GroupID    int64
	MigratedAt time.Time
}

// SchemaStatus 是 `db status` 的只读结果。
type SchemaStatus struct {
	Initialized bool   // 迁移表是否存在
	Expected    string // 本二进制最新迁移版本
	Applied     []AppliedMigration
	Pending     []string // 本二进制有、库里未应用
	Unknown     []string // 库里已应用、本二进制不认识（库比二进制新）
}

// Current 报告库版本是否正好是本二进制期望的版本。
func (s SchemaStatus) Current() bool {
	return s.Initialized && len(s.Pending) == 0 && len(s.Unknown) == 0
}

// Status 只读查询迁移状态：不建表、不加锁、不写入。
func Status(ctx context.Context, db bun.IDB) (SchemaStatus, error) {
	known, err := KnownMigrations()
	if err != nil {
		return SchemaStatus{}, err
	}
	status := SchemaStatus{Expected: known[len(known)-1]}
	var exists bool
	if err := db.NewRaw(`SELECT to_regclass(?) IS NOT NULL`, "public."+MigrationsTable).Scan(ctx, &exists); err != nil {
		return SchemaStatus{}, fmt.Errorf("查询迁移表: %w", err)
	}
	if !exists {
		status.Pending = known
		return status, nil
	}
	status.Initialized = true
	rows, err := db.QueryContext(ctx, `SELECT name, group_id, migrated_at FROM `+MigrationsTable+` ORDER BY name, id`)
	if err != nil {
		return SchemaStatus{}, fmt.Errorf("读取迁移表: %w", err)
	}
	defer rows.Close()
	applied := map[string]bool{}
	for rows.Next() {
		var row AppliedMigration
		if err := rows.Scan(&row.Name, &row.GroupID, &row.MigratedAt); err != nil {
			return SchemaStatus{}, fmt.Errorf("读取迁移表: %w", err)
		}
		status.Applied = append(status.Applied, row)
		applied[row.Name] = true
	}
	if err := rows.Err(); err != nil {
		return SchemaStatus{}, fmt.Errorf("读取迁移表: %w", err)
	}
	for _, name := range known {
		if !applied[name] {
			status.Pending = append(status.Pending, name)
		}
	}
	for _, row := range status.Applied {
		if !slices.Contains(known, row.Name) && !slices.Contains(status.Unknown, row.Name) {
			status.Unknown = append(status.Unknown, row.Name)
		}
	}
	return status, nil
}

// RequireCurrent 供运行池（P2 原生存储、P3 webapi）打开后校验：库版本必须
// 正好是本二进制期望的版本，不建表、不迁移。
func RequireCurrent(ctx context.Context, db bun.IDB) error {
	status, err := Status(ctx, db)
	if err != nil {
		return err
	}
	if len(status.Unknown) > 0 {
		return fmt.Errorf("%w：库里已应用 %s", ErrDatabaseNewer, strings.Join(status.Unknown, ","))
	}
	if !status.Current() {
		return fmt.Errorf("%w：期望 %s，未应用 %s", ErrSchemaNotCurrent, status.Expected, strings.Join(status.Pending, ","))
	}
	return nil
}
