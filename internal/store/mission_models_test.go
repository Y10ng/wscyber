package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"wscyber/internal/domain"
	"wscyber/internal/model"
	"wscyber/internal/modelconfig"
)

func TestPostgresMissionModelBindingReplayAndRestart(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	m := modelManager(t, s)
	in := modelInput()
	if _, err := m.Save(ctx, modelID, in); err != nil {
		t.Fatal(err)
	}
	const other = "ffeeddccbbaa99887766554433221100"
	in.Model = "other-model"
	if _, err := m.Save(ctx, other, in); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetDefault(ctx, modelID, 0); err != nil {
		t.Fatal(err)
	}
	opts := domain.ExplorationModelOptions{EnvironmentConfigured: true, EnvironmentFingerprint: "fixture-env", Validate: m.ValidateRecordForUse}
	started, err := s.StartExplorationWithModel(ctx, startKey, startInput(), opts)
	if err != nil {
		t.Fatal(err)
	}
	explicit := startInput()
	explicit.ModelConfigID = other
	selected, err := s.StartExplorationWithModel(ctx, "explicit-model-fixture-key-0001", explicit, opts)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := s.GetMissionModelBinding(ctx, selected.Mission.ID); err != nil || b.ConfigID != other || b.ConfigRevision != 1 {
		t.Fatal(b, err)
	}
	if _, err := s.StartExplorationWithModel(ctx, startKey, explicit, opts); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("显式选择必须参与去重身份", err)
	}
	// 历史省略字段的指纹完全相同，而不是另做不可靠的先查再写。
	raw, _ := json.Marshal(struct {
		Description string   `json:"description"`
		Goal        string   `json:"goal"`
		Origins     []string `json:"allowed_origins"`
	}{startInput().Description, startInput().Goal, startInput().AllowedOrigins})
	var hash string
	if err := s.pool.QueryRow(ctx, `SELECT request_hash FROM operations WHERE key=$1`, startKey).Scan(&hash); err != nil || hash != fmt.Sprintf("%x", sha256.Sum256(raw)) {
		t.Fatal(hash, err)
	}
	if _, err := m.SetDefault(ctx, other, 1); err != nil {
		t.Fatal(err)
	}
	in = modelInput()
	in.ExpectedRevision = 1
	in.Model = "edited-model"
	if _, err := m.Save(ctx, modelID, in); err != nil {
		t.Fatal(err)
	}
	b, err := s.GetMissionModelBinding(ctx, started.Mission.ID)
	if err != nil || b.ConfigID != modelID || b.ConfigRevision != 1 {
		t.Fatal(b, err)
	}
	router := model.NewRouted(s, m, nil, "")
	v, err := router.MissionModel(ctx, started.Mission.ID)
	if err != nil || v.Model != "fixture-model" {
		t.Fatal(v, err)
	}
	in.ExpectedRevision = 2
	in.Enabled = false
	if _, err := m.Save(ctx, modelID, in); err != nil {
		t.Fatal(err)
	}
	v, err = router.MissionModel(ctx, started.Mission.ID)
	if err != nil || v.Enabled || v.Model != "fixture-model" {
		t.Fatal(v, err)
	}
	// 同键回放优先于当前默认/禁用/密钥和环境检查；原响应不改变。
	replay, err := s.StartExplorationWithModel(ctx, startKey, startInput(), domain.ExplorationModelOptions{})
	a, _ := json.Marshal(started)
	braw, _ := json.Marshal(replay)
	if err != nil || !bytes.Equal(a, braw) {
		t.Fatal(err)
	}
	explicit.ModelConfigID = modelID
	if _, err := s.StartExplorationWithModel(ctx, "disabled-model-fixture-0001", explicit, opts); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := m.SetDefault(ctx, "", 2); err != nil {
		t.Fatal(err)
	}
	environment, err := s.StartExplorationWithModel(ctx, "environment-model-fixture-0001", startInput(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := s.GetMissionModelBinding(ctx, environment.Mission.ID); err != nil || b.Source != "environment" || b.EnvironmentFingerprint != "fixture-env" {
		t.Fatal(b, err)
	}
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	router = model.NewRouted(s, modelManager(t, s), nil, "")
	v, err = router.MissionModel(ctx, started.Mission.ID)
	if err != nil || v.Model != "fixture-model" || v.Enabled {
		t.Fatal(v, err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE mission_models SET config_revision=3 WHERE mission_id=$1`, started.Mission.ID); err == nil {
		t.Fatal("绑定不得改写")
	}
}

func TestPostgresMissionModelAtomicFaultAndConcurrentEdit(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	m := modelManager(t, s)
	if _, err := m.Save(ctx, modelID, modelInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetDefault(ctx, modelID, 0); err != nil {
		t.Fatal(err)
	}
	opts := domain.ExplorationModelOptions{Validate: m.ValidateRecordForUse}
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION fail_mission_binding() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'binding fixture'; END $$; CREATE TRIGGER test_binding_fault BEFORE INSERT ON mission_models FOR EACH ROW EXECUTE FUNCTION fail_mission_binding()`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartExplorationWithModel(ctx, startKey, startInput(), opts); err == nil {
		t.Fatal("绑定故障应整体回滚")
	}
	for _, table := range []string{"missions", "threads", "events", "operations", "mission_models"} {
		var n int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	if _, err := s.pool.Exec(ctx, `DROP TRIGGER test_binding_fault ON mission_models; DROP FUNCTION fail_mission_binding()`); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	opts.Validate = func(r domain.SealedModelConfig) error {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return m.ValidateRecordForUse(r)
	}
	type response struct {
		start domain.ExplorationStart
		err   error
	}
	done := make(chan response, 1)
	go func() { v, e := s.StartExplorationWithModel(ctx, startKey, startInput(), opts); done <- response{v, e} }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("未进入事务内本地校验")
	}
	edited := make(chan error, 1)
	go func() {
		in := modelInput()
		in.ExpectedRevision = 1
		in.Enabled = false
		_, e := m.Save(ctx, modelID, in)
		edited <- e
	}()
	// 事务已持有设置/配置锁；另一个真实事务不能抢先禁用或改变被绑定版本。
	once.Do(func() { close(release) })
	var accepted domain.ExplorationStart
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
		accepted = r.start
	case <-time.After(5 * time.Second):
		t.Fatal("受理未完成")
	}
	if err := <-edited; err != nil {
		t.Fatal(err)
	}
	b, err := s.GetMissionModelBinding(ctx, accepted.Mission.ID)
	if err != nil || b.ConfigRevision != 1 {
		t.Fatal(b, err)
	}
	if _, err := m.ResolveVersion(ctx, b.ConfigID, b.ConfigRevision); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("禁用必须在下一次决策生效", err)
	}
	opts.Validate = m.ValidateRecordForUse
	opts.EnvironmentConfigured = true
	if _, err := s.StartExplorationWithModel(ctx, "after-disable-fixture-key-001", startInput(), opts); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("无效默认不得回环境", err)
	}
}

