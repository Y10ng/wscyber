package webapi

import (
	"context"
	"database/sql"
	"time"

	aop "github.com/chainreactors/cyber/aop"
	"github.com/uptrace/bun"
)

// scanUsage 是用量与任务列表唯一的事件入口。taskIDs=nil 为全局；否则仅扫描
// 给定会话和任务的子 Agent 登记表。activity 只观察原有业务会话活动，不改变列表时间语义。
func (s *taskStore) scanUsage(ctx context.Context, sessions map[string]usageSessionRef, taskIDs []string,
	start, end time.Time, activity func(string, time.Time), visit func(string, string, time.Time, *aop.TokenUsage)) (int, error) {
	skipped := 0
	count := func(session, task, rawAt string, usage *aop.TokenUsage) {
		at, err := time.Parse(time.RFC3339Nano, rawAt)
		if err != nil {
			skipped++
			return
		}
		if (!start.IsZero() && at.Before(start)) || (!end.IsZero() && !at.Before(end)) {
			return
		}
		visit(session, task, at, usage)
	}
	err := s.read(ctx, func(ctx context.Context, db bun.IDB) error {
		scanEvents := func(where string, args []any) error {
			if !start.IsZero() {
				where += ` AND substr(created_at, 1, 10) COLLATE "C" >= ?`
				args = append(args, start.UTC().Format("2006-01-02"))
			}
			if !end.IsZero() {
				where += ` AND substr(created_at, 1, 10) COLLATE "C" <= ?`
				args = append(args, end.UTC().Format("2006-01-02"))
			}
			return scanRows(db.QueryContext(ctx, `SELECT session_id, created_at, event_proto FROM chat_aop_events WHERE `+where, args...))(func(rows *sql.Rows) error {
				var session, rawAt string
				var raw []byte
				if err := rows.Scan(&session, &rawAt, &raw); err != nil {
					return err
				}
				ref := sessions[session]
				if ref.role == "subagent" {
					return nil
				}
				if activity != nil {
					activity(ref.taskID, parseAt(rawAt))
				}
				usage, err := eventUsage(raw)
				if err != nil {
					skipped++
					return nil
				}
				if usage != nil {
					count(session, ref.taskID, rawAt, usage)
				}
				return nil
			})
		}
		if taskIDs == nil {
			if err := scanEvents("TRUE", nil); err != nil {
				return err
			}
		} else {
			ids := make([]string, 0, len(sessions))
			for id := range sessions {
				ids = append(ids, id)
			}
			if err := inChunks(ids, func(p string, args []any) error { return scanEvents("session_id IN ("+p+")", args) }); err != nil {
				return err
			}
		}
		query := `SELECT session_id, task_id, model, input_tokens, output_tokens, total_tokens, cache_read_tokens, cache_write_tokens, created_at FROM webapi_subsession_usage`
		var args []any
		if taskIDs != nil {
			query += ` WHERE task_id IN (?)`
			args = []any{bun.In(taskIDs)}
		}
		return scanRows(db.QueryContext(ctx, query, args...))(func(rows *sql.Rows) error {
			var session, task, model, at string
			var input, output, total, cacheRead, cacheWrite uint64
			if err := rows.Scan(&session, &task, &model, &input, &output, &total, &cacheRead, &cacheWrite, &at); err != nil {
				return err
			}
			// 全局历史口径按会话映射归属；任务查询按登记表 task_id 取数。
			if taskIDs == nil {
				task = sessions[session].taskID
			}
			count(session, task, at, &aop.TokenUsage{Model: model, InputTokens: input, OutputTokens: output, TotalTokens: total, Detail: map[string]uint64{"cache_read": cacheRead, "cache_write": cacheWrite}})
			return nil
		})
	})
	return skipped, err
}
