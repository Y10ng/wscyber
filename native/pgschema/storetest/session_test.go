package storetest_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	types "github.com/chainreactors/cyber/core/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestSessionCRUD(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
	record := sessionRecord("s1", "node-a", "第一个会话", now)
	record.AgentName = "cyber"
	if err := f.store.CreateSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := f.store.CreateSession(ctx, record); err == nil {
		t.Fatal("重复 CreateSession 未报错")
	}
	got, err := f.store.GetSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(got, record) {
		t.Fatalf("GetSession = %v, want %v", got, record)
	}
	if _, err := f.store.GetSession(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetSession(missing) err = %v, want sql.ErrNoRows", err)
	}

	got.Session.Title = "改名"
	got.Session.NodeId = "node-b"
	got.Archived = true
	got.UpdatedAt = record.UpdatedAt
	if err := f.store.UpdateSession(ctx, got); err != nil {
		t.Fatal(err)
	}
	var title, node string
	var archived bool
	if err := f.raw.QueryRowContext(ctx, `SELECT title, node_id, archived FROM chat_sessions WHERE id = 's1'`).Scan(&title, &node, &archived); err != nil {
		t.Fatal(err)
	}
	if title != "改名" || node != "node-b" || !archived {
		t.Fatalf("relational projection = %q %q %v", title, node, archived)
	}
	listed, err := f.store.ListSessions(ctx, 10)
	if err != nil || len(listed) != 1 || listed[0].GetSession().GetTitle() != "改名" {
		t.Fatalf("ListSessions = %v, %v", listed, err)
	}
	if err := f.store.DeleteSession(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetSession(ctx, "s1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("deleted session err = %v", err)
	}
	if err := f.store.DeleteSession(ctx, "s1"); err != nil {
		t.Fatalf("删除不存在的会话应无错误（与上游一致）: %v", err)
	}
}

