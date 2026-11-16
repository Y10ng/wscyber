package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/modelledger"
)

func TestPostgresModelLedgerV5UpgradeNoBackfill(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	input := startInput()
	started, e := s.StartExplorationWithModel(ctx, startKey, input, domain.ExplorationModelOptions{EnvironmentConfigured: true, EnvironmentFingerprint: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	before, _ := json.Marshal(mustSnapshot(t, ctx, s, started.Thread.ID))
	dropLedgerFixture(t, s, ctx)
	s.Close()
	s, e = Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.Close)
	after, _ := json.Marshal(mustSnapshot(t, ctx, s, started.Thread.ID))
	if !bytes.Equal(before, after) {
		t.Fatal("006修改旧研究")
	}
	recording, e := s.GetModelRecording(ctx, started.Mission.ID)
	if e != nil || recording.RecordModelContent {
		t.Fatal(recording, e)
	}
	if replay, e := s.StartExplorationWithModel(ctx, startKey, input, domain.ExplorationModelOptions{}); e != nil || replay.Mission.ID != started.Mission.ID {
		t.Fatal(replay, e)
	}
	var n int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM model_calls`).Scan(&n); e != nil || n != 0 {
		t.Fatal("不可回填历史伪调用", n, e)
	}
}
func TestPostgresModelLedgerSkippedCalendarDate(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	f, e := modelledger.ParseFilter(url.Values{"from": {"2011-12-29"}, "to": {"2011-12-31"}, "time_zone": {"Pacific/Apia"}}, "", true, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.ModelUsage(ctx, f)
	if e != nil || len(r.Daily) != 2 || r.Daily[1].Date != "2011-12-30" || r.Daily[1].Calls != "0" {
		t.Fatal(r, e)
	}
	f, e = modelledger.ParseFilter(url.Values{"from": {"2011-12-30"}, "to": {"2011-12-31"}, "time_zone": {"Pacific/Apia"}}, "", true, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.ModelUsage(ctx, f)
	if e != nil || len(r.Daily) != 1 || r.Summary.Calls != "0" {
		t.Fatal(r, e)
	}
}
func TestPostgresModelPriceHistoricalIdentityAndCapacity(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	manager := modelManager(t, s)
	config := modelInput()
	if _, e := manager.Save(ctx, modelID, config); e != nil {
		t.Fatal(e)
	}
	config.ExpectedRevision = 1
	config.Model = "edited"
	if _, e := manager.Save(ctx, modelID, config); e != nil {
		t.Fatal(e)
	}
	ledger := modelledger.New(s, nil, manager)
	p := domain.ModelPriceWrite{ModelPriceKey: domain.ModelPriceKey{Source: "managed", ModelConfigID: modelID, Protocol: "openai_chat", Model: "not-in-history"}, PriceTerms: domain.PriceTerms{Currency: "CNY", InputPerMillion: "1", OutputPerMillion: "2"}, Enabled: true}
	if _, _, e := ledger.SavePrice(ctx, ledgerID(t), p); !errors.Is(e, domain.ErrInvalid) {
		t.Fatal("伪造model组合", e)
	}
	p.Model = "fixture-model"
	saved, created, e := ledger.SavePrice(ctx, ledgerID(t), p)
	if e != nil || !created {
		t.Fatal(saved, e)
	}
	terms, _ := json.Marshal(p.PriceTerms)
	if _, e = s.pool.Exec(ctx, `INSERT INTO model_prices(id,source,model_config_id,protocol,model,revision,enabled,terms) SELECT md5('capacity-fixture-'||n::text),'environment','','openai_chat','capacity-'||n::text,1,true,$1 FROM generate_series(1,499) n`, terms); e != nil {
		t.Fatal(e)
	}
	p.Model = "edited"
	if _, _, e = ledger.SavePrice(ctx, ledgerID(t), p); !errors.Is(e, domain.ErrConflict) {
		t.Fatal("容量应拒绝第501项", e)
	}
	p.Model = "fixture-model"
	p.ExpectedRevision = 1
	p.Enabled = false
	if _, _, e = ledger.SavePrice(ctx, saved.ID, p); e != nil {
		t.Fatal("满容量仍可编辑", e)
	}
	prices, e := s.ListModelPrices(ctx)
	if e != nil || len(prices) != 500 {
		t.Fatal(len(prices), e)
	}
}
func TestPostgresModelRecordingBindingFailureRollsBack(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION recording_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'recording fixture'; END $$; CREATE TRIGGER recording_fault BEFORE INSERT ON mission_models FOR EACH ROW EXECUTE FUNCTION recording_fault()`); e != nil {
		t.Fatal(e)
	}
	input := startInput()
	input.RecordModelContent = true
	_, e := s.StartExplorationWithModel(ctx, startKey, input, domain.ExplorationModelOptions{EnvironmentConfigured: true, EnvironmentFingerprint: "fixture", ContentRecordingAvailable: true})
	if e == nil {
		t.Fatal("绑定故障应失败")
	}
	for _, table := range []string{"missions", "threads", "mission_models", "operations", "events"} {
		var n int
		if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); e != nil || n != 0 {
			t.Fatal(table, n, e)
		}
	}
}
