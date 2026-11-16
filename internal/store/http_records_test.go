package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"wscyber/internal/domain"
)

const (
	httpRecordMissionID = "00000000-0000-4000-8000-000000000001"
	httpRecordThreadID  = "00000000-0000-4000-8000-000000000002"
	httpRecordActionID  = "00000000-0000-4000-8000-000000000003"
)

func TestHTTPRecordCursorValidation(t *testing.T) {
	q := domain.HTTPRecordQuery{Limit: 2, ThreadID: httpRecordThreadID, Status: "succeeded", Method: "GET"}
	last := domain.HTTPRecordSummary{ID: httpRecordActionID, CreatedAt: time.Date(2026, 11, 11, 1, 2, 3, 123456000, time.UTC), URL: "https://fixture.invalid/?secret=not-in-cursor"}
	raw, err := encodeHTTPRecordCursor(httpRecordMissionID, q, last)
	if err != nil {
		t.Fatal(err)
	}
	cursor, err := decodeHTTPRecordCursor(raw, httpRecordMissionID, q)
	if err != nil || cursor.ID != last.ID || !cursor.CreatedAt.Equal(last.CreatedAt) {
		t.Fatal(cursor, err)
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || strings.Contains(string(data), "secret") || strings.Contains(string(data), "fixture.invalid") {
		t.Fatal("游标不应包含请求材料", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != 7 {
		t.Fatal(fields, err)
	}
	encode := func(value string) string { return base64.RawURLEncoding.EncodeToString([]byte(value)) }
	bad := []string{
		"not*base64", raw + "=", raw + "\n", strings.Repeat("a", 2049), encode("null"), encode("[]"), encode("{}"),
		encode(string(data) + " {}"), encode(strings.TrimSuffix(string(data), "}") + `,"version":1}`),
		encode(strings.Replace(string(data), `"version"`, `"Version"`, 1)),
	}
	for key, value := range map[string]any{
		"version": 2, "mission_id": "missing", "thread_id": "wrong", "status": "failed", "method": "get",
		"created_at": "2026-11-11T01:02:03,123456Z", "id": "not-an-id", "unknown": "extra",
	} {
		copy := make(map[string]any, len(fields)+1)
		for k, v := range fields {
			copy[k] = v
		}
		copy[key] = value
		encoded, err := json.Marshal(copy)
		if err != nil {
			t.Fatal(err)
		}
		bad = append(bad, encode(string(encoded)))
	}
	for key := range fields {
		for _, remove := range []bool{false, true} {
			copy := make(map[string]any, len(fields))
			for k, v := range fields {
				copy[k] = v
			}
			if remove {
				delete(copy, key)
			} else {
				copy[key] = nil
			}
			encoded, err := json.Marshal(copy)
			if err != nil {
				t.Fatal(err)
			}
			bad = append(bad, encode(string(encoded)))
		}
	}
	for _, timestamp := range []string{"invalid", "0001-01-01T00:00:00Z", "2026-11-11T01:02:03.123456789Z", "2026-11-11T01:02:03+24:00", "2026-13-11T01:02:03Z"} {
		copy := make(map[string]any, len(fields))
		for k, v := range fields {
			copy[k] = v
		}
		copy["created_at"] = timestamp
		encoded, err := json.Marshal(copy)
		if err != nil {
			t.Fatal(err)
		}
		bad = append(bad, encode(string(encoded)))
	}
	for i, raw := range bad {
		if _, err := decodeHTTPRecordCursor(raw, httpRecordMissionID, q); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("非法游标 #%d 未拒绝：%v", i, err)
		}
	}
	for _, other := range []domain.HTTPRecordQuery{
		{Limit: 2, Status: q.Status, Method: q.Method},
		{Limit: 2, ThreadID: q.ThreadID, Method: q.Method},
		{Limit: 2, ThreadID: q.ThreadID, Status: q.Status},
	} {
		if _, err := decodeHTTPRecordCursor(raw, httpRecordMissionID, other); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("游标不得跨筛选使用", other, err)
		}
	}
	if _, err := decodeHTTPRecordCursor(raw, httpRecordThreadID, q); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("游标不得跨项目使用", err)
	}
	q.Limit = 100
	if _, err := decodeHTTPRecordCursor(raw, httpRecordMissionID, q); err != nil {
		t.Fatal("改变页大小不改变筛选身份", err)
	}
	if cursor, err := decodeHTTPRecordCursor("", httpRecordMissionID, q); err != nil || cursor != nil {
		t.Fatal("空游标应是首屏", cursor, err)
	}
}