func TestListSessionPagePaginationAndFilters(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	base := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	for i := range 125 {
		record := sessionRecord(fmt.Sprintf("s-%03d", i), "offline", fmt.Sprintf("Task %03d", i), base.Add(time.Duration(i)*time.Second))
		if err := f.store.CreateSession(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	page, more, err := f.store.ListSessionPage(ctx, 0, 100, &types.ListSessionsRequest{IncludeClosed: true})
	if err != nil || len(page) != 100 || !more {
		t.Fatalf("first page %d more=%v err=%v", len(page), more, err)
	}
	if page[0].GetSession().GetId() != "s-124" || page[99].GetSession().GetId() != "s-025" {
		t.Fatalf("排序应为 updated_at DESC: first=%s last=%s", page[0].GetSession().GetId(), page[99].GetSession().GetId())
	}
	page, more, err = f.store.ListSessionPage(ctx, 100, 100, &types.ListSessionsRequest{IncludeClosed: true})
	if err != nil || len(page) != 25 || more {
		t.Fatalf("second page %d more=%v err=%v", len(page), more, err)
	}
	// limit ≤ 0 → 100；limit > 500 → 500；offset < 0 → 0；nil filters 只列 open 未归档。
	page, more, err = f.store.ListSessionPage(ctx, -5, 0, nil)
	if err != nil || len(page) != 100 || !more {
		t.Fatalf("default page %d more=%v err=%v", len(page), more, err)
	}
	page, _, err = f.store.ListSessionPage(ctx, 0, 10000, nil)
	if err != nil || len(page) != 125 {
		t.Fatalf("capped page %d err=%v", len(page), err)
	}

	// 标题搜索大小写不敏感，在分页前过滤。
	page, _, err = f.store.ListSessionPage(ctx, 0, 10, &types.ListSessionsRequest{Search: "tASK 007", NodeId: "offline"})
	if err != nil || len(page) != 1 || page[0].GetSession().GetId() != "s-007" {
		t.Fatalf("search: %v %v", page, err)
	}
	page, _, err = f.store.ListSessionPage(ctx, 0, 10, &types.ListSessionsRequest{Search: "task 00"})
	if err != nil || len(page) != 10 {
		t.Fatalf("search prefix: %d %v", len(page), err)
	}
	page, _, err = f.store.ListSessionPage(ctx, 0, 10, &types.ListSessionsRequest{Search: "%"})
	if err != nil || len(page) != 0 {
		t.Fatalf("search 不应把 %% 当通配符: %d %v", len(page), err)
	}
	page, _, err = f.store.ListSessionPage(ctx, 0, 10, &types.ListSessionsRequest{NodeId: "other"})
	if err != nil || len(page) != 0 {
		t.Fatalf("node filter: %d %v", len(page), err)
	}
	// 没有 scan 模块时按目标过滤直接返回空（与上游一致）。
	page, more, err = f.store.ListSessionPage(ctx, 0, 10, &types.ListSessionsRequest{Target: "https://x.test"})
	if err != nil || page != nil || more {
		t.Fatalf("target without scan module: %v %v %v", page, more, err)
	}

	// 归档与关闭状态过滤。
	record, err := f.store.GetSession(ctx, "s-000")
	if err != nil {
		t.Fatal(err)
	}
	record.Archived = true
	if err := f.store.UpdateSession(ctx, record); err != nil {
		t.Fatal(err)
	}
	closed, err := f.store.GetSession(ctx, "s-001")
	if err != nil {
		t.Fatal(err)
	}
	closed.Session.State = "closed"
	if err := f.store.UpdateSession(ctx, closed); err != nil {
		t.Fatal(err)
	}
	page, _, err = f.store.ListSessionPage(ctx, 0, 10, &types.ListSessionsRequest{Archived: proto.Bool(true)})
	if err != nil || len(page) != 1 || !page[0].Archived {
		t.Fatalf("archived: %v %v", page, err)
	}
	page, _, err = f.store.ListSessionPage(ctx, 0, 500, &types.ListSessionsRequest{})
	if err != nil || len(page) != 123 {
		t.Fatalf("open+unarchived = %d, %v", len(page), err)
	}
	page, _, err = f.store.ListSessionPage(ctx, 0, 500, &types.ListSessionsRequest{IncludeClosed: true})
	if err != nil || len(page) != 124 {
		t.Fatalf("include closed = %d, %v", len(page), err)
	}
}

// 中文标题与 Unicode 大小写；同一 updated_at 时按 id 字节序倒序。
func TestListSessionPageSearchUnicodeAndTieOrder(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()
	at := time.Date(2026, 11, 22, 8, 0, 0, 0, time.UTC)
	for _, rec := range []*types.SessionRecord{
		sessionRecord("s-a", "n", "登录接口越权测试", at),
		sessionRecord("sa", "n", "Admin Panel", at),
		sessionRecord("S-b", "n", "其它", at),
	} {
		if err := f.store.CreateSession(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	page, _, err := f.store.ListSessionPage(ctx, 0, 10, &types.ListSessionsRequest{Search: "越权"})
	if err != nil || len(page) != 1 || page[0].GetSession().GetId() != "s-a" {
		t.Fatalf("中文搜索: %v %v", page, err)
	}
	page, _, err = f.store.ListSessionPage(ctx, 0, 10, &types.ListSessionsRequest{Search: "ADMIN"})
	if err != nil || len(page) != 1 || page[0].GetSession().GetId() != "sa" {
		t.Fatalf("大小写不敏感: %v %v", page, err)
	}
	page, _, err = f.store.ListSessionPage(ctx, 0, 10, nil)
	if err != nil || len(page) != 3 {
		t.Fatal(page, err)
	}
	var ids []string
	for _, s := range page {
		ids = append(ids, s.GetSession().GetId())
	}
	if want := []string{"sa", "s-a", "S-b"}; fmt.Sprint(ids) != fmt.Sprint(want) {
		t.Fatalf("同时间按 id 字节序倒序 = %v, want %v", ids, want)
	}
}

func TestSessionJSONDropsRetiredScanKeysAndDerivedExtensions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	createSession(t, ctx, f.store, "s1")
	session, err := f.store.GetSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	// 派生的 scan 扩展不落库。
	section, _ := structpb.NewStruct(map[string]any{"ids": []any{"scan-1"}})
	session.Extensions = map[string]*structpb.Struct{"scan": section}
	if err := f.store.UpdateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := f.raw.QueryRowContext(ctx, `SELECT session_json FROM chat_sessions WHERE id = 's1'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var saved map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	if _, ok := saved["extensions"]; ok {
		t.Fatalf("derived scan association persisted: %s", raw)
	}
	// 旧记录里的 scanIds/scan_ids 被忽略。
	saved["scanIds"] = json.RawMessage(`["old-scan"]`)
	saved["scan_ids"] = json.RawMessage(`["old-scan"]`)
	legacy, _ := json.Marshal(saved)
	if _, err := f.raw.ExecContext(ctx, `UPDATE chat_sessions SET session_json = ? WHERE id = 's1'`, string(legacy)); err != nil {
		t.Fatal(err)
	}
	loaded, err := f.store.GetSession(ctx, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Extensions["scan"] != nil {
		t.Fatalf("core-only store backfilled scan: %v", loaded.Extensions)
	}
	// 其它未知字段仍报错。
	saved["unexpectedField"] = json.RawMessage(`true`)
	invalid, _ := json.Marshal(saved)
	if _, err := f.raw.ExecContext(ctx, `UPDATE chat_sessions SET session_json = ? WHERE id = 's1'`, string(invalid)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.GetSession(ctx, "s1"); err == nil {
		t.Fatal("unexpected session field was accepted")
	}
}

func TestCreateSessionRequiresSession(t *testing.T) {
	f := newFixture(t)
	if err := f.store.CreateSession(context.Background(), &types.SessionRecord{}); err == nil {
		t.Fatal("CreateSession 接受了空 session")
	}
	if err := f.store.UpdateSession(context.Background(), nil); err == nil {
		t.Fatal("UpdateSession 接受了 nil")
	}
}
