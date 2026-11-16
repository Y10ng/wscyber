package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"wscyber/internal/domain"
)

// 仅随机隔离schema的历史升级fixture使用，不接触业务库或旧运行实例。
func dropBriefFixture(t *testing.T, s *Store, ctx context.Context) {
	t.Helper()
	dropControlsFixture(t, s, ctx)
	if _, err := s.pool.Exec(ctx, `DROP TABLE mission_research_briefs; DELETE FROM wscyber_schema_migrations WHERE version=9`); err != nil {
		t.Fatal(err)
	}
}
func fixedBrief(share bool) *domain.ResearchBrief {
	return &domain.ResearchBrief{Identities: []domain.ResearchIdentity{{Label: "成员A", Role: "普通成员", Ownership: "本人记录"}}, BusinessRules: "BRIEF_SENTINEL\r\n用户声明", ShareWithModel: &share}
}
func TestPostgresBriefConcurrentReplayAndIsolation(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	in := startInput()
	in.ResearchBrief = fixedBrief(false)
	const n = 4
	var wg sync.WaitGroup
	results := make(chan domain.ExplorationStart, n)
	errs := make(chan error, n)
	for range n {
		wg.Go(func() { v, e := s.StartExploration(ctx, startKey, in, true); results <- v; errs <- e })
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var first domain.ExplorationStart
	for v := range results {
		if first.Mission.ID == "" {
			first = v
		}
		if !reflect.DeepEqual(first, v) {
			t.Fatal("同键回执不一致")
		}
	}
	cfg, err := s.GetResearchConfiguration(ctx, first.Mission.ID)
	if err != nil || !reflect.DeepEqual(cfg.ResearchBrief, in.ResearchBrief) {
		t.Fatal("固定配置不一致", err)
	}
	model, err := s.GetResearchContext(ctx, first.Mission.ID)
	if err != nil || model.ResearchBrief != nil {
		t.Fatal("false外发", err)
	}
	for _, sql := range []string{`UPDATE mission_research_briefs SET content=content`, `DELETE FROM mission_research_briefs`} {
		if _, err = s.pool.Exec(ctx, sql); err == nil {
			t.Fatal("配置必须不可变")
		}
	}
	for _, change := range []func(*domain.ResearchBrief){func(b *domain.ResearchBrief) { *b.ShareWithModel = true }, func(b *domain.ResearchBrief) { b.BusinessRules += "变更" }} {
		changed := in
		changed.ResearchBrief = in.ResearchBrief.Clone()
		change(changed.ResearchBrief)
		if _, err = s.StartExploration(ctx, startKey, changed, true); !errors.Is(err, domain.ErrConflict) {
			t.Fatal("新内容同键须冲突", err)
		}
	}
	events, _ := s.ListEvents(ctx, first.Mission.ID, 0)
	missions, _ := s.ListMissions(ctx)
	receipt, _ := s.GetOperation(ctx, startKey)
	snap := mustSnapshot(t, ctx, s, first.Thread.ID)
	for _, v := range []any{events, missions, receipt, snap, first} {
		raw, _ := json.Marshal(v)
		if strings.Contains(string(raw), "BRIEF_SENTINEL") || strings.Contains(string(raw), "research_brief") {
			t.Fatal("普通读取泄漏正文")
		}
	}
	// 新任务显式分享；不继承另一个任务的配置。
	shared := startInput()
	shared.ResearchBrief = fixedBrief(true)
	other, err := s.StartExploration(ctx, materialID(72), shared, true)
	if err != nil {
		t.Fatal(err)
	}
	model, err = s.GetResearchContext(ctx, other.Mission.ID)
	if err != nil || !reflect.DeepEqual(model.ResearchBrief, shared.ResearchBrief) {
		t.Fatal("only-brief未装配", err)
	}
	plain, err := s.StartExploration(ctx, materialID(73), startInput(), true)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = s.GetResearchConfiguration(ctx, plain.Mission.ID)
	if err != nil || cfg.ResearchBrief != nil {
		t.Fatal("跨任务泄漏", err)
	}
	if _, err = s.GetResearchConfiguration(ctx, "missing"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}
func TestPostgresBriefAtomicRollbackAndIntegrity(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	// 仅此隔离schema注入写入失败，检查此前mission/选材/配置等全部回滚。
	_, err := s.pool.Exec(ctx, `CREATE FUNCTION reject_brief() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture'; END $$; CREATE TRIGGER reject_brief BEFORE INSERT ON mission_research_briefs FOR EACH ROW EXECUTE FUNCTION reject_brief()`)
	if err != nil {
		t.Fatal(err)
	}
	in := startInput()
	in.ResearchBrief = fixedBrief(false)
	if _, err = s.StartExploration(ctx, startKey, in, true); err == nil {
		t.Fatal("故障注入未触发")
	}
	for _, table := range []string{"missions", "threads", "mission_research_configs", "mission_research_briefs", "mission_materials", "mission_models", "operations", "events"} {
		var n int
		if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	if _, err = s.pool.Exec(ctx, `DROP TRIGGER reject_brief ON mission_research_briefs`); err != nil {
		t.Fatal(err)
	}
	first, err := s.StartExploration(ctx, startKey, in, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, `ALTER TABLE mission_research_briefs DISABLE TRIGGER mission_research_briefs_immutable; UPDATE mission_research_briefs SET content_sha256=repeat('0',64)`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetResearchConfiguration(ctx, first.Mission.ID); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("损坏原件不得忽略", err)
	}
	if _, err = s.GetResearchContext(ctx, first.Mission.ID); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal("分享关闭仍需检查完整性", err)
	}
	replay, err := s.StartExploration(ctx, startKey, in, false)
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatal("回执优先于当前能力或配置读取", err)
	}
	// hash匹配仍拒绝非规范、重复/未知键及缺省分享选择。
	for _, raw := range []string{`{"business_rules":"a","share_with_model":false,"share_with_model":true}`, `{"business_rules":"a","share_with_model":false,"token":"private"}`, `{"business_rules":"a"}`} {
		if _, err = s.pool.Exec(ctx, `UPDATE mission_research_briefs SET content=$1,content_sha256=$2`, raw, fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))); err != nil {
			t.Fatal(err)
		}
		if _, err = s.GetResearchContext(ctx, first.Mission.ID); !errors.Is(err, domain.ErrUnavailable) {
			t.Fatal("拒绝非规范原件", err)
		}
	}
}
func TestPostgresBriefUpgradeOldFingerprintAndRecovery(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	in := startInput()
	old, err := s.StartExploration(ctx, startKey, in, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(in)
	var oldHash string
	if err = s.pool.QueryRow(ctx, `SELECT request_hash FROM operations WHERE key=$1`, startKey).Scan(&oldHash); err != nil || oldHash != fmt.Sprintf("%x", sha256.Sum256(raw)) {
		t.Fatal("旧指纹改变", err)
	}
	dropBriefFixture(t, s, ctx)
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	replay, err := s.StartExploration(ctx, startKey, in, false)
	if err != nil || !reflect.DeepEqual(old, replay) {
		t.Fatal("旧回执兼容", err)
	}
	cfg, err := s.GetResearchConfiguration(ctx, old.Mission.ID)
	if err != nil || cfg.ResearchBrief != nil {
		t.Fatal("历史任务不得补造配置", err)
	}
	if _, err = s.SetThreadControl(ctx, old.Thread.ID, "paused", false); err != nil {
		t.Fatal(err)
	}
	in.ResearchBrief = fixedBrief(true)
	first, err := s.StartExploration(ctx, materialID(75), in, true)
	if err != nil {
		t.Fatal(err)
	}
	attempt := claimAttempt(t, ctx, s, first.Thread.ID, 100)
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err = s.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	snap := mustSnapshot(t, ctx, s, first.Thread.ID)
	if snap.Thread.Status != "blocked" || len(snap.Attempts) != 1 || snap.Attempts[0].ID != attempt.ID || snap.Attempts[0].Status != "interrupted" {
		t.Fatal("恢复不得重放", snap.Thread.Status)
	}
	if next, e := s.ClaimThread(ctx, 100); e != nil || next != nil {
		t.Fatal("恢复不得自动认领", e)
	}
	cfg, err = s.GetResearchConfiguration(ctx, first.Mission.ID)
	if err != nil || !reflect.DeepEqual(cfg.ResearchBrief, in.ResearchBrief) {
		t.Fatal("重开不得改固定配置", err)
	}
	var n int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM model_calls`).Scan(&n); err != nil || n != 0 {
		t.Fatal("恢复不得补造调用", err)
	}
}
