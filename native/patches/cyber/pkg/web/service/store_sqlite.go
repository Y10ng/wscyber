// wscyber overlay：整文件替换上游 pkg/web/service/store_sqlite.go（固定版本
// f4b1fb2f，上游源 sha256 见 native/patches/cyber/manifest.json）。
//
// 这是 PostgreSQL 实现。类型名 SQLiteStore、构造函数 NewSQLiteStore、字段 orm、
// 方法 appendAOPEvent/archiveArtifactEvents 等全部保留上游名字，只为让上游其它
// 文件（service.go、events.go、agents.go、agents_mux.go、scan_store.go、
// exts/web/extension.go）一行不改——补丁面最小，名字里的 SQLite 不代表驱动。
//
// 与上游的差异（逐条见 var/p2-native-store/result.md）：
//   - pgx/v5 stdlib + pgdialect；连接参数与 native/pgschema.OpenDB 一致；
//   - 不建表、不迁移，只校验迁移已应用且声明的表/列存在（store_schema.go）；
//   - 每个读写方法在自己的事务里先 set_config 身份（store_pg_identity.go）；
//   - instr → strpos；文本排序显式 COLLATE "C"（按字节比较）；
//   - 事件游标与制品游标写入额外持事务级 advisory lock（见各处注释）。

package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	toolpb "github.com/chainreactors/cyber/aop/tool"
	types "github.com/chainreactors/cyber/core/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"google.golang.org/protobuf/encoding/protojson"
	protobuf "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// 连接参数与 native/pgschema.OpenDB(…, AppNative, DefaultPoolOptions) 一致
// （POSTGRES.md §1）。overlay 编译在上游模块里，不能导入 wscyber/native/pgschema。
const (
	storeApplicationName = "wscyber-hub-native"
	storeMaxOpenConns    = 8
	storeMaxIdleConns    = 4
	storeConnMaxLifetime = 30 * time.Minute
	storeConnMaxIdleTime = 5 * time.Minute
	storeOpenTimeout     = 15 * time.Second
)

// SQLiteStore is the Hub's native session/event/artifact store. Despite the
// upstream name it is backed by PostgreSQL (see the file header).
type SQLiteStore struct {
	db      *sql.DB
	orm     *bun.DB
	modules []SchemaModule
}

// NewSQLiteStore opens a PostgreSQL pool for dsn (a postgres:// URL or
// key=value string) and verifies that the wscyber migrations have been applied
// and that every table/column declared by the core schema and modules exists.
// It never creates or alters tables. Errors never echo dsn (it may carry a
// password).
func NewSQLiteStore(dsn string, modules ...SchemaModule) (*SQLiteStore, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("open postgres: connection string is empty")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("open postgres: connection string cannot be parsed (content not shown)")
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = map[string]string{}
	}
	cfg.RuntimeParams["application_name"] = storeApplicationName
	cfg.RuntimeParams["timezone"] = "UTC"
	cfg.RuntimeParams["lock_timeout"] = "10s"
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(storeMaxOpenConns)
	db.SetMaxIdleConns(storeMaxIdleConns)
	db.SetConnMaxLifetime(storeConnMaxLifetime)
	db.SetConnMaxIdleTime(storeConnMaxIdleTime)
	orm := bun.NewDB(db, pgdialect.New())
	ctx, cancel := context.WithTimeout(context.Background(), storeOpenTimeout)
	defer cancel()
	if err := initializeSchema(ctx, orm, schemaUnion(modules)); err != nil {
		_ = orm.Close()
		return nil, fmt.Errorf("verify postgres schema: %w", err)
	}
	return &SQLiteStore{db: db, orm: orm, modules: modules}, nil
}

// hasModule reports whether the store carries a module's tables.
func (s *SQLiteStore) hasModule(name string) bool {
	for _, module := range s.modules {
		if module.Name == name {
			return true
		}
	}
	return false
}

