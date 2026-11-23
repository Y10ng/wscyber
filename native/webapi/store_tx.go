package webapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/uptrace/bun"

	webservice "github.com/chainreactors/cyber/pkg/web/service"
)

// webapi 存储的事务与身份助手（native/POSTGRES.md §4.4、§5.3）。
//
// 每次访问都在自己的事务里执行，事务首句按 ctx 身份
// set_config('app.role'/'app.tenant_id'/'app.user_id', …, true)：is_local=true 只在本
// 事务有效，连接归还池后不残留。ctx 无身份时与上游 overlay 存储一致，视为
// system + 默认租户 "default"（P5 收紧）。
//
// 并发语义：业务代码（claim 的同指纹未决检查、MAX(number)+1 取号等）
// 以所有 webapi 写事务全局串行为正确性前提。PG READ COMMITTED 下并发事务互不阻塞，
// 所以 write 默认先取全局事务级 advisory lock webapiWriteLock，精确复现单写者
// 串行；取锁后的每条语句都看得到之前所有已提交写事务。读不加锁，走 READ ONLY
// 事务（每条语句读最新已提交数据，与原先“读排在写之前或之后”可观察等价）。
//
// 约束：read/write 的回调里不得再调用 s.read/s.write（另取连接：读看不到本事务
// 未提交的写；写会等本事务持有的全局锁直到 lock_timeout）。需要复用的查询写成
// 接收 bun.IDB / bun.Tx 的函数。

// webapiWriteLock 是 P3 的全局写锁键；P5 收窄为每租户键（§5.3 第 3 条）。
// 与上游 overlay 的 wscyber:aop:<session>、wscyber:raw_artifacts 同一命名空间、互不重叠。
const webapiWriteLock = "wscyber:webapi:write"

const (
	identityRoleAdmin  = "admin"
	identityRoleUser   = "user"
	identityRoleSystem = "system"
	// identityDefaultTenant 是基线种子里的默认租户 ID。
	identityDefaultTenant = "default"
)

// storeIdentity 解析本次访问实际使用的身份并校验取值（规则与上游 overlay 一致）。
func storeIdentity(ctx context.Context) (webservice.Identity, error) {
	identity, ok := webservice.IdentityFrom(ctx)
	if !ok {
		return webservice.Identity{TenantID: identityDefaultTenant, Role: identityRoleSystem}, nil
	}
	identity.TenantID = strings.TrimSpace(identity.TenantID)
	identity.UserID = strings.TrimSpace(identity.UserID)
	switch identity.Role {
	case identityRoleAdmin, identityRoleSystem:
	case identityRoleUser:
		if identity.TenantID == "" {
			return webservice.Identity{}, fmt.Errorf("webapi 存储身份：角色 %q 必须带租户", identity.Role)
		}
	default:
		return webservice.Identity{}, fmt.Errorf("webapi 存储身份：未知角色 %q", identity.Role)
	}
	return identity, nil
}

// inTx 开事务、设置身份、按序取 advisory lock，再执行 fn；fn 出错或 panic 时回滚。
func (s *taskStore) inTx(ctx context.Context, opts *sql.TxOptions, locks []string, fn func(context.Context, bun.Tx) error) error {
	identity, err := storeIdentity(ctx)
	if err != nil {
		return err
	}
	return s.orm.RunInTx(ctx, opts, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`SELECT set_config('app.role', ?, true), set_config('app.tenant_id', ?, true), set_config('app.user_id', ?, true)`,
			identity.Role, identity.TenantID, identity.UserID); err != nil {
			return fmt.Errorf("设置 webapi 存储身份: %w", err)
		}
		for _, key := range locks {
			if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, key); err != nil {
				return fmt.Errorf("获取 webapi 写锁: %w", err)
			}
		}
		return fn(ctx, tx)
	})
}

// read 在 READ ONLY 事务里执行只读访问，不加锁。
func (s *taskStore) read(ctx context.Context, fn func(ctx context.Context, db bun.IDB) error) error {
	return s.inTx(ctx, &sql.TxOptions{ReadOnly: true}, nil, func(ctx context.Context, tx bun.Tx) error {
		return fn(ctx, tx)
	})
}

// write 在读写事务里执行写访问。locks 为空时取全局写锁 webapiWriteLock（P3 的
// 默认，所有写入口都这样调用）；非空时按字典序去重后依次取，避免多键死锁。
func (s *taskStore) write(ctx context.Context, locks []string, fn func(ctx context.Context, tx bun.Tx) error) error {
	if len(locks) == 0 {
		locks = []string{webapiWriteLock}
	} else {
		locks = slices.Compact(slices.Sorted(slices.Values(locks)))
	}
	return s.inTx(ctx, nil, locks, fn)
}

// readValue 是 read 的取值形式：fn 的结果在事务成功提交后返回。
func readValue[T any](ctx context.Context, s *taskStore, fn func(ctx context.Context, db bun.IDB) (T, error)) (T, error) {
	var value T
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		var err error
		value, err = fn(ctx, db)
		return err
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return value, nil
}

// readOptional 是 findOptional 的独立只读事务形式。
func readOptional[M any](ctx context.Context, s *taskStore, where string, args ...any) (*M, error) {
	return readValue(ctx, s, func(ctx context.Context, db bun.IDB) (*M, error) {
		return findOptional[M](ctx, db, where, args...)
	})
}

// queryRows 在独立只读事务里执行原生 SQL（bun 格式化 ? 占位），逐行调用 scan。
func (s *taskStore) queryRows(ctx context.Context, scan func(*sql.Rows) error, query string, args ...any) error {
	return s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		return scanRows(db.QueryContext(ctx, query, args...))(scan)
	})
}

// scanRows 把 QueryContext 的结果包装成逐行扫描（扫描完关闭 rows 并返回 rows.Err）。
func scanRows(rows *sql.Rows, err error) func(scan func(*sql.Rows) error) error {
	return func(scan func(*sql.Rows) error) error {
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
}

// isMissingParent 判断插入被基线租户继承触发器以“父行不存在”（WT003）拒绝。
// PG 触发器在 BEFORE INSERT 即拒绝（ON CONFLICT DO NOTHING 也先触发），
// 调用处据此映射回原来的未找到错误，保持对外行为（404）不变。
func isMissingParent(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "WT003"
}