func TestPostgresMissionModelsV4UpgradeKeepsLegacy(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	m := modelManager(t, s)
	if _, err := m.Save(ctx, modelID, modelInput()); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.StartExploration(ctx, startKey, startInput(), true)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(mustSnapshot(t, ctx, s, legacy.Thread.ID))
	sealed, _ := s.GetModelConfig(ctx, modelID, 1)
	dropLedgerFixture(t, s, ctx)
	if _, err := s.pool.Exec(ctx, `DROP TABLE mission_models;DROP FUNCTION reject_mission_model_mutation();DELETE FROM wscyber_schema_migrations WHERE version=5`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	after, _ := json.Marshal(mustSnapshot(t, ctx, s, legacy.Thread.ID))
	current, _ := s.GetModelConfig(ctx, modelID, 1)
	if !bytes.Equal(before, after) || !bytes.Equal(sealed.Ciphertext, current.Ciphertext) {
		t.Fatal("v4迁移不得改变历史研究或密文")
	}
	if b, err := s.GetMissionModelBinding(ctx, legacy.Mission.ID); err != nil || b != nil {
		t.Fatal("历史任务不能回填当前默认", b, err)
	}
	if v, err := s.StartExplorationWithModel(ctx, startKey, startInput(), domain.ExplorationModelOptions{}); err != nil || v.Mission.ID != legacy.Mission.ID {
		t.Fatal(v, err)
	}
	if _, err := s.GetMissionModelBinding(ctx, "nonexistent"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
}

// 无密钥管理器也必须识别已有默认，不能把解密不可用当作无默认。
func TestPostgresMissionDefaultMissingKey(t *testing.T) {
	s, ctx, _ := postgresStore(t)
	m := modelManager(t, s)
	if _, err := m.Save(ctx, modelID, modelInput()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetDefault(ctx, modelID, 0); err != nil {
		t.Fatal(err)
	}
	missing := modelconfig.New(s, nil)
	id, name, usable, err := missing.Default(context.Background())
	if err != nil || id != modelID || name != "" || usable {
		t.Fatal(id, name, usable, err)
	}
	if _, err := s.StartExplorationWithModel(ctx, startKey, startInput(), domain.ExplorationModelOptions{EnvironmentConfigured: true, Validate: missing.ValidateRecordForUse}); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatal(err)
	}
}