// initializeSchema only verifies: the wscyber migrations (run by the Hub via
// pgschema.Start before the store is opened) own every DDL statement.
func initializeSchema(ctx context.Context, orm *bun.DB, schema SchemaModule) error {
	if err := orm.PingContext(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	if err := requireMigrated(ctx, orm); err != nil {
		return err
	}
	return validateSchema(ctx, orm, schema)
}

// inTx runs fn in a transaction whose first statement installs the store
// identity as transaction-local settings (is_local=true): they vanish at
// COMMIT/ROLLBACK, so a pooled connection never carries them into the next use.
func (s *SQLiteStore) inTx(ctx context.Context, opts *sql.TxOptions, fn func(context.Context, bun.Tx) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	identity, err := storeIdentity(ctx)
	if err != nil {
		return err
	}
	return s.orm.RunInTx(ctx, opts, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`SELECT set_config('app.role', ?, true), set_config('app.tenant_id', ?, true), set_config('app.user_id', ?, true)`,
			identity.Role, identity.TenantID, identity.UserID); err != nil {
			return fmt.Errorf("set store identity: %w", err)
		}
		return fn(ctx, tx)
	})
}

// read runs fn in a READ ONLY transaction with the store identity set (RLS,
// once enabled in P5, filters by it).
func (s *SQLiteStore) read(ctx context.Context, fn func(context.Context, bun.Tx) error) error {
	return s.inTx(ctx, &sql.TxOptions{ReadOnly: true}, fn)
}

// write runs fn in a read-write transaction with the store identity set; new
// root rows take tenant_id from it.
func (s *SQLiteStore) write(ctx context.Context, fn func(context.Context, bun.Tx) error) error {
	return s.inTx(ctx, nil, fn)
}

// advisoryXactLock serializes writers of one logical sequence until the
// transaction ends. Keys are namespaced "wscyber:…" and hashed to bigint.
func advisoryXactLock(ctx context.Context, tx bun.Tx, key string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "wscyber:"+key)
	return err
}

func marshalProtoJSON(message protobuf.Message) (string, error) {
	if message == nil {
		return "", fmt.Errorf("protobuf message is required")
	}
	raw, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(message)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func unmarshalProtoJSON(raw string, message protobuf.Message, kind string) error {
	if err := protojson.Unmarshal([]byte(raw), message); err != nil {
		return fmt.Errorf("decode %s protojson: %w", kind, err)
	}
	return nil
}

func (s *SQLiteStore) LoadAOPRequest(ctx context.Context, requestID, method string, requestHash []byte, response protobuf.Message) (found, conflict bool, err error) {
	if s == nil || strings.TrimSpace(requestID) == "" || response == nil {
		return false, false, nil
	}
	var model requestLedgerModel
	err = s.read(ctx, func(ctx context.Context, tx bun.Tx) error {
		return tx.NewSelect().Model(&model).Where("request_id = ?", requestID).Limit(1).Scan(ctx)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if model.Method != method || !bytes.Equal(model.RequestHash, requestHash) {
		return false, true, nil
	}
	if err := unmarshalProtoJSON(model.ResponseJSON, response, "AOP response"); err != nil {
		return false, false, err
	}
	return true, false, nil
}

func (s *SQLiteStore) SaveAOPRequest(ctx context.Context, requestID, method string, requestHash []byte, response protobuf.Message) error {
	if s == nil || strings.TrimSpace(requestID) == "" || response == nil {
		return nil
	}
	raw, err := marshalProtoJSON(response)
	if err != nil {
		return err
	}
	return s.write(ctx, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(&requestLedgerModel{
			RequestID: requestID, Method: method, RequestHash: requestHash,
			ResponseJSON: raw, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}).Exec(ctx)
		return err
	})
}

func (s *SQLiteStore) Close() error {
	return s.orm.Close()
}

func formatProtoTime(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return time.Now().UTC().Format(time.RFC3339Nano)
	}
	return ts.AsTime().UTC().Format(time.RFC3339Nano)
}

func (s *SQLiteStore) CreateSession(ctx context.Context, session *types.SessionRecord) error {
	model, err := sessionToModel(session)
	if err != nil {
		return err
	}
	return s.write(ctx, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewInsert().Model(model).Exec(ctx)
		return err
	})
}

