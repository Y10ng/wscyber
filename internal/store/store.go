// Package store 使用 PostgreSQL 保存研究状态，所有业务迁移与事件在同一事务中提交。
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"wscyber/internal/domain"
)

//go:embed schema.sql
var schema string

const (
	lockNamespace int32 = 0x57534359
	instanceLock  int32 = 1
	eventLock     int32 = 2
)

// Store 的锁连接不进入连接池，不会因归还池连接而丢失单实例约束。
// Close 应在调度和请求退出后调用；Recover 仅供主入口在启动调度前调用。
type Store struct {
	pool      *pgxpool.Pool
	lock      *pgx.Conn
	lockMu    sync.Mutex
	lockLost  bool
	closeOnce sync.Once
	cursorKey [32]byte
}

var _ domain.Repository = (*Store)(nil)

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, invalid("数据库地址不能为空")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// 不回显可能含密码的连接串。
		return nil, invalid("PostgreSQL 连接配置无效")
	}
	lock, err := pgx.ConnectConfig(ctx, config.ConnConfig.Copy())
	if err != nil {
		return nil, fmt.Errorf("连接 PostgreSQL: %w", err)
	}
	cleanupLock := func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = lock.Close(closeCtx)
	}
	var acquired bool
	if err = lock.QueryRow(ctx, `SELECT pg_try_advisory_lock($1, $2)`, lockNamespace, instanceLock).Scan(&acquired); err != nil {
		cleanupLock()
		return nil, fmt.Errorf("取得单实例锁: %w", err)
	}
	if !acquired {
		cleanupLock()
		return nil, conflict("数据库已由另一个 wscyber 实例持有")
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		cleanupLock()
		return nil, fmt.Errorf("创建 PostgreSQL 连接池: %w", err)
	}
	s := &Store{pool: pool, lock: lock}
	if _, err = rand.Read(s.cursorKey[:]); err != nil {
		s.Close()
		return nil, domain.ErrUnavailable
	}
	if err = s.migrate(ctx); err != nil {
		s.Close()
		return nil, fmt.Errorf("初始化 PostgreSQL schema: %w", err)
	}
	return s, nil
}

func (s *Store) Close() {
	s.closeOnce.Do(func() {
		// 等待连接池使用者先退出，再关闭持锁会话，避免另一进程提前接管。
		s.pool.Close()
		s.lockMu.Lock()
		defer s.lockMu.Unlock()
		s.lockLost = true
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.lock.Close(ctx)
	})
}

// 不自动重连持锁会话；失去锁的旧进程必须停止迁移，由新 Open/Recover 接管。
// 每次事务（包括每次认领）都实际查询专用连接，不能仅依赖建连时的布尔值。
func (s *Store) ensureInstanceLock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.lockMu.Lock()
	defer s.lockMu.Unlock()
	// 等待其他锁检查期间请求可能已经取消；不再触碰专用连接。
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.lockLost || s.lock.IsClosed() {
		s.lockLost = true
		return conflict("单实例锁连接已断开，禁止继续迁移；需要重新启动并恢复")
	}
	// pgx取消在途查询可能关闭连接；单个HTTP请求不能因此撤销全服务的持锁会话。
	// 只隔离调用者取消，内部5秒超时仍生效；真实连接/锁丢失仍保持fail-stop、不重连。
	checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var held bool
	err := s.lock.QueryRow(checkCtx, `SELECT EXISTS (
		SELECT 1 FROM pg_locks WHERE locktype = 'advisory' AND pid = pg_backend_pid()
		AND classid::bigint = $1::bigint AND objid::bigint = $2::bigint AND objsubid = 2 AND granted
	)`, lockNamespace, instanceLock).Scan(&held)
	if err != nil {
		if s.lock.IsClosed() {
			s.lockLost = true
		}
		return fmt.Errorf("确认单实例锁失败，未执行状态迁移: %w", err)
	}
	if !held {
		s.lockLost = true
		return conflict("单实例锁已丢失，禁止继续迁移；需要重新启动并恢复")
	}
	return ctx.Err()
}

type querier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type scanner interface {
	Scan(...any) error
}