// 仅准备隔离 PG 的保存态 fixture，不启动 worker/模型，也不执行任何请求。
func insertHTTPRecordFixture(t *testing.T, ctx context.Context, s *Store, thread domain.Thread, a domain.Action) domain.Action {
	t.Helper()
	if a.ID == "" {
		var err error
		a.ID, err = newID()
		if err != nil {
			t.Fatal(err)
		}
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Date(2026, 11, 11, 12, 0, 0, 123456000, time.UTC)
	}
	input, hash, err := canonicalInput(a.Input)
	if err != nil {
		t.Fatal(err)
	}
	var result any
	if a.Result != nil {
		result, err = json.Marshal(a.Result)
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := scanAction(s.pool.QueryRow(ctx, `INSERT INTO actions
		(id, thread_id, input, request_hash, status, result, error, decision_reason, resolved_at, resolution_note, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11) RETURNING `+actionColumns,
		a.ID, thread.ID, input, hash, a.Status, result, a.Error, a.DecisionReason, a.ResolvedAt, a.ResolutionNote, a.CreatedAt))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func httpRecordInput(method string) domain.ActionInput {
	return domain.ActionInput{Kind: "http", Method: method, URL: "https://fixture.invalid/path?sensitive=saved-query",
		Headers: map[string]string{"Authorization": "fixture-request-secret"}, Body: "fixture-request-body", Reason: "fixture-reason"}
}

func sameMissionThread(t *testing.T, ctx context.Context, s *Store, missionID, title string) domain.Thread {
	t.Helper()
	thread, err := s.CreateThread(ctx, domain.Thread{MissionID: missionID, Title: title, Question: "只验证已保存记录"})
	if err != nil {
		t.Fatal(err)
	}
	return thread
}

func TestPostgresHTTPRecordsEmptyAndOwnership(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	other := fixtureThread(t, ctx, s)
	page, err := s.ListHTTPRecords(ctx, thread.MissionID, domain.HTTPRecordQuery{})
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatal("空列表必须明确返回 [] 和空游标", page, err)
	}
	for _, missionID := range []string{"", "missing"} {
		if _, err := s.ListHTTPRecords(ctx, missionID, domain.HTTPRecordQuery{}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("不存在项目不能冒充空列表", missionID, err)
		}
	}
	for _, threadID := range []string{other.ID, "missing"} {
		if _, err := s.ListHTTPRecords(ctx, thread.MissionID, domain.HTTPRecordQuery{ThreadID: threadID}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("跨项目/缺失方向必须 404", threadID, err)
		}
	}
	command := insertHTTPRecordFixture(t, ctx, s, thread, domain.Action{
		Input: domain.ActionInput{Kind: "local_command", Command: "printf fixture", Reason: "只保存命令记录，不执行"}, Status: "succeeded",
	})
	httpAction := insertHTTPRecordFixture(t, ctx, s, other, domain.Action{Input: httpRecordInput("GET"), Status: "succeeded"})
	page, err = s.ListHTTPRecords(ctx, thread.MissionID, domain.HTTPRecordQuery{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("列表必须排除命令与其他项目 HTTP", page, err)
	}
	for _, tc := range []struct{ missionID, actionID string }{
		{thread.MissionID, command.ID}, {thread.MissionID, httpAction.ID}, {thread.MissionID, "missing"}, {"missing", httpAction.ID},
	} {
		if _, err := s.GetHTTPRecord(ctx, tc.missionID, tc.actionID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("缺失/非HTTP/跨项目详情必须 404", tc, err)
		}
	}
	for _, q := range []domain.HTTPRecordQuery{{Limit: -1}, {Limit: 101}, {Status: "unknown"}, {Method: "GET\n"}, {Cursor: "broken"}} {
		if _, err := s.ListHTTPRecords(ctx, thread.MissionID, q); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal("持久层也必须验证分页输入", q, err)
		}
	}
}

func TestPostgresHTTPRecordsPaginationAndFilters(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	first := fixtureThread(t, ctx, s)
	second := sameMissionThread(t, ctx, s, first.MissionID, "另一个研究方向")
	other := fixtureThread(t, ctx, s)
	now := time.Date(2026, 11, 11, 12, 0, 0, 123456000, time.UTC)
	var expected []domain.Action
	for i := 1; i <= 9; i++ {
		thread, method := first, "get"
		if i%2 == 0 {
			thread, method = second, "POST"
		}
		created := now
		if i <= 3 {
			created = now.Add(-time.Hour)
		}
		expected = append(expected, insertHTTPRecordFixture(t, ctx, s, thread, domain.Action{
			ID: fmt.Sprintf("00000000-0000-4000-8000-%012d", i), Input: httpRecordInput(method), Status: "succeeded", CreatedAt: created,
		}))
	}
	insertHTTPRecordFixture(t, ctx, s, other, domain.Action{Input: httpRecordInput("GET"), Status: "succeeded", CreatedAt: now.Add(time.Hour)})
	insertHTTPRecordFixture(t, ctx, s, first, domain.Action{Input: domain.ActionInput{Kind: "local_command", Command: "printf fixture", Reason: "不执行"}, Status: "succeeded", CreatedAt: now.Add(time.Hour)})
	sort.Slice(expected, func(i, j int) bool {
		if expected[i].CreatedAt.Equal(expected[j].CreatedAt) {
			return expected[i].ID > expected[j].ID
		}
		return expected[i].CreatedAt.After(expected[j].CreatedAt)
	})
	query := domain.HTTPRecordQuery{Limit: 2}
	var seen []string
	var newAction domain.Action
	for pageNumber := 0; pageNumber < 10; pageNumber++ {
		page, err := s.ListHTTPRecords(ctx, first.MissionID, query)
		if err != nil || len(page.Items) > 2 {
			t.Fatal(page, err)
		}
		for _, item := range page.Items {
			seen = append(seen, item.ID)
			if item.MissionID != first.MissionID || (item.ThreadID != first.ID && item.ThreadID != second.ID) {
				t.Fatal("项目/方向归属错误", item)
			}
		}
		if pageNumber == 0 {
			if page.NextCursor == "" {
				t.Fatal("应通过 limit+1 检测下一页")
			}
			newAction = insertHTTPRecordFixture(t, ctx, s, first, domain.Action{Input: httpRecordInput("GET"), Status: "succeeded", CreatedAt: now.Add(time.Hour)})
			for _, changed := range []domain.HTTPRecordQuery{
				{Cursor: page.NextCursor, ThreadID: first.ID}, {Cursor: page.NextCursor, Status: "succeeded"}, {Cursor: page.NextCursor, Method: "GET"},
			} {
				if _, err := s.ListHTTPRecords(ctx, first.MissionID, changed); !errors.Is(err, domain.ErrInvalid) {
					t.Fatal("更换筛选必须拒绝旧游标", changed, err)
				}
			}
			if _, err := s.ListHTTPRecords(ctx, other.MissionID, domain.HTTPRecordQuery{Cursor: page.NextCursor}); !errors.Is(err, domain.ErrInvalid) {
				t.Fatal("跨项目不能复用游标", err)
			}
		}
		if page.NextCursor == "" {
			break
		}
		query.Cursor = page.NextCursor
	}
	var want []string
	for _, a := range expected {
		want = append(want, a.ID)
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatal("同时间按ID降序分页必须不重复、不漏旧记录、不混入新页记录", seen, want)
	}
	filteredQuery := domain.HTTPRecordQuery{Limit: 1, ThreadID: second.ID, Status: "succeeded", Method: "post"}
	var filteredIDs []string
	for i := 0; i < 5; i++ {
		page, err := s.ListHTTPRecords(ctx, first.MissionID, filteredQuery)
		if err != nil || len(page.Items) > 1 {
			t.Fatal("组合筛选分页失败", page, err)
		}
		for _, item := range page.Items {
			filteredIDs = append(filteredIDs, item.ID)
		}
		if page.NextCursor == "" {
			break
		}
		filteredQuery.Cursor = page.NextCursor
	}
	var filteredWant []string
	for _, a := range expected {
		if a.ThreadID == second.ID {
			filteredWant = append(filteredWant, a.ID)
		}
	}
	if !reflect.DeepEqual(filteredIDs, filteredWant) {
		t.Fatal("规范化方法与组合筛选应跨页保持", filteredIDs, filteredWant)
	}
	latest, err := s.ListHTTPRecords(ctx, first.MissionID, domain.HTTPRecordQuery{Limit: 2})
	if err != nil || len(latest.Items) != 2 || latest.Items[0].ID != newAction.ID {
		t.Fatal("回最新页应出现新记录", latest, err)
	}
	for _, tc := range []struct {
		q    domain.HTTPRecordQuery
		want int
	}{
		{domain.HTTPRecordQuery{ThreadID: first.ID}, 6}, {domain.HTTPRecordQuery{ThreadID: second.ID}, 4},
		{domain.HTTPRecordQuery{Method: "get"}, 6}, {domain.HTTPRecordQuery{Method: "post"}, 4},
		{domain.HTTPRecordQuery{ThreadID: second.ID, Status: "succeeded", Method: "POST"}, 4},
		{domain.HTTPRecordQuery{ThreadID: second.ID, Method: "GET"}, 0}, {domain.HTTPRecordQuery{Status: "failed"}, 0},
	} {
		page, err := s.ListHTTPRecords(ctx, first.MissionID, tc.q)
		if err != nil || len(page.Items) != tc.want || page.Items == nil || page.NextCursor != "" {
			t.Fatal(tc, page, err)
		}
		for _, item := range page.Items {
			if item.ThreadID == second.ID && item.ThreadTitle != second.Title {
				t.Fatal("摘要应来自实际方向标题", item)
			}
		}
	}
}

func TestPostgresHTTPRecordsLimitBounds(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	for i := 0; i < 105; i++ {
		insertHTTPRecordFixture(t, ctx, s, thread, domain.Action{Input: httpRecordInput("GET"), Status: "succeeded"})
	}
	for _, limit := range []int{0, 1, 50, 100} {
		page, err := s.ListHTTPRecords(ctx, thread.MissionID, domain.HTTPRecordQuery{Limit: limit})
		want := limit
		if want == 0 {
			want = 50
		}
		if err != nil || len(page.Items) != want || page.NextCursor == "" {
			t.Fatal(limit, len(page.Items), err)
		}
	}
	first, err := s.ListHTTPRecords(ctx, thread.MissionID, domain.HTTPRecordQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	last, err := s.ListHTTPRecords(ctx, thread.MissionID, domain.HTTPRecordQuery{Limit: 100, Cursor: first.NextCursor})
	if err != nil || len(last.Items) != 5 || last.NextCursor != "" {
		t.Fatal(last, err)
	}
}

func TestPostgresHTTPRecordsStatesEvidenceAndNoWrites(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	first := fixtureThread(t, ctx, s)
	var actions []domain.Action
	var threads []domain.Thread
	resolved := time.Date(2026, 11, 11, 13, 0, 0, 0, time.UTC)
	for _, status := range []string{"pending", "approved", "running", "succeeded", "failed", "rejected", "outcome_unknown"} {
		thread := sameMissionThread(t, ctx, s, first.MissionID, status)
		a := domain.Action{Input: httpRecordInput("POST"), Status: status, Error: "fixture-error", DecisionReason: "fixture-decision"}
		if status == "succeeded" || status == "failed" || status == "outcome_unknown" {
			a.Result = &domain.ActionResult{Headers: map[string][]string{"Set-Cookie": {"fixture-response-secret"}}, Body: []byte{0, 128, 255, 10}, Truncated: true, EffectUnknown: status == "outcome_unknown"}
		}
		if status == "succeeded" {
			a.Result.StatusCode = 201
		}
		if status == "outcome_unknown" {
			a.ResolvedAt, a.ResolutionNote = &resolved, "fixture-resolution"
		}
		actions = append(actions, insertHTTPRecordFixture(t, ctx, s, thread, a))
		threads = append(threads, thread)
	}
	// unknown 的状态与回执字段分别保存：没有回执时不伪造 effect_unknown=true。
	unknownThread := sameMissionThread(t, ctx, s, first.MissionID, "无回执的未知")
	actions = append(actions, insertHTTPRecordFixture(t, ctx, s, unknownThread, domain.Action{Input: httpRecordInput("GET"), Status: "outcome_unknown"}))
	threads = append(threads, unknownThread)
	var before []domain.Snapshot
	for _, thread := range threads {
		before = append(before, mustSnapshot(t, ctx, s, thread.ID))
	}
	eventsBefore, err := s.ListEvents(ctx, first.MissionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	all, err := s.ListHTTPRecords(ctx, first.MissionID, domain.HTTPRecordQuery{})
	if err != nil || len(all.Items) != len(actions) {
		t.Fatal("默认列表必须包含未执行、拒绝与未知等全部状态", all, err)
	}
	for _, a := range actions {
		page, err := s.ListHTTPRecords(ctx, first.MissionID, domain.HTTPRecordQuery{Status: a.Status, ThreadID: a.ThreadID})
		if err != nil || len(page.Items) != 1 {
			t.Fatal(a.Status, page, err)
		}
		item := page.Items[0]
		if item.ID != a.ID || item.Status != a.Status || item.HasResult != (a.Result != nil) || !reflect.DeepEqual(item.ResolvedAt, a.ResolvedAt) {
			t.Fatal("保存态摘要不一致", item, a)
		}
		if a.Result != nil {
			if item.StatusCode != a.Result.StatusCode || item.Truncated != a.Result.Truncated || item.EffectUnknown != a.Result.EffectUnknown {
				t.Fatal("回执摘要不一致", item, a.Result)
			}
		} else if item.StatusCode != 0 || item.Truncated || item.EffectUnknown {
			t.Fatal("没有回执不能伪造响应", item)
		}
		encoded, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"fixture-request-secret", "fixture-request-body", "fixture-reason", "fixture-response-secret", "fixture-error", "fixture-decision", "fixture-resolution", "body_base64", "request_hash"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatal("列表泄漏非摘要字段", secret)
			}
		}
		if !strings.Contains(string(encoded), "saved-query") {
			t.Fatal("原URL的query不应静默改写")
		}
		if strings.Contains(string(encoded), `"status_code"`) != (a.Result != nil && a.Result.StatusCode != 0) {
			t.Fatal("只在实际非零时返回 status_code", string(encoded))
		}
		detail, err := s.GetHTTPRecord(ctx, first.MissionID, a.ID)
		if err != nil || detail.MissionID != first.MissionID || detail.Thread.MissionID != first.MissionID || detail.Thread.ID != a.ThreadID || !reflect.DeepEqual(detail.Action, a) {
			t.Fatal("按需详情应保留原Action及完整字节", detail, err)
		}
	}
	for i, thread := range threads {
		if after := mustSnapshot(t, ctx, s, thread.ID); !reflect.DeepEqual(after, before[i]) {
			t.Fatal("列表/详情不得改变方向、动作、尝试或观察", thread.ID)
		}
	}
	eventsAfter, err := s.ListEvents(ctx, first.MissionID, 0)
	if err != nil || !reflect.DeepEqual(eventsBefore, eventsAfter) {
		t.Fatal("读取不得新增事件", eventsAfter, err)
	}
	// 外部状态迁移后重新读当前页/详情必须看到现态，不把创建游标当变更流。
	pending := actions[0]
	if _, err := s.DecideAction(ctx, pending.ID, "rejected", "人工拒绝fixture"); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListHTTPRecords(ctx, first.MissionID, domain.HTTPRecordQuery{ThreadID: pending.ThreadID})
	if err != nil || len(page.Items) != 1 || page.Items[0].Status != "rejected" || !page.Items[0].CreatedAt.Equal(pending.CreatedAt) || !page.Items[0].UpdatedAt.After(pending.UpdatedAt) {
		t.Fatal("重读应反映状态更新且不改变创建顺序", page, err)
	}
	detail, err := s.GetHTTPRecord(ctx, first.MissionID, pending.ID)
	if err != nil || detail.Action.Status != "rejected" || detail.Action.DecisionReason != "人工拒绝fixture" || detail.Action.Result != nil {
		t.Fatal(detail, err)
	}
	filtered, err := s.ListHTTPRecords(ctx, first.MissionID, domain.HTTPRecordQuery{ThreadID: pending.ThreadID, Status: "pending"})
	if err != nil || len(filtered.Items) != 0 {
		t.Fatal("旧状态筛选应反映当前状态", filtered, err)
	}
}
