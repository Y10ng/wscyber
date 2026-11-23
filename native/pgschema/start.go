package pgschema

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

// Instance 是 Hub 启动阶段取得的数据库控制面：角色已检查、单实例锁已持有、
// schema 已迁移到本二进制版本。运行池（原生存储、webapi）另行 OpenDB。
type Instance struct {
	db   *bun.DB
	lock *InstanceLock
}

// Start 按 §1/§3 的启动顺序执行：打开控制池 → 角色检查（拒绝超级用户/BYPASSRLS）
// → 单实例锁（同库已有 Hub 即失败）→ 迁移（库较新即失败）。logf 只会收到
// 不含口令的字段；可为 nil。任何一步失败都释放已取得的资源。
func Start(ctx context.Context, u DatabaseURL, logf func(format string, args ...any)) (*Instance, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	db, err := OpenDB(u, AppControl, PoolOptions{MaxOpenConns: 3, MaxIdleConns: 1})
	if err != nil {
		return nil, err
	}
	instance := &Instance{db: db}
	fail := func(err error) (*Instance, error) {
		return nil, errors.Join(err, instance.Close())
	}
	pingCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		return fail(fmt.Errorf("连接 PostgreSQL（%s）失败: %w", u, err))
	}
	logf("PostgreSQL: %s（来源 %s）", u, u.Source())
	if err := CheckRole(ctx, db); err != nil {
		return fail(err)
	}
	lock, err := AcquireInstanceLock(ctx, db.DB)
	if err != nil {
		return fail(err)
	}
	instance.lock = lock
	result, err := Migrate(ctx, db)
	if err != nil {
		return fail(err)
	}
	if len(result.Applied) > 0 {
		logf("PostgreSQL schema 已迁移: %v", result.Applied)
	}
	if err := RequireCurrent(ctx, db); err != nil {
		return fail(err)
	}
	return instance, nil
}

// DB 返回控制池（只给 db status 等低频控制操作用；业务读写用各自运行池）。
func (i *Instance) DB() *bun.DB { return i.db }

// Lock 返回持有的单实例锁。
func (i *Instance) Lock() *InstanceLock { return i.lock }

// Close 释放单实例锁并关闭控制池；可重复调用。
func (i *Instance) Close() error {
	if i == nil {
		return nil
	}
	var errs []error
	if i.lock != nil {
		errs = append(errs, i.lock.Release())
		i.lock = nil
	}
	if i.db != nil {
		errs = append(errs, i.db.Close())
		i.db = nil
	}
	return errors.Join(errs...)
}