func transaction[T any](ctx context.Context, s *Store, options pgx.TxOptions, fn func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	if err := s.ensureInstanceLock(ctx); err != nil {
		return zero, err
	}
	tx, err := s.pool.BeginTx(ctx, options)
	if err != nil {
		return zero, databaseError(err)
	}
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(rollbackCtx)
	}()
	value, err := fn(tx)
	if err != nil {
		return zero, databaseError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, databaseError(err)
	}
	return value, nil
}

func (s *Store) mutate(ctx context.Context, fn func(pgx.Tx) error) error {
	_, err := transaction(ctx, s, pgx.TxOptions{}, func(tx pgx.Tx) (struct{}, error) {
		return struct{}{}, fn(tx)
	})
	return err
}

func databaseError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var postgres *pgconn.PgError
	if errors.As(err, &postgres) {
		switch postgres.Code {
		case "23505", "40001", "40P01":
			return conflict("并发操作或已有记录与本次迁移冲突")
		case "23503", "23514", "23502", "22001", "22003":
			return invalid("记录归属、字段或状态约束不满足")
		}
	}
	return err
}

func invalid(message string) error  { return fmt.Errorf("%w: %s", domain.ErrInvalid, message) }
func conflict(message string) error { return fmt.Errorf("%w: %s", domain.ErrConflict, message) }

func newID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("生成记录标识: %w", err)
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16]), nil
}

// canonicalInput 对全部对象键排序，再计算 SHA256。请求只有字符串、对象，无浮点数歧义。
// 存储前完成序列化，调用方随后修改 Headers 也不会改变已审批内容。
func canonicalInput(input domain.ActionInput) ([]byte, string, error) {
	if strings.TrimSpace(input.Reason) == "" {
		return nil, "", invalid("动作目的不能为空")
	}
	switch input.Kind {
	case "http":
		if strings.TrimSpace(input.Method) == "" || strings.TrimSpace(input.URL) == "" || input.Command != "" {
			return nil, "", invalid("HTTP 动作参数无效")
		}
	case "local_command":
		if strings.TrimSpace(input.Command) == "" || input.Method != "" || input.URL != "" || len(input.Headers) != 0 || input.Body != "" {
			return nil, "", invalid("本地命令动作参数无效")
		}
	default:
		return nil, "", invalid("不支持的动作类型")
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, "", invalid("动作参数无法序列化")
	}
	var object map[string]any
	if err = json.Unmarshal(encoded, &object); err != nil {
		return nil, "", err
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(canonical)
	return canonical, hex.EncodeToString(sum[:]), nil
}

// 事件 ID 的分配在事务锁下串行化，避免先提交的大 ID 使增量查询漏掉晚提交的小 ID。
// 调用者必须先取得所需业务行锁；取得事件锁之后不再等待新的研究方向行锁。
func appendEvent(ctx context.Context, tx pgx.Tx, missionID, threadID, kind, message string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, $2)`, lockNamespace, eventLock); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO events(mission_id, thread_id, kind, message) VALUES ($1, NULLIF($2, ''), $3, $4)`, missionID, threadID, kind, message)
	return err
}

const missionColumns = `id, title, origin, goal, allowed_origins, created_at`
const threadColumns = `id, mission_id, title, question, rationale, next_step, status, priority, auto_run, step_count, created_at, updated_at, pause_requested`
const attemptColumns = `id, thread_id, status, summary, error, started_at, ended_at`
const actionColumns = `id, thread_id, COALESCE(attempt_id, ''), input, request_hash, status, result, error, decision_reason, resolved_at, resolution_note, created_at, updated_at`
const observationColumns = `id, thread_id, COALESCE(attempt_id, ''), COALESCE(action_id, ''), kind, content, created_at`

func scanMission(row scanner) (domain.Mission, error) {
	var m domain.Mission
	var origins []byte
	if err := row.Scan(&m.ID, &m.Title, &m.Origin, &m.Goal, &origins, &m.CreatedAt); err != nil {
		return m, databaseError(err)
	}
	if err := json.Unmarshal(origins, &m.AllowedOrigins); err != nil {
		return m, fmt.Errorf("读取授权 origins: %w", err)
	}
	if m.AllowedOrigins == nil {
		m.AllowedOrigins = []string{}
	}
	return m, nil
}

