package store

import (
	"context"
	"testing"
)

// 只供postgresStore创建的随机隔离schema；还原真实v9约束，不触碰旧运行实例。
func dropControlsFixture(t *testing.T, s *Store, ctx context.Context) {
	t.Helper()
	dropContextFixture(t, s, ctx)
	_, err := s.pool.Exec(ctx, `DROP TRIGGER operations_resource ON operations; DROP FUNCTION wscyber_operation_resource();
 DROP TABLE model_call_supplements; DROP TABLE research_supplements; DROP FUNCTION wscyber_supplement_guard();
 ALTER TABLE operations DROP CONSTRAINT operations_resource_type_check;
 ALTER TABLE operations ADD CONSTRAINT operations_resource_type_check CHECK(resource_type='mission');
 ALTER TABLE operations ADD CONSTRAINT operations_resource_id_fkey FOREIGN KEY(resource_id) REFERENCES missions(id);
 DELETE FROM wscyber_schema_migrations WHERE version=10`)
	if err != nil {
		t.Fatal(err)
	}
}
func TestC2BUpgradeV9(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	old, err := s.StartExploration(ctx, startKey, startInput(), true)
	if err != nil {
		t.Fatal(err)
	}
	dropControlsFixture(t, s, ctx)
	s.Close()
	next, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(next.Close)
	receipt, err := next.GetOperation(ctx, startKey)
	if err != nil || receipt.ResourceID != old.Mission.ID {
		t.Fatal(receipt, err)
	}
	replay, err := next.StartExploration(ctx, startKey, startInput(), false)
	if err != nil || replay.Thread.ID != old.Thread.ID {
		t.Fatal(replay, err)
	}
	if _, err = next.ControlResearch(ctx, controlKey(901), old.Mission.ID, old.Thread.ID, "pause", nil); err != nil {
		t.Fatal(err)
	}
}
