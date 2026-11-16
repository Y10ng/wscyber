package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"wscyber/internal/domain"
)

// 仅测试随机schema；退去本片新增空结构用于真实旧版本升级回归。
func dropFindingsFixture(t *testing.T, s *Store, ctx context.Context) {
	t.Helper()
	dropVerificationsFixture(t, s, ctx)
	var schema string
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT current_schema(),(SELECT count(*) FROM findings)`).Scan(&schema, &count); err != nil || !strings.HasPrefix(schema, "store_test_") || count != 0 {
		t.Fatal("not an empty isolated finding fixture", schema, count, err)
	}
	_, err := s.pool.Exec(ctx, `DROP TRIGGER events_finding_guard ON events;
 ALTER TABLE events DROP COLUMN finding_id, DROP COLUMN finding_revision_id;
 DROP TABLE findings,finding_revisions;
 DROP FUNCTION wscyber_finding_guard(),wscyber_finding_revision_guard(),wscyber_finding_event_guard();
 DELETE FROM wscyber_schema_migrations WHERE version=12`)
	if err != nil {
		t.Fatal(err)
	}
}
func TestD1UpgradeElevenPreservesOriginalAndOldAudit(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	th := fixtureThread(t, ctx, s)
	if _, e := s.AddObservation(ctx, domain.Observation{ThreadID: th.ID, Kind: "operator_note", Content: "OLD_ORIGINAL"}); e != nil {
		t.Fatal(e)
	}
	a := claimAttempt(t, ctx, s, th.ID, 8)
	snap, e := s.AssembleResearchSnapshot(ctx, th.MissionID, th.ID, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	snap.Assembly.Policy = domain.ContextPolicy
	snap.Assembly.Windows = snap.Assembly.Windows[:3]
	c, e := s.BeginModelCall(ctx, contextCall(t, th, a, snap))
	if e != nil {
		t.Fatal(e)
	}
	original := mustSnapshot(t, ctx, s, th.ID)
	before, _ := json.Marshal(original)
	oldAudit, e := s.GetModelCallContext(ctx, c.ID, th.MissionID)
	if e != nil {
		t.Fatal(e)
	}
	auditBefore, _ := json.Marshal(oldAudit)
	dropFindingsFixture(t, s, ctx)
	s.Close()
	next, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	after, _ := json.Marshal(mustSnapshot(t, ctx, next, th.ID))
	if string(before) != string(after) {
		t.Fatal("upgrade altered original")
	}
	got, e := next.GetModelCallContext(ctx, c.ID, th.MissionID)
	auditAfter, _ := json.Marshal(got)
	if e != nil || string(auditBefore) != string(auditAfter) {
		t.Fatal("old audit rewritten", e)
	}
	page, e := next.ListFindings(ctx, th.MissionID, domain.FindingQuery{})
	if e != nil || len(page.Items) != 0 {
		t.Fatal("backfilled fake findings", e)
	}
}
