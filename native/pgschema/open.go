package pgschema

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
)

// 连接池应用名（§1）；PG 侧 pg_stat_activity 可据此区分。
const (
	AppNative  = "wscyber-hub-native"
	AppWebAPI  = "wscyber-hub-webapi"
	AppControl = "wscyber-hub-control"
)

// PoolOptions 是连接池参数；零值字段取 §1 默认值。
type PoolOptions struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DefaultPoolOptions 是每个运行池的默认参数（§1：8/4/30m/5m，两池合计 ≤16 连接）。
var DefaultPoolOptions = PoolOptions{MaxOpenConns: 8, MaxIdleConns: 4, ConnMaxLifetime: 30 * time.Minute, ConnMaxIdleTime: 5 * time.Minute}

// OpenDB 用 pgx/v5/stdlib + pgdialect 打开连接池。会话参数固定
// application_name=<application>、timezone=UTC、lock_timeout=10s（§1、§5.2），
// 不覆盖连接串里的其它参数。只建池不连库；调用方随后 PingContext 或 CheckRole。
func OpenDB(u DatabaseURL, application string, opts PoolOptions) (*bun.DB, error) {
	if u.IsZero() {
		return nil, errors.New("PostgreSQL 连接串未配置")
	}
	cfg, err := pgx.ParseConfig(u.dsn)
	if err != nil {
		return nil, errors.New("PostgreSQL 连接串无法解析（内容不回显）")
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = map[string]string{}
	}
	if application != "" {
		cfg.RuntimeParams["application_name"] = application
	}
	cfg.RuntimeParams["timezone"] = "UTC"
	cfg.RuntimeParams["lock_timeout"] = "10s"
	sqldb := stdlib.OpenDB(*cfg)
	opts = opts.withDefaults()
	sqldb.SetMaxOpenConns(opts.MaxOpenConns)
	sqldb.SetMaxIdleConns(opts.MaxIdleConns)
	sqldb.SetConnMaxLifetime(opts.ConnMaxLifetime)
	sqldb.SetConnMaxIdleTime(opts.ConnMaxIdleTime)
	return bun.NewDB(sqldb, pgdialect.New()), nil
}

func (o PoolOptions) withDefaults() PoolOptions {
	if o.MaxOpenConns <= 0 {
		o.MaxOpenConns = DefaultPoolOptions.MaxOpenConns
	}
	if o.MaxIdleConns <= 0 {
		o.MaxIdleConns = min(DefaultPoolOptions.MaxIdleConns, o.MaxOpenConns)
	}
	if o.ConnMaxLifetime <= 0 {
		o.ConnMaxLifetime = DefaultPoolOptions.ConnMaxLifetime
	}
	if o.ConnMaxIdleTime <= 0 {
		o.ConnMaxIdleTime = DefaultPoolOptions.ConnMaxIdleTime
	}
	return o
}

// RoleAttributes 是当前连接角色与 RLS 相关的属性。
type RoleAttributes struct {
	Name      string
	Superuser bool
	BypassRLS bool
}

// ErrUnsafeRole 表示连接角色会绕过行级安全（§4.3），拒绝启动。
var ErrUnsafeRole = errors.New("PostgreSQL 角色会绕过行级安全")

// QueryRoleAttributes 只读查询当前角色的 rolsuper/rolbypassrls。
func QueryRoleAttributes(ctx context.Context, db bun.IConn) (RoleAttributes, error) {
	var attrs RoleAttributes
	err := db.QueryRowContext(ctx, `SELECT rolname, rolsuper, rolbypassrls FROM pg_roles WHERE rolname = current_user`).
		Scan(&attrs.Name, &attrs.Superuser, &attrs.BypassRLS)
	if err != nil {
		return RoleAttributes{}, fmt.Errorf("查询 PostgreSQL 角色属性: %w", err)
	}
	return attrs, nil
}

// CheckRoleAttributes 拒绝超级用户与 BYPASSRLS 角色（超级用户无视 FORCE ROW LEVEL SECURITY）。
func CheckRoleAttributes(attrs RoleAttributes) error {
	switch {
	case attrs.Superuser:
		return fmt.Errorf("%w：%q 是超级用户；请改用非超级用户、无 BYPASSRLS 的库属主角色", ErrUnsafeRole, attrs.Name)
	case attrs.BypassRLS:
		return fmt.Errorf("%w：%q 具有 BYPASSRLS；请改用无 BYPASSRLS 的角色", ErrUnsafeRole, attrs.Name)
	}
	return nil
}

// CheckRole 查询并检查当前角色。
func CheckRole(ctx context.Context, db bun.IConn) error {
	attrs, err := QueryRoleAttributes(ctx, db)
	if err != nil {
		return err
	}
	return CheckRoleAttributes(attrs)
}

// advisory lock 用两个 int4 的形式，第一段是 wscyber 命名空间（"wscy"）。
const (
	lockNamespace   int32 = 0x77736379
	lockInstanceKey int32 = 1
	lockMigrateKey  int32 = 2
)

// ErrInstanceLocked 表示同一数据库已有另一个 Hub 持有单实例锁。
var ErrInstanceLocked = errors.New("同一数据库已有 wscyber Hub 在运行（单实例锁被占用）")

// InstanceLock 是会话级 advisory lock，持有一条专用连接直到 Release。
// 锁随连接存在：连接断开（PG 重启、网络中断）锁即失效，Lost() 可用于探测。
type InstanceLock struct {
	conn *sql.Conn
}

// AcquireInstanceLock 从池里占用一条连接并 pg_try_advisory_lock；
// 已被占用时返回 ErrInstanceLocked（不等待）。advisory lock 按库隔离。
func AcquireInstanceLock(ctx context.Context, db *sql.DB) (*InstanceLock, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("取得单实例锁连接: %w", err)
	}
	var ok bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1, $2)`, lockNamespace, lockInstanceKey).Scan(&ok); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("获取单实例锁: %w", err)
	}
	if !ok {
		_ = conn.Close()
		return nil, ErrInstanceLocked
	}
	return &InstanceLock{conn: conn}, nil
}

// Check 确认锁连接仍存活且仍持有锁。
func (l *InstanceLock) Check(ctx context.Context) error {
	if l == nil || l.conn == nil {
		return errors.New("单实例锁已释放")
	}
	var held bool
	err := l.conn.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND pid = pg_backend_pid() AND classid = $1::int::oid AND objid = $2::int::oid AND objsubid = 2 AND granted)`,
		lockNamespace, lockInstanceKey).Scan(&held)
	if err != nil {
		return fmt.Errorf("检查单实例锁: %w", err)
	}
	if !held {
		return errors.New("单实例锁已丢失")
	}
	return nil
}

// Release 释放锁并归还（关闭）专用连接；可重复调用。
func (l *InstanceLock) Release() error {
	if l == nil || l.conn == nil {
		return nil
	}
	conn := l.conn
	l.conn = nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, unlockErr := conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1, $2)`, lockNamespace, lockInstanceKey)
	discardConn(conn)
	return unlockErr
}

// discardConn 关闭物理连接而不是归还池：会话级 advisory lock 随会话结束释放，
// 即使解锁语句失败也不会把仍持锁的连接留在池里。
func discardConn(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}
