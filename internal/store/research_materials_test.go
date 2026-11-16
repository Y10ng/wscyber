package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"wscyber/internal/domain"
)

// 仅postgresStore创建的隔离schema；模拟历史安装时先撤去008/007，不改旧迁移文本。
func dropResearchFixture(t *testing.T, s *Store, ctx context.Context) {
	t.Helper()
	dropSkillsFixture(t, s, ctx)
	if _, err := s.pool.Exec(ctx, `DROP TABLE mission_materials; DROP TABLE mission_research_configs; DROP TABLE library_operations; DROP TABLE library_items; DELETE FROM wscyber_schema_migrations WHERE version=7`); err != nil {
		t.Fatal(err)
	}
}
func materialID(n int) string { return fmt.Sprintf("%032x", n) }
func saveMaterial(t *testing.T, s *Store, ctx context.Context, n int, in domain.LibraryWrite) domain.LibraryItem {
	t.Helper()
	key, err := libraryToken()
	if err != nil {
		t.Fatal(err)
	}
	v, err := s.SaveLibraryItem(ctx, key, materialID(n), in)
	if err != nil {
		t.Fatal(err)
	}
	return *v.Item
}
func fileInput(name, content string) domain.LibraryWrite {
	return domain.LibraryWrite{Kind: "file", Name: name, Content: content}
}
func TestPostgresLibraryCASReplayAndMetadata(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	in := fileInput("SKILL.md", "---\r\nname: local\r\n---\r\n正文_SENTINEL\n")
	const n = 5
	var wg sync.WaitGroup
	values := make(chan domain.LibraryMutation, n)
	errs := make(chan error, n)
	for range n {
		wg.Go(func() { v, e := s.SaveLibraryItem(ctx, startKey, materialID(1), in); values <- v; errs <- e })
	}
	wg.Wait()
	close(values)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first domain.LibraryMutation
	for v := range values {
		if first.Item == nil {
			first = v
		}
		if !reflect.DeepEqual(first, v) {
			t.Fatal("并发回放不一致")
		}
	}
	got, err := s.GetLibraryItem(ctx, materialID(1))
	if err != nil || got.Content != in.Content || got.ByteSize != len(in.Content) {
		t.Fatal(got, err)
	}
	page, err := s.ListLibraryItems(ctx, domain.LibraryQuery{Limit: 25})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "正文_SENTINEL") || strings.Contains(string(raw), `"content":`) {
		t.Fatal("列表泄漏正文")
	}
	change := in
	change.Content = "新正文"
	if _, err = s.SaveLibraryItem(ctx, startKey, materialID(1), change); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	change.ExpectedToken = got.Token
	updated := saveMaterial(t, s, ctx, 1, change)
	if updated.Token == got.Token {
		t.Fatal("保存未更新CAS")
	}
	if _, err = s.SaveLibraryItem(ctx, materialID(101), materialID(1), change); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("旧CAS应失败", err)
	}
	if _, err = s.DeleteLibraryItem(ctx, materialID(102), materialID(1), domain.LibraryDelete{ExpectedToken: got.Token}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	deleted, err := s.DeleteLibraryItem(ctx, materialID(103), materialID(1), domain.LibraryDelete{ExpectedToken: updated.Token})
	if err != nil || !deleted.Deleted {
		t.Fatal(err)
	}
	if _, err = s.GetLibraryItem(ctx, materialID(1)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	replay, err := s.SaveLibraryItem(ctx, startKey, materialID(1), in)
	if err != nil || !reflect.DeepEqual(replay, first) {
		t.Fatal("删除后回执必须仍可回放", err)
	}
	if receipt, err := s.GetLibraryOperation(ctx, startKey); err != nil || !reflect.DeepEqual(receipt, first) {
		t.Fatal(receipt, err)
	}
	if _, err = s.SaveLibraryItem(ctx, materialID(104), materialID(1), in); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("墓碑ID不得复用", err)
	}
	for _, sql := range []string{`UPDATE library_operations SET method='DELETE'`, `DELETE FROM library_operations`} {
		if _, err = s.pool.Exec(ctx, sql); err == nil {
			t.Fatal("回执不应可变")
		}
	}
}
func TestPostgresLibraryFolderBounds(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	parent := ""
	folders := []domain.LibraryItem{}
	for n := 1; n <= 8; n++ {
		f := saveMaterial(t, s, ctx, n, domain.LibraryWrite{Kind: "folder", Name: fmt.Sprint("目录", n), ParentID: parent})
		folders = append(folders, f)
		parent = f.ID
	}
	if _, err := s.SaveLibraryItem(ctx, materialID(100), materialID(9), domain.LibraryWrite{Kind: "folder", Name: "过深", ParentID: parent}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	in := fileInput("文本.txt", "正文")
	in.ParentID = parent
	f := saveMaterial(t, s, ctx, 10, in)
	if _, err := s.DeleteLibraryItem(ctx, materialID(101), parent, domain.LibraryDelete{ExpectedToken: folders[7].Token}); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("非空目录不可删除", err)
	}
	if _, err := s.SaveLibraryItem(ctx, materialID(102), folders[0].ID, domain.LibraryWrite{Kind: "folder", Name: "目录1", ParentID: folders[1].ID, ExpectedToken: folders[0].Token}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("循环移动被接收", err)
	}
	other := saveMaterial(t, s, ctx, 20, domain.LibraryWrite{Kind: "folder", Name: "另一目录"})
	if _, err := s.SaveLibraryItem(ctx, materialID(103), folders[0].ID, domain.LibraryWrite{Kind: "folder", Name: "目录1", ParentID: other.ID, ExpectedToken: folders[0].Token}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("移动子树超深被接收", err)
	}
	if _, err := s.SaveLibraryItem(ctx, materialID(104), materialID(30), in); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("同目录同名被接收", err)
	}
	in.Name = "子.txt"
	in.ParentID = f.ID
	if _, err := s.SaveLibraryItem(ctx, materialID(105), materialID(31), in); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	page, err := s.ListLibraryItems(ctx, domain.LibraryQuery{All: true, Limit: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor == "" {
		t.Fatal(page, err)
	}
	next, err := s.ListLibraryItems(ctx, domain.LibraryQuery{All: true, Limit: 2, After: page.NextCursor})
	if err != nil || next.Items[0].ID <= page.NextCursor {
		t.Fatal(next, err)
	}
	literal, err := s.ListLibraryItems(ctx, domain.LibraryQuery{All: true, Search: "%", Limit: 25})
	if err != nil || len(literal.Items) != 0 {
		t.Fatal("查询不应把百分号当通配符", err)
	}
	selection := startInputMaterial(f)
	started, err := s.StartExploration(ctx, startKey, selection, true)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := s.GetResearchConfiguration(ctx, started.Mission.ID)
	if err != nil || !strings.HasSuffix(cfg.Materials[0].Path, "目录8/文本.txt") {
		t.Fatal(cfg, err)
	}
}
func startInputMaterial(items ...domain.LibraryItem) domain.ExplorationInput {
	in := startInput()
	for _, x := range items {
		in.Materials = append(in.Materials, domain.MaterialSelection{ID: x.ID, Token: x.Token})
	}
	return in
}
func TestPostgresResearchFixedSelectionAndReplay(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	a := saveMaterial(t, s, ctx, 1, fileInput("a.md", "原件\r\n"))
	b := saveMaterial(t, s, ctx, 2, fileInput("b.txt", "未选正文"))
	in := startInputMaterial(a)
	in.SafetyPromptEnabled = true
	in.SafetyPrompt = "禁止写入目标"
	started, err := s.StartExploration(ctx, startKey, in, true)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.GetTaskMaterial(ctx, started.Mission.ID, a.ID)
	if err != nil || before.Content != "原件\r\n" || before.SourceToken != a.Token {
		t.Fatal(before, err)
	}
	cfg, err := s.GetResearchConfiguration(ctx, started.Mission.ID)
	if err != nil || len(cfg.Materials) != 1 || cfg.SafetyPrompt != in.SafetyPrompt {
		t.Fatal(cfg, err)
	}
	snapshot := mustSnapshot(t, ctx, s, started.Thread.ID)
	if snapshot.ResearchContext != nil {
		t.Fatal("普通Snapshot不应加载资料正文")
	}
	if _, err = s.GetTaskMaterial(ctx, started.Mission.ID, b.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("未选资料不能从任务读取", err)
	}
	update := fileInput("renamed.md", "新正文")
	update.ExpectedToken = a.Token
	a2 := saveMaterial(t, s, ctx, 1, update)
	if _, err = s.DeleteLibraryItem(ctx, materialID(201), a.ID, domain.LibraryDelete{ExpectedToken: a2.Token}); err != nil {
		t.Fatal(err)
	}
	if after, err := s.GetTaskMaterial(ctx, started.Mission.ID, a.ID); err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("库删除污染固定材料", err)
	}
	replay, err := s.StartExploration(ctx, startKey, in, false)
	if err != nil || !reflect.DeepEqual(replay, started) {
		t.Fatal("历史回放应先于过期资料与模型可用性", err)
	}
	if _, err = s.StartExploration(ctx, materialID(202), in, true); !errors.Is(err, domain.ErrMaterialSelectionStale) {
		t.Fatal("新任务过期选择缺少稳定错误", err)
	}
	if _, err = s.GetOperation(ctx, materialID(202)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("过期选择不能提交回执", err)
	}
	if _, err = s.GetTaskMaterial(ctx, "not-this-mission", a.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	for _, sql := range []string{`UPDATE mission_materials SET name='changed'`, `DELETE FROM mission_materials`, `UPDATE mission_research_configs SET safety_prompt='changed'`, `DELETE FROM mission_research_configs`} {
		if _, err = s.pool.Exec(ctx, sql); err == nil {
			t.Fatal("任务配置不可变")
		}
	}
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	fixed, err := s.GetResearchContext(ctx, started.Mission.ID)
	if err != nil || len(fixed.Materials) != 1 || fixed.Materials[0].Content != before.Content || !fixed.SafetyPromptEnabled {
		t.Fatal(fixed, err)
	}
}
func TestPostgresResearchAtomicAndCanonical(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	a := saveMaterial(t, s, ctx, 1, fileInput("a.md", "A"))
	b := saveMaterial(t, s, ctx, 2, fileInput("b.md", "B"))
	in := startInputMaterial(b, a)
	got, err := s.StartExploration(ctx, startKey, in, true)
	if err != nil {
		t.Fatal(err)
	}
	if in.Materials[0].ID != b.ID {
		t.Fatal("不能重排调用者数组")
	}
	replay, err := s.StartExploration(ctx, startKey, startInputMaterial(a, b), true)
	if err != nil || replay.Mission.ID != got.Mission.ID {
		t.Fatal("选材顺序应canonical", err)
	}
	if _, err = s.pool.Exec(ctx, `CREATE FUNCTION fail_material_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture rollback'; END $$;CREATE TRIGGER fail_material_insert BEFORE INSERT ON mission_materials FOR EACH ROW EXECUTE FUNCTION fail_material_insert()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.StartExploration(ctx, materialID(300), in, true); err == nil {
		t.Fatal("故障应回滚")
	}
	missions, err := s.ListMissions(ctx)
	if err != nil || len(missions) != 1 {
		t.Fatal("快照失败遗留任务", err)
	}
	if _, err = s.GetOperation(ctx, materialID(300)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `DROP TRIGGER fail_material_insert ON mission_materials`); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", domain.MaxMaterialBytes)
	a = saveMaterial(t, s, ctx, 10, fileInput("large1.md", big))
	b = saveMaterial(t, s, ctx, 11, fileInput("large2.md", big))
	c := saveMaterial(t, s, ctx, 12, fileInput("extra.md", "x"))
	if _, err = s.StartExploration(ctx, materialID(301), startInputMaterial(a, b), true); err != nil {
		t.Fatal("精确上限应该成功", err)
	}
	if _, err = s.StartExploration(ctx, materialID(302), startInputMaterial(a, b, c), true); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal("总正文越界应原子拒绝", err)
	}
}
func TestPostgresResearchV6Upgrade(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	started, err := s.StartExploration(ctx, startKey, startInput(), true)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(mustSnapshot(t, ctx, s, started.Thread.ID))
	dropResearchFixture(t, s, ctx)
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	after, _ := json.Marshal(mustSnapshot(t, ctx, s, started.Thread.ID))
	if string(before) != string(after) {
		t.Fatal("迁移改变旧快照")
	}
	cfg, err := s.GetResearchConfiguration(ctx, started.Mission.ID)
	if err != nil || cfg.SafetyPromptEnabled || cfg.SafetyPrompt != "" || len(cfg.Materials) != 0 {
		t.Fatal(cfg, err)
	}
	replay, err := s.StartExploration(ctx, startKey, startInput(), false)
	if err != nil || replay.Mission.ID != started.Mission.ID {
		t.Fatal("旧省略字段指纹未兼容", err)
	}
}
