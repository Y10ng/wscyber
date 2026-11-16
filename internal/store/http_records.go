package store

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"time"

	"github.com/jackc/pgx/v5"

	"wscyber/internal/domain"
)

// 游标只携带创建顺序及筛选身份，不包含 URL、正文或凭据。
// 它不是访问凭据，也不是跨请求的一致快照或状态增量流。
type httpRecordCursor struct {
	Version   int       `json:"version"`
	MissionID string    `json:"mission_id"`
	ThreadID  string    `json:"thread_id"`
	Status    string    `json:"status"`
	Method    string    `json:"method"`
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func validHTTPRecordCursorID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func decodeHTTPRecordCursor(raw, missionID string, q domain.HTTPRecordQuery) (*httpRecordCursor, error) {
	if raw == "" {
		return nil, nil
	}
	bad := func() (*httpRecordCursor, error) { return nil, invalid("HTTP 记录游标无效或与筛选不一致") }
	if len(raw) > 2048 {
		return bad()
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || base64.RawURLEncoding.EncodeToString(data) != raw {
		return bad()
	}
	// encoding/json 默认接受重复键与大小写别名；这里要求全部精确字段各出现一次。
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return bad()
	}
	fields := map[string]bool{"version": false, "mission_id": false, "thread_id": false, "status": false, "method": false, "created_at": false, "id": false}
	var createdAtText string
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return bad()
		}
		key, ok := token.(string)
		seen, known := fields[key]
		if !ok || !known || seen {
			return bad()
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return bad()
		}
		if key == "created_at" {
			if err := json.Unmarshal(value, &createdAtText); err != nil {
				return bad()
			}
		}
		fields[key] = true
	}
	if token, err = dec.Token(); err != nil || token != json.Delim('}') {
		return bad()
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return bad()
	}
	for _, seen := range fields {
		if !seen {
			return bad()
		}
	}
	var cursor httpRecordCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.Version != 1 || cursor.CreatedAt.IsZero() || cursor.CreatedAt.Year() < 1 || cursor.CreatedAt.Year() > 9999 {
		return bad()
	}
	// 编码器只产生 UTC RFC3339Nano；拒绝宽松解析接受的逗号小数、异常时区和超出 PG 微秒精度的时间。
	if createdAtText != cursor.CreatedAt.UTC().Format(time.RFC3339Nano) || cursor.CreatedAt.Nanosecond()%1000 != 0 {
		return bad()
	}
	if !validHTTPRecordCursorID(cursor.ID) || !validHTTPRecordCursorID(cursor.MissionID) || (cursor.ThreadID != "" && !validHTTPRecordCursorID(cursor.ThreadID)) {
		return bad()
	}
	if cursor.MissionID != missionID || cursor.ThreadID != q.ThreadID || cursor.Status != q.Status || cursor.Method != q.Method {
		return bad()
	}
	return &cursor, nil
}