func (s *SQLiteStore) GetSession(ctx context.Context, id string) (*types.SessionRecord, error) {
	var session *types.SessionRecord
	err := s.read(ctx, func(ctx context.Context, tx bun.Tx) error {
		var model sessionModel
		if err := tx.NewSelect().Model(&model).Column("session_json").Where("id = ?", id).Limit(1).Scan(ctx); err != nil {
			return err
		}
		var err error
		session, err = sessionFromJSON(model.SessionJSON)
		if err != nil {
			return err
		}
		return s.attachSessionScans(ctx, tx, session)
	})
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (s *SQLiteStore) ListSessions(ctx context.Context, limit int) ([]*types.SessionRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	var sessions []*types.SessionRecord
	err := s.read(ctx, func(ctx context.Context, tx bun.Tx) error {
		var models []sessionModel
		if err := tx.NewSelect().Model(&models).Column("session_json").OrderExpr(`updated_at COLLATE "C" DESC`).Limit(int64(limit)).Scan(ctx); err != nil {
			return err
		}
		var err error
		sessions, err = sessionsFromModels(models)
		if err != nil {
			return err
		}
		for _, session := range sessions {
			if err := s.attachSessionScans(ctx, tx, session); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *SQLiteStore) ListSessionPage(ctx context.Context, offset, limit int, filters *types.ListSessionsRequest) ([]*types.SessionRecord, bool, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if filters == nil {
		filters = &types.ListSessionsRequest{}
	}
	if filters.Target != "" && !s.hasModule(ScanSchema.Name) {
		return nil, false, nil
	}
	var sessions []*types.SessionRecord
	var hasMore bool
	err := s.read(ctx, func(ctx context.Context, tx bun.Tx) error {
		// COLLATE "C" keeps byte-wise text ordering regardless of the
		// database's default collation.
		query := tx.NewSelect().Model((*sessionModel)(nil)).Column("session_json").
			OrderExpr(`updated_at COLLATE "C" DESC, id COLLATE "C" DESC`).Limit(int64(limit + 1)).Offset(int64(offset))
		if !filters.IncludeClosed {
			query = query.Where("status = ?", SessionStateOpen)
		}
		query = query.Where("archived = ?", filters.GetArchived())
		if filters.NodeId != "" {
			query = query.Where("node_id = ?", filters.NodeId)
		}
		if filters.Search != "" {
			if s.hasModule(ScanSchema.Name) {
				query = query.Where("(strpos(lower(title), lower(?)) > 0 OR EXISTS (SELECT 1 FROM session_scans ss JOIN scans sc ON sc.id = ss.scan_id WHERE ss.session_id = session.id AND strpos(lower(sc.target), lower(?)) > 0))", filters.Search, filters.Search)
			} else {
				query = query.Where("strpos(lower(title), lower(?)) > 0", filters.Search)
			}
		}
		if filters.Target != "" {
			query = query.Where("EXISTS (SELECT 1 FROM session_scans ss JOIN scans sc ON sc.id = ss.scan_id WHERE ss.session_id = session.id AND sc.target = ?)", filters.Target)
		}
		var models []sessionModel
		if err := query.Model(&models).Scan(ctx); err != nil {
			return err
		}
		hasMore = len(models) > limit
		if hasMore {
			models = models[:limit]
		}
		var err error
		sessions, err = sessionsFromModels(models)
		if err != nil {
			return err
		}
		for _, session := range sessions {
			if err := s.attachSessionScans(ctx, tx, session); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return sessions, hasMore, nil
}

func sessionsFromModels(models []sessionModel) ([]*types.SessionRecord, error) {
	sessions := make([]*types.SessionRecord, 0, len(models))
	for _, model := range models {
		session, err := sessionFromJSON(model.SessionJSON)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func sessionFromJSON(raw string) (*types.SessionRecord, error) {
	session := new(types.SessionRecord)
	// Older records stored a scan association in the shared message. The
	// association now comes from session_scans; ignore only that retired key.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, fmt.Errorf("decode session: %w", err)
	}
	delete(fields, "scan_ids")
	delete(fields, "scanIds")
	clean, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if err := unmarshalProtoJSON(string(clean), session, "session"); err != nil {
		return nil, err
	}
	return session, nil
}

func sessionToModel(session *types.SessionRecord) (*sessionModel, error) {
	if session == nil || session.GetSession() == nil {
		return nil, fmt.Errorf("session is required")
	}
	copy := protobuf.Clone(session).(*types.SessionRecord)
	delete(copy.Extensions, "scan") // session_scans is the source of this association.
	raw, err := marshalProtoJSON(copy)
	if err != nil {
		return nil, err
	}
	domain := session.GetSession()
	return &sessionModel{
		ID: domain.GetId(), NodeID: domain.GetNodeId(), Status: domain.GetState(),
		Archived: session.GetArchived(), Title: domain.GetTitle(), AgentName: session.GetAgentName(), SessionJSON: raw,
		CreatedAt: formatProtoTime(session.GetCreatedAt()), UpdatedAt: formatProtoTime(session.GetUpdatedAt()),
	}, nil
}

func (s *SQLiteStore) UpdateSession(ctx context.Context, session *types.SessionRecord) error {
	model, err := sessionToModel(session)
	if err != nil {
		return err
	}
	return s.write(ctx, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewUpdate().Model(model).
			Column("node_id", "status", "title", "archived", "agent_name", "session_json", "updated_at").WherePK().Exec(ctx)
		return err
	})
}

// attachSessionScans reads the scan association inside the caller's
// transaction (upstream went through SessionScanIDs on the shared pool).
func (s *SQLiteStore) attachSessionScans(ctx context.Context, db bun.IDB, session *types.SessionRecord) error {
	if !s.hasModule(ScanSchema.Name) {
		return nil
	}
	var ids []string
	if err := db.NewSelect().Model((*sessionScanModel)(nil)).Column("scan_id").
		Where("session_id = ?", session.GetSession().GetId()).Scan(ctx, &ids); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	values := make([]any, len(ids))
	for index, id := range ids {
		values[index] = id
	}
	var targets []string
	if err := db.NewSelect().Model((*scanModel)(nil)).ColumnExpr(`DISTINCT target COLLATE "C" AS target`).Where("id IN (?)", bun.List(ids)).OrderExpr("1 ASC").Scan(ctx, &targets); err != nil {
		return err
	}
	targetValues := make([]any, len(targets))
	for i, target := range targets {
		targetValues[i] = target
	}
	section, err := structpb.NewStruct(map[string]any{"ids": values, "targets": targetValues})
	if err != nil {
		return err
	}
	if session.Extensions == nil {
		session.Extensions = make(map[string]*structpb.Struct)
	}
	session.Extensions["scan"] = section
	return nil
}

func (s *SQLiteStore) DeleteSession(ctx context.Context, id string) error {
	return s.write(ctx, func(ctx context.Context, tx bun.Tx) error {
		_, err := tx.NewDelete().Model((*sessionModel)(nil)).Where("id = ?", id).Exec(ctx)
		return err
	})
}

func (s *SQLiteStore) AddAOPEvent(ctx context.Context, sessionID string, event *aop.Event) error {
	_, _, err := s.AppendAOPEvent(ctx, sessionID, event)
	return err
}

func (s *SQLiteStore) AppendAOPEvent(ctx context.Context, sessionID string, event *aop.Event) (int64, bool, error) {
	return s.appendAOPEvent(ctx, sessionID, event, 0)
}

// appendAOPEvent assigns the next per-session cursor as COALESCE(MAX)+1 inside
// one transaction. Correctness relies, as upstream, on the in-process
// Service.eventMu serializing appends (single Hub instance per database, held
// by the pgschema instance lock) plus the (session_id, cursor) unique
// constraint as the backstop. Under PostgreSQL's READ COMMITTED a concurrent
// writer outside eventMu would otherwise read the same MAX, so the transaction
// also holds a per-session advisory lock: appends to one session are
// serialized across connections and commit in cursor order.
func (s *SQLiteStore) appendAOPEvent(ctx context.Context, sessionID string, event *aop.Event, after int64) (cursor int64, persisted bool, err error) {
	if event == nil || event.GetMessageDelta() != nil || event.GetToolCallDelta() != nil {
		return 0, false, nil
	}
	if strings.TrimSpace(sessionID) == "" {
		return 0, false, fmt.Errorf("AOP event session_id is required")
	}
	if strings.TrimSpace(event.Id) == "" {
		return 0, false, fmt.Errorf("AOP event id is required")
	}
	raw, err := protobuf.Marshal(event)
	if err != nil {
		return 0, false, err
	}
	createdAt := time.Now().UTC().Format(time.RFC3339Nano)
	if event.GetEmittedAt() != nil {
		createdAt = event.GetEmittedAt().AsTime().UTC().Format(time.RFC3339Nano)
	}
	err = s.write(ctx, func(ctx context.Context, tx bun.Tx) error {
		cursor, persisted = 0, false
		if err := advisoryXactLock(ctx, tx, "aop:"+sessionID); err != nil {
			return err
		}
		var existing aopEventModel
		lookupErr := tx.NewSelect().Model(&existing).Column("cursor").
			Where("session_id = ? AND event_id = ?", sessionID, event.Id).Limit(1).Scan(ctx)
		if lookupErr == nil {
			cursor = existing.Cursor
			persisted = false
			return nil
		}
		if !errors.Is(lookupErr, sql.ErrNoRows) {
			return lookupErr
		}
		if event.GetTurnEnded() != nil && event.TurnId != "" {
			terminal, err := turnTerminalCursor(ctx, tx, sessionID, event.SessionId, event.TurnId, after)
			if err != nil {
				return err
			}
			if terminal > 0 {
				cursor = terminal
				return nil
			}
		}
		if err := tx.NewSelect().Model((*aopEventModel)(nil)).
			ColumnExpr("COALESCE(MAX(cursor), 0) + 1").Where("session_id = ?", sessionID).Scan(ctx, &cursor); err != nil {
			return err
		}
		_, err := tx.NewInsert().Model(&aopEventModel{
			ID: generateID(), SessionID: sessionID, EventID: event.Id, Cursor: cursor,
			TurnID: event.GetTurnId(), Emitter: event.GetEmitter(), Sequence: event.GetSeq(),
			EventProto: raw, CreatedAt: createdAt,
		}).Exec(ctx)
		if err == nil {
			persisted = true
		}
		return err
	})
	if err != nil {
		return 0, false, err
	}
	return cursor, persisted, nil
}

func (s *SQLiteStore) ListAOPEvents(ctx context.Context, sessionID string, limit int) ([]*aop.Event, error) {
	page, _, err := s.ListAOPEventPage(ctx, sessionID, 0, limit)
	if err != nil {
		return nil, err
	}
	events := make([]*aop.Event, 0, len(page))
	for _, stored := range page {
		events = append(events, stored.Event)
	}
	return events, nil
}

// turnTerminalCursor checks only the requested turn, stopping at its latest
// start. Terminal identity survives Web restarts and different event IDs.
func turnTerminalCursor(ctx context.Context, db bun.IDB, sessionID, originID, turnID string, after int64) (int64, error) {
	var events []aopEventModel
	if err := db.NewSelect().Model(&events).Column("cursor", "event_proto").
		Where("session_id = ? AND turn_id = ? AND cursor > ?", sessionID, turnID, after).
		OrderExpr("cursor DESC").Scan(ctx); err != nil {
		return 0, err
	}
	for _, stored := range events {
		event, err := eventFromProto(stored.EventProto)
		if err != nil {
			return 0, err
		}
		if event.SessionId != originID {
			continue
		}
		if event.GetTurnEnded() != nil {
			return stored.Cursor, nil
		}
		if event.GetTurnStarted() != nil {
			break
		}
	}
	return 0, nil
}

func (s *SQLiteStore) HasTurnEnded(ctx context.Context, sessionID, turnID string) (bool, error) {
	var cursor int64
	err := s.read(ctx, func(ctx context.Context, tx bun.Tx) error {
		var err error
		cursor, err = turnTerminalCursor(ctx, tx, sessionID, sessionID, turnID, 0)
		return err
	})
	return cursor > 0, err
}

func (s *SQLiteStore) MaxAOPEventSeq(ctx context.Context, sessionID string) (uint64, error) {
	var maximum uint64
	err := s.read(ctx, func(ctx context.Context, tx bun.Tx) error {
		return tx.NewSelect().Model((*aopEventModel)(nil)).
			ColumnExpr("COALESCE(MAX(sequence), 0)").Where("session_id = ?", sessionID).Scan(ctx, &maximum)
	})
	return maximum, err
}

func (s *SQLiteStore) ListAOPEventPage(ctx context.Context, sessionID string, before int64, limit int) ([]*aop.EventDelivery, int64, error) {
	if limit <= 0 {
		limit = 10000
	}
	if limit > 10000 {
		limit = 10000
	}
	var models []aopEventModel
	err := s.read(ctx, func(ctx context.Context, tx bun.Tx) error {
		query := tx.NewSelect().Model((*aopEventModel)(nil)).Column("cursor", "event_proto").
			Where("session_id = ?", sessionID).OrderExpr("cursor DESC").Limit(int64(limit + 1))
		if before > 0 {
			query = query.Where("cursor < ?", before)
		}
		return query.Model(&models).Scan(ctx)
	})
	if err != nil {
		return nil, 0, err
	}
	hasMore := len(models) > limit
	if hasMore {
		models = models[:limit]
	}
	events := make([]*aop.EventDelivery, 0, len(models))
	for i := len(models) - 1; i >= 0; i-- {
		event, err := eventFromProto(models[i].EventProto)
		if err != nil {
			return nil, 0, fmt.Errorf("decode AOP event at cursor %d: %w", models[i].Cursor, err)
		}
		if event.GetSessionId() == "" || event.GetPayload() == nil {
			continue
		}
		events = append(events, &aop.EventDelivery{Cursor: strconv.FormatInt(models[i].Cursor, 10), Event: event})
	}
	var next int64
	if hasMore && len(events) > 0 {
		next, _ = strconv.ParseInt(events[0].Cursor, 10, 64)
	}
	return events, next, nil
}

// ListAOPEventsAfter returns events and a continuation cursor (zero to stop).
// The cursor advances over stored rows even when their events are filtered.
func (s *SQLiteStore) ListAOPEventsAfter(ctx context.Context, sessionID string, after int64, limit int) ([]*aop.EventDelivery, int64, error) {
	if limit > 10000 {
		limit = 10000
	}
	var models []aopEventModel
	err := s.read(ctx, func(ctx context.Context, tx bun.Tx) error {
		query := tx.NewSelect().Model((*aopEventModel)(nil)).Column("cursor", "event_proto").
			Where("session_id = ? AND cursor > ?", sessionID, after).OrderExpr("cursor ASC")
		if limit > 0 {
			query = query.Limit(int64(limit))
		}
		return query.Model(&models).Scan(ctx)
	})
	if err != nil {
		return nil, 0, err
	}
	events := make([]*aop.EventDelivery, 0, len(models))
	for _, model := range models {
		event, err := eventFromProto(model.EventProto)
		if err != nil {
			return nil, 0, fmt.Errorf("decode AOP event at cursor %d: %w", model.Cursor, err)
		}
		if event.GetSessionId() == "" || event.GetPayload() == nil {
			continue
		}
		events = append(events, &aop.EventDelivery{Cursor: strconv.FormatInt(model.Cursor, 10), Event: event})
	}
	var next int64
	if limit > 0 && len(models) == limit {
		next = models[len(models)-1].Cursor
	}
	return events, next, nil
}

func eventFromProto(raw []byte) (*aop.Event, error) {
	event := new(aop.Event)
	if err := protobuf.Unmarshal(raw, event); err != nil {
		return nil, err
	}
	return event, nil
}

const artifactSyncBatch = 100

// SyncArtifactEvents appends raw Artifact events and returns the next archive
// page. CSTX parsing and projection belong to the browser.
func (s *SQLiteStore) SyncArtifactEvents(ctx context.Context, appended []*aop.Event, after int64) ([]*aop.EventDelivery, error) {
	models, err := artifactEventModels(appended)
	if err != nil {
		return nil, err
	}

	var deliveries []*aop.EventDelivery
	err = s.write(ctx, func(ctx context.Context, tx bun.Tx) error {
		deliveries = nil
		if err := insertArtifactModels(ctx, tx, models); err != nil {
			return err
		}
		var archived []rawArtifactModel
		if err := tx.NewSelect().Model(&archived).Where("cursor > ?", after).
			OrderExpr("cursor ASC").Limit(artifactSyncBatch).Scan(ctx); err != nil {
			return err
		}
		deliveries = make([]*aop.EventDelivery, 0, len(archived))
		for _, model := range archived {
			event := new(aop.Event)
			if err := protobuf.Unmarshal(model.EventProto, event); err != nil {
				return fmt.Errorf("decode artifact event %q: %w", model.EventID, err)
			}
			deliveries = append(deliveries, &aop.EventDelivery{
				Cursor: strconv.FormatInt(model.Cursor, 10),
				Event:  event,
			})
		}
		return nil
	})
	return deliveries, err
}

func (s *SQLiteStore) archiveArtifactEvents(ctx context.Context, appended []*aop.Event) error {
	models, err := artifactEventModels(appended)
	if err != nil {
		return err
	}
	return s.write(ctx, func(ctx context.Context, tx bun.Tx) error {
		return insertArtifactModels(ctx, tx, models)
	})
}

func artifactEventModels(appended []*aop.Event) ([]*rawArtifactModel, error) {
	models := make([]*rawArtifactModel, 0, len(appended))
	for index, event := range appended {
		if event == nil {
			return nil, fmt.Errorf("artifact event %d is required", index)
		}
		if _, _, found, err := toolpb.FromEvent(event); err != nil {
			return nil, fmt.Errorf("artifact event %d: %w", index, err)
		} else if !found && !event.GetExtension().MessageIs(new(toolpb.Loot)) {
			return nil, fmt.Errorf("artifact event %d does not contain an aop.tool.Artifact or Loot payload", index)
		}
		eventID := strings.TrimSpace(event.GetId())
		if eventID == "" {
			return nil, fmt.Errorf("artifact event %d has no id", index)
		}
		if event.GetEmittedAt() == nil || !event.GetEmittedAt().IsValid() {
			return nil, fmt.Errorf("artifact event %q has an invalid emitted_at", eventID)
		}
		raw, err := (protobuf.MarshalOptions{Deterministic: true}).Marshal(event)
		if err != nil {
			return nil, fmt.Errorf("encode artifact event %q: %w", eventID, err)
		}
		models = append(models, &rawArtifactModel{
			EventID:    eventID,
			EventProto: raw,
			CreatedAt:  event.GetEmittedAt().AsTime().UTC().Format(time.RFC3339Nano),
		})
	}
	return models, nil
}

// insertArtifactModels inserts with the identity cursor left to the database.
// Readers page by "cursor > after", which is only gap-safe if rows become
// visible in cursor order. PostgreSQL inserters hold one advisory lock until
// commit to guarantee that order. Identity values
// burned by ON CONFLICT DO NOTHING leave harmless gaps.
func insertArtifactModels(ctx context.Context, tx bun.Tx, models []*rawArtifactModel) error {
	if len(models) == 0 {
		return nil
	}
	if err := advisoryXactLock(ctx, tx, "raw_artifacts"); err != nil {
		return err
	}
	for _, model := range models {
		model := *model // RETURNING fills Cursor; keep the caller's models reusable across retries.
		if _, err := tx.NewInsert().Model(&model).On("CONFLICT (event_id) DO NOTHING").Exec(ctx); err != nil {
			return err
		}
	}
	return nil
}