func scanThread(row scanner) (domain.Thread, error) {
	var t domain.Thread
	err := row.Scan(&t.ID, &t.MissionID, &t.Title, &t.Question, &t.Rationale, &t.NextStep, &t.Status, &t.Priority, &t.AutoRun, &t.StepCount, &t.CreatedAt, &t.UpdatedAt, &t.PauseRequested)
	return t, databaseError(err)
}

func scanAttempt(row scanner) (domain.Attempt, error) {
	var a domain.Attempt
	err := row.Scan(&a.ID, &a.ThreadID, &a.Status, &a.Summary, &a.Error, &a.StartedAt, &a.EndedAt)
	return a, databaseError(err)
}

func scanAction(row scanner) (domain.Action, error) {
	var a domain.Action
	var input, result []byte
	if err := row.Scan(&a.ID, &a.ThreadID, &a.AttemptID, &input, &a.RequestHash, &a.Status, &result, &a.Error, &a.DecisionReason, &a.ResolvedAt, &a.ResolutionNote, &a.CreatedAt, &a.UpdatedAt); err != nil {
		return a, databaseError(err)
	}
	if err := json.Unmarshal(input, &a.Input); err != nil {
		return a, fmt.Errorf("读取动作参数: %w", err)
	}
	if len(result) != 0 {
		if err := json.Unmarshal(result, &a.Result); err != nil {
			return a, fmt.Errorf("读取动作结果: %w", err)
		}
	}
	return a, nil
}

func scanObservation(row scanner) (domain.Observation, error) {
	var o domain.Observation
	err := row.Scan(&o.ID, &o.ThreadID, &o.AttemptID, &o.ActionID, &o.Kind, &o.Content, &o.CreatedAt)
	return o, databaseError(err)
}

func collect[T any](ctx context.Context, db querier, sql string, scan func(scanner) (T, error), args ...any) ([]T, error) {
	items := make([]T, 0)
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, databaseError(err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, databaseError(err)
	}
	return items, nil
}

func getMission(ctx context.Context, db querier, id string) (domain.Mission, error) {
	return scanMission(db.QueryRow(ctx, `SELECT `+missionColumns+` FROM missions WHERE id = $1`, id))
}

func getThread(ctx context.Context, db querier, id string) (domain.Thread, error) {
	return scanThread(db.QueryRow(ctx, `SELECT `+threadColumns+` FROM threads WHERE id = $1`, id))
}

func lockThread(ctx context.Context, tx pgx.Tx, id string) (domain.Thread, error) {
	return scanThread(tx.QueryRow(ctx, `SELECT `+threadColumns+` FROM threads WHERE id = $1 FOR UPDATE`, id))
}

// 所有动作/尝试写入先锁所属方向，再锁子记录；认领与人工操作遵守相同顺序。
func lockActionThread(ctx context.Context, tx pgx.Tx, id string) (domain.Thread, error) {
	return scanThread(tx.QueryRow(ctx, `SELECT `+threadColumns+` FROM threads
		WHERE id = (SELECT thread_id FROM actions WHERE id = $1) FOR UPDATE`, id))
}

func lockAttemptThread(ctx context.Context, tx pgx.Tx, id string) (domain.Thread, error) {
	return scanThread(tx.QueryRow(ctx, `SELECT `+threadColumns+` FROM threads
		WHERE id = (SELECT thread_id FROM attempts WHERE id = $1) FOR UPDATE`, id))
}

const unsettledPredicate = `(status IN ('pending', 'approved', 'running') OR (status = 'outcome_unknown' AND resolved_at IS NULL))`

func unsettledAction(ctx context.Context, tx pgx.Tx, threadID string) (*domain.Action, error) {
	a, err := scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+` FROM actions WHERE thread_id = $1 AND `+unsettledPredicate+` FOR UPDATE`, threadID))
	if errors.Is(err, domain.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func runningAttempt(ctx context.Context, tx pgx.Tx, threadID string) (bool, error) {
	var running bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM attempts WHERE thread_id = $1 AND status = 'running')`, threadID).Scan(&running)
	return running, err
}

func pauseRequested(ctx context.Context, tx pgx.Tx, threadID string) (bool, error) {
	var paused bool
	err := tx.QueryRow(ctx, `SELECT pause_requested FROM threads WHERE id = $1`, threadID).Scan(&paused)
	return paused, err
}