func encodeHTTPRecordCursor(missionID string, q domain.HTTPRecordQuery, last domain.HTTPRecordSummary) (string, error) {
	data, err := json.Marshal(httpRecordCursor{
		Version: 1, MissionID: missionID, ThreadID: q.ThreadID, Status: q.Status, Method: q.Method,
		CreatedAt: last.CreatedAt.UTC(), ID: last.ID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// 列表仅在数据库中投影白名单字段，绝不读取完整 input/result 再在内存分页。
const httpRecordSummaryColumns = `a.id, t.mission_id, a.thread_id, t.title,
	COALESCE(a.input->>'method', ''), COALESCE(a.input->>'url', ''), a.status,
	(a.result IS NOT NULL AND a.result <> 'null'::jsonb),
	COALESCE((a.result->>'status_code')::integer, 0),
	COALESCE((a.result->>'truncated')::boolean, false),
	COALESCE((a.result->>'effect_unknown')::boolean, false),
	a.resolved_at, a.created_at, a.updated_at`

func scanHTTPRecordSummary(row scanner) (domain.HTTPRecordSummary, error) {
	var v domain.HTTPRecordSummary
	err := row.Scan(&v.ID, &v.MissionID, &v.ThreadID, &v.ThreadTitle, &v.Method, &v.URL, &v.Status,
		&v.HasResult, &v.StatusCode, &v.Truncated, &v.EffectUnknown, &v.ResolvedAt, &v.CreatedAt, &v.UpdatedAt)
	return v, databaseError(err)
}

func (s *Store) ListHTTPRecords(ctx context.Context, missionID string, query domain.HTTPRecordQuery) (domain.HTTPRecordPage, error) {
	q, err := domain.NormalizeHTTPRecordQuery(query)
	if err != nil {
		return domain.HTTPRecordPage{}, err
	}
	cursor, err := decodeHTTPRecordCursor(q.Cursor, missionID, q)
	if err != nil {
		return domain.HTTPRecordPage{}, err
	}
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.HTTPRecordPage, error) {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM missions WHERE id = $1)`, missionID).Scan(&exists); err != nil {
			return domain.HTTPRecordPage{}, err
		}
		if !exists {
			return domain.HTTPRecordPage{}, domain.ErrNotFound
		}
		if q.ThreadID != "" {
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM threads WHERE id = $1 AND mission_id = $2)`, q.ThreadID, missionID).Scan(&exists); err != nil {
				return domain.HTTPRecordPage{}, err
			}
			if !exists {
				return domain.HTTPRecordPage{}, domain.ErrNotFound
			}
		}
		var beforeTime any
		var beforeID string
		if cursor != nil {
			beforeTime, beforeID = cursor.CreatedAt, cursor.ID
		}
		items, err := collect(ctx, tx, `SELECT `+httpRecordSummaryColumns+`
			FROM actions a JOIN threads t ON t.id = a.thread_id
			WHERE t.mission_id = $1 AND a.input->>'kind' = 'http'
			AND ($2::text = '' OR a.thread_id = $2)
			AND ($3::text = '' OR a.status = $3)
			AND ($4::text = '' OR upper(a.input->>'method') = $4)
			AND ($5::timestamptz IS NULL OR (a.created_at, a.id) < ($5::timestamptz, $6::text))
			ORDER BY a.created_at DESC, a.id DESC LIMIT $7`, scanHTTPRecordSummary,
			missionID, q.ThreadID, q.Status, q.Method, beforeTime, beforeID, q.Limit+1)
		if err != nil {
			return domain.HTTPRecordPage{}, err
		}
		page := domain.HTTPRecordPage{Items: items}
		if len(items) > q.Limit {
			page.Items = items[:q.Limit]
			page.NextCursor, err = encodeHTTPRecordCursor(missionID, q, page.Items[len(page.Items)-1])
		}
		return page, err
	})
}

// GetHTTPRecord 先限制项目和 kind，再读取唯一动作；归属及详情共用同一只读快照。
func (s *Store) GetHTTPRecord(ctx context.Context, missionID, actionID string) (domain.HTTPRecordDetail, error) {
	return transaction(ctx, s, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) (domain.HTTPRecordDetail, error) {
		var detail domain.HTTPRecordDetail
		action, err := scanAction(tx.QueryRow(ctx, `SELECT `+actionColumns+` FROM actions
			WHERE id = $1 AND input->>'kind' = 'http' AND thread_id IN (
				SELECT t.id FROM threads t JOIN missions m ON m.id = t.mission_id WHERE m.id = $2
			)`, actionID, missionID))
		if err != nil {
			return detail, err
		}
		thread, err := getThread(ctx, tx, action.ThreadID)
		if err != nil {
			return detail, err
		}
		if thread.MissionID != missionID || action.ThreadID != thread.ID || action.Input.Kind != "http" {
			return detail, domain.ErrNotFound
		}
		return domain.HTTPRecordDetail{MissionID: missionID, Thread: thread, Action: action}, nil
	})
}
