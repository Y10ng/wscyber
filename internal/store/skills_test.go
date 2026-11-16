package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/modelprovider"
)

// 只由postgresStore随机隔离schema调用，历史升级fixture撤销最新空结构。
func dropSkillsFixture(t *testing.T, s *Store, ctx context.Context) {
	t.Helper()
	dropBriefFixture(t, s, ctx)
	_, err := s.pool.Exec(ctx, `DROP TABLE model_call_skills; DROP TABLE thread_skill_context; DROP TABLE skill_loads; DROP TABLE skill_activities; ALTER TABLE library_items DROP COLUMN classification; DELETE FROM wscyber_schema_migrations WHERE version=8`)
	if err != nil {
		t.Fatal(err)
	}
}
func skillInput(name, body string) domain.LibraryWrite {
	in := fileInput(name, body)
	in.Classification = "skill"
	in.Description = "本机状态检查方法"
	return in
}
func skillActivity(t *testing.T, s *Store, ctx context.Context, thread domain.Thread, kind string, r domain.SkillRequest) domain.SkillActivity {
	t.Helper()
	a := claimAttempt(t, ctx, s, thread.ID, 100)
	out, err := s.ApplySkillActivity(ctx, a.ID, kind, r)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishAttempt(ctx, a.ID, "succeeded", "paused", "已保存活动", "检查本地结果"); err != nil {
		t.Fatal(err)
	}
	return out
}
func loadRequest(item domain.LibraryItem) domain.SkillRequest {
	return domain.SkillRequest{ItemID: item.ID, SourceToken: item.Token, Purpose: "仅检查fixture"}
}
func TestPostgresSkillsNumericActivityOrder(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	var expected []string
	for range 12 {
		a := skillActivity(t, s, ctx, thread, "skill_discover", domain.SkillRequest{})
		expected = append(expected, a.Sequence)
		current, err := s.GetSkillContext(ctx, thread.MissionID, thread.ID)
		if err != nil || current.LastActivity == nil || current.LastActivity.Sequence != a.Sequence {
			t.Fatal("最近活动必须按数字排序", current, err)
		}
	}
	var got []string
	after := ""
	for {
		page, err := s.ListSkillActivities(ctx, thread.MissionID, after, 3)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range page.Items {
			got = append(got, a.Sequence)
		}
		if page.NextCursor == "" {
			break
		}
		after = page.NextCursor
	}
	if !reflect.DeepEqual(expected, got) {
		t.Fatal("跨十位分页丢失或重复活动", got, expected)
	}
}
func TestPostgresSkillsClassificationUpgradeAndReplay(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	// 用007实际列模拟历史条目；校验省略新字段的序列化和当前回执，不冒充旧二进制回执测试。
	dropSkillsFixture(t, s, ctx)
	old := fileInput("SKILL.md", "---\nname: private\n---\n普通正文")
	raw, _ := json.Marshal(old)
	if strings.Contains(string(raw), "classification") {
		t.Fatal("旧省略输入指纹改变")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO library_items(id,kind,name,format,content,content_sha256,byte_size,token) VALUES($1,'file','SKILL.md','markdown',$2,repeat('a',64),$3,$4)`, materialID(1), old.Content, len(old.Content), materialID(2))
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	item, err := s.GetLibraryItem(ctx, materialID(1))
	if err != nil || item.Classification != "material" {
		t.Fatal(item, err)
	}
	old.ExpectedToken = item.Token
	saved, err := s.SaveLibraryItem(ctx, startKey, item.ID, old)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.SaveLibraryItem(ctx, startKey, item.ID, old)
	if err != nil || !reflect.DeepEqual(saved, again) {
		t.Fatal("旧请求不能回放", err)
	}
	old.ExpectedToken = saved.Item.Token
	old.Classification = "skill"
	old.Description = "适用范围"
	item.LibraryItem = saveMaterial(t, s, ctx, 1, old)
	old.ExpectedToken = item.Token
	old.Classification = ""
	old.Content = "保留分类"
	got := saveMaterial(t, s, ctx, 1, old)
	if got.Classification != "skill" {
		t.Fatal(got)
	}
	old.ExpectedToken = got.Token
	old.Description = ""
	if _, err = s.SaveLibraryItem(ctx, materialID(99), item.ID, old); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("省略分类也须保留Skill说明校验", err)
	}
	old.Classification = "material"
	got = saveMaterial(t, s, ctx, 1, old)
	if got.Classification != "material" {
		t.Fatal(got)
	}
	replay, err := s.GetLibraryOperation(ctx, startKey)
	if err != nil || !reflect.DeepEqual(saved, replay) {
		t.Fatal("历史回执被改", err)
	}
}
func TestPostgresSkillsIsolationPaginationAndFailureFacts(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	ordinary := saveMaterial(t, s, ctx, 1, fileInput("SKILL.md", "PRIVATE_BODY"))
	a := saveMaterial(t, s, ctx, 2, skillInput("first%.md", "METHOD_BODY"))
	b := saveMaterial(t, s, ctx, 3, skillInput("second.md", "METHOD_BODY_2"))
	discover := func(r domain.SkillRequest) domain.SkillActivity {
		return skillActivity(t, s, ctx, thread, "skill_discover", r)
	}
	page := discover(domain.SkillRequest{Limit: 1})
	if page.Result.Status != "discovered" || len(page.Result.Items) != 1 || page.Result.Items[0].ID != a.ID || page.Result.NextCursor != a.ID {
		t.Fatal(page)
	}
	next := discover(domain.SkillRequest{After: page.Result.NextCursor, Limit: 1})
	if len(next.Result.Items) != 1 || next.Result.Items[0].ID != b.ID || next.Result.NextCursor != "" {
		t.Fatal(next)
	}
	literal := discover(domain.SkillRequest{Query: "%"})
	if len(literal.Result.Items) != 1 {
		t.Fatal(literal)
	}
	empty := discover(domain.SkillRequest{Query: "METHOD_BODY"})
	if len(empty.Result.Items) != 0 {
		t.Fatal("发现搜索了正文")
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "BODY") || strings.Contains(string(raw), ordinary.Name) {
		t.Fatal("目录泄漏非显式内容")
	}
	for _, tt := range []struct {
		r      domain.SkillRequest
		status string
	}{
		{loadRequest(ordinary), "unavailable"},
		{domain.SkillRequest{ItemID: a.ID, SourceToken: materialID(88), Purpose: "陈旧身份"}, "stale"},
		{domain.SkillRequest{ItemID: materialID(90), SourceToken: materialID(91), Purpose: "不存在"}, "unavailable"},
	} {
		out := skillActivity(t, s, ctx, thread, "skill_load", tt.r)
		if out.Result.Status != tt.status {
			t.Fatal(out)
		}
	}
	usage, err := s.SkillUsage(ctx, thread.MissionID, "", 25)
	if err != nil || usage.TotalSuccessfulLoads != "0" {
		t.Fatal(usage, err)
	}
	activities, err := s.ListSkillActivities(ctx, thread.MissionID, "", 2)
	if err != nil || len(activities.Items) != 2 || activities.NextCursor == "" {
		t.Fatal(activities, err)
	}
	second, err := s.ListSkillActivities(ctx, thread.MissionID, activities.NextCursor, 100)
	if err != nil || len(second.Items) != 5 {
		t.Fatal(second, err)
	}
}
func TestPostgresSkillsConcurrentReplayFrozenHistoryAndRelease(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	folder := saveMaterial(t, s, ctx, 1, domain.LibraryWrite{Kind: "folder", Name: "方法"})
	in := skillInput("check.md", "原件\r\n<script>不执行</script>")
	in.ParentID = folder.ID
	item := saveMaterial(t, s, ctx, 2, in)
	a := claimAttempt(t, ctx, s, thread.ID, 100)
	req := loadRequest(item)
	results := make(chan domain.SkillActivity, 5)
	errs := make(chan error, 5)
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() { v, e := s.ApplySkillActivity(ctx, a.ID, "skill_load", req); results <- v; errs <- e })
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first domain.SkillActivity
	for v := range results {
		if first.AttemptID == "" {
			first = v
		}
		if !reflect.DeepEqual(first, v) {
			t.Fatal("并发回执不一致")
		}
	}
	if first.Result.Status != "loaded" || first.Result.LoadID != a.ID {
		t.Fatal(first)
	}
	req.Purpose = "不同请求"
	if _, err := s.ApplySkillActivity(ctx, a.ID, "skill_load", req); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if err := s.FinishAttempt(ctx, a.ID, "succeeded", "paused", "完成", "检查"); err != nil {
		t.Fatal(err)
	}
	replay, err := s.ApplySkillActivity(ctx, a.ID, "skill_load", loadRequest(item))
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatal("结束后回放失败", err)
	}
	in.ExpectedToken = item.Token
	in.Name = "changed.md"
	in.Content = "changed"
	in.Classification = "material"
	updated := saveMaterial(t, s, ctx, 2, in)
	if _, err = s.DeleteLibraryItem(ctx, materialID(90), item.ID, domain.LibraryDelete{ExpectedToken: updated.Token}); err != nil {
		t.Fatal(err)
	}
	frozen, err := s.GetSkillLoad(ctx, thread.MissionID, a.ID)
	if err != nil || frozen.Content != "原件\r\n<script>不执行</script>" || frozen.Path != "方法/check.md" || frozen.SourceToken != item.Token {
		t.Fatal(frozen, err)
	}
	for range 3 {
		c, e := s.GetSkillContext(ctx, thread.MissionID, thread.ID)
		if e != nil || len(c.Active) != 1 || c.Active[0].Content != frozen.Content {
			t.Fatal(c, e)
		}
	}
	other := fixtureThread(t, ctx, s)
	if _, err = s.GetSkillLoad(ctx, other.MissionID, a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("跨项目原件泄漏", err)
	}
	if _, err = s.GetSkillContext(ctx, other.MissionID, thread.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	notActive := skillActivity(t, s, ctx, other, "skill_release", domain.SkillRequest{LoadID: a.ID})
	if notActive.Result.Status != "not_active" {
		t.Fatal(notActive)
	}
	released := skillActivity(t, s, ctx, thread, "skill_release", domain.SkillRequest{LoadID: a.ID})
	if released.Result.Status != "released" {
		t.Fatal(released)
	}
	active, err := s.GetSkillContext(ctx, thread.MissionID, thread.ID)
	if err != nil || len(active.Active) != 0 {
		t.Fatal(active, err)
	}
	usage, err := s.SkillUsage(ctx, thread.MissionID, "", 25)
	if err != nil || usage.TotalSuccessfulLoads != "1" || usage.Items[0].Name != "check.md" {
		t.Fatal(usage, err)
	}
	if _, err = s.pool.Exec(ctx, `UPDATE skill_loads SET content='changed' WHERE load_id=$1`, a.ID); err == nil {
		t.Fatal("固定历史允许更新")
	}
	if _, err = s.pool.Exec(ctx, `DELETE FROM skill_activities WHERE attempt_id=$1`, a.ID); err == nil {
		t.Fatal("固定活动允许删除")
	}
}
func TestPostgresSkillsCapacityAndRepeatedExplicitLoads(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		allowed    int
	}{{"count", "small", 4}, {"bytes", strings.Repeat("x", 32768), 2}, {"escaped", strings.Repeat("<", 17000), 0}} {
		t.Run(tt.name, func(t *testing.T) {
			s, ctx, _ := postgresStore(t)
			thread := fixtureThread(t, ctx, s)
			item := saveMaterial(t, s, ctx, 1, skillInput("capacity.md", tt.body))
			var last string
			for range tt.allowed {
				out := skillActivity(t, s, ctx, thread, "skill_load", loadRequest(item))
				if out.Result.Status != "loaded" {
					t.Fatal(out)
				}
				last = out.Result.LoadID
			}
			out := skillActivity(t, s, ctx, thread, "skill_load", loadRequest(item))
			if out.Result.Status != "capacity" {
				t.Fatal(out)
			}
			if last != "" {
				skillActivity(t, s, ctx, thread, "skill_release", domain.SkillRequest{LoadID: last})
				out = skillActivity(t, s, ctx, thread, "skill_load", loadRequest(item))
				if out.Result.Status != "loaded" {
					t.Fatal(out)
				}
			}
			usage, err := s.SkillUsage(ctx, thread.MissionID, "", 25)
			want := "0"
			if tt.allowed == 4 {
				want = "5"
			}
			if tt.allowed == 2 {
				want = "3"
			}
			if err != nil || usage.TotalSuccessfulLoads != want {
				t.Fatal(usage, err)
			}
		})
	}
}
func TestPostgresSkillsCallAssociationAtomicAndRecovery(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	thread := fixtureThread(t, ctx, s)
	item := saveMaterial(t, s, ctx, 1, skillInput("method.md", "persistent method"))
	loaded := skillActivity(t, s, ctx, thread, "skill_load", loadRequest(item))
	loadID := loaded.Result.LoadID
	a := claimAttempt(t, ctx, s, thread.ID, 100)
	call := domain.ModelCall{ID: ledgerID(t), ModelPriceKey: domain.ModelPriceKey{Source: "environment", Protocol: "openai_chat", Model: "fixture"}, Purpose: "research", MissionID: thread.MissionID, ThreadID: thread.ID, AttemptID: a.ID}
	// 关联不存在、同Attempt或重复加载身份，Begin整事务回滚。
	for _, ids := range [][]string{{a.ID}, {loadID, loadID}} {
		if _, err := s.BeginModelCall(ctx, domain.BeginModelCall{Call: call, SkillLoadIDs: ids}); err == nil {
			t.Fatal("错误关联被接受")
		}
		if _, err := s.GetModelCall(ctx, call.ID, thread.MissionID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("失败Begin留记录", err)
		}
	}
	began, err := s.BeginModelCall(ctx, domain.BeginModelCall{Call: call, SkillLoadIDs: []string{loadID}})
	if err != nil {
		t.Fatal(err)
	}
	calls, err := s.SkillModelCalls(ctx, thread.MissionID, loadID, "", 25)
	if err != nil || len(calls.Items) != 1 || calls.Items[0].Attempted != nil || calls.Items[0].State != "running" {
		t.Fatal(calls, err)
	}
	if _, err = s.BeginModelCall(ctx, domain.BeginModelCall{Call: call, SkillLoadIDs: []string{loadID}}); err == nil {
		t.Fatal("同Attempt再次调用")
	}
	if err = s.FinishModelCall(ctx, domain.FinishModelCall{ID: began.ID, Outcome: "not_sent", Attempted: false, FinishedAt: time.Now(), Usage: modelprovider.MissingUsage(), Cost: began.Cost}); err != nil {
		t.Fatal(err)
	}
	calls, err = s.SkillModelCalls(ctx, thread.MissionID, loadID, "", 25)
	if err != nil || calls.Items[0].Outcome != "not_sent" || calls.Items[0].Attempted == nil || *calls.Items[0].Attempted {
		t.Fatal(calls, err)
	}
	if err = s.FinishAttempt(ctx, a.ID, "succeeded", "paused", "完成", "继续"); err != nil {
		t.Fatal(err)
	}
	a = claimAttempt(t, ctx, s, thread.ID, 100)
	call.ID = ledgerID(t)
	call.AttemptID = a.ID
	if _, err = s.BeginModelCall(ctx, domain.BeginModelCall{Call: call, SkillLoadIDs: []string{loadID}}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err = s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	calls, err = s.SkillModelCalls(ctx, thread.MissionID, loadID, "", 100)
	if err != nil || len(calls.Items) != 2 {
		t.Fatal(calls, err)
	}
	for _, c := range calls.Items {
		if c.ID == call.ID && (c.State != "interrupted" || c.Attempted != nil) {
			t.Fatal(c)
		}
	}
	usage, err := s.SkillUsage(ctx, thread.MissionID, "", 25)
	if err != nil || usage.TotalSuccessfulLoads != "1" {
		t.Fatal(usage, err)
	}
	frozen, err := s.GetSkillLoad(ctx, thread.MissionID, loadID)
	if err != nil || frozen.Content != "persistent method" {
		t.Fatal(frozen, err)
	}
	var n int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM model_call_contents`).Scan(&n); err != nil || n != 0 {
		t.Fatal("关闭归档仍存整次原文", n, err)
	}
}
