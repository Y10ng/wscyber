package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// 仅回退随机测试schema中的空D2结构；绝不是生产回退/迁移工具。
func dropVerificationsFixture(t *testing.T, s *Store, ctx context.Context) {
	t.Helper()
	var schema string
	var count int
	if e := s.pool.QueryRow(ctx, `SELECT current_schema(),(SELECT count(*) FROM finding_verifications)`).Scan(&schema, &count); e != nil || !strings.HasPrefix(schema, "store_test_") || count != 0 {
		t.Fatal("not an empty isolated verification fixture", schema, count, e)
	}
	_, e := s.pool.Exec(ctx, `DROP TABLE verification_ends,verification_results,finding_verifications;
 DROP FUNCTION wscyber_verification_owner(),wscyber_verification_end_guard(),wscyber_verification_result_guard();
 ALTER TABLE operations DROP CONSTRAINT operations_resource_type_check;
 ALTER TABLE operations ADD CONSTRAINT operations_resource_type_check CHECK(resource_type IN ('mission','thread','research_supplement'));
 ALTER TABLE model_calls DROP CONSTRAINT model_calls_purpose_check;
 ALTER TABLE model_calls ADD CONSTRAINT model_calls_purpose_check CHECK(purpose IN ('research','connectivity_test'));
 ALTER TABLE model_calls DROP CONSTRAINT model_calls_purpose_owner;
 ALTER TABLE model_calls ADD CHECK ((purpose='research' AND mission_id IS NOT NULL AND thread_id IS NOT NULL AND attempt_id IS NOT NULL AND test_id IS NULL) OR (purpose='connectivity_test' AND mission_id IS NULL AND thread_id IS NULL AND attempt_id IS NULL AND test_id IS NOT NULL AND NOT record_model_content));
 DELETE FROM wscyber_schema_migrations WHERE version=13`)
	if e != nil {
		t.Fatal(e)
	}
	// 从已冻结迁移取回原函数定义，不变更旧文件或伪造旧校验值。
	for _, item := range []struct{ source, name string }{{researchControlsSchema, "wscyber_operation_resource"}, {contextAssemblySchema, "wscyber_context_guard"}} {
		start := strings.Index(item.source, "CREATE FUNCTION "+item.name+"(")
		if start < 0 {
			t.Fatal("missing old fixture function")
		}
		text := item.source[start:]
		end := strings.Index(text, "$$;")
		if end < 0 {
			t.Fatal("missing function end")
		}
		text = strings.Replace(text[:end+3], "CREATE FUNCTION", "CREATE OR REPLACE FUNCTION", 1)
		if _, e = s.pool.Exec(ctx, text); e != nil {
			t.Fatal(e)
		}
	}
}
func TestD2UpgradeTwelvePreservesCandidateAndAudit(t *testing.T) {
	s, ctx, dsn := postgresStore(t)
	_, src := d2Source(t, s, ctx)
	before, _ := json.Marshal(src)
	audit, e := s.GetModelCallContext(ctx, src.CallID, src.MissionID)
	if e != nil {
		t.Fatal(e)
	}
	auditBefore, _ := json.Marshal(audit)
	dropVerificationsFixture(t, s, ctx)
	s.Close()
	next, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer next.Close()
	got, e := next.GetFinding(ctx, src.MissionID, src.FindingID, src.ID)
	after, _ := json.Marshal(got)
	if e != nil || string(before) != string(after) {
		t.Fatal("candidate drift", e)
	}
	audit, e = next.GetModelCallContext(ctx, src.CallID, src.MissionID)
	auditAfter, _ := json.Marshal(audit)
	if e != nil || string(auditBefore) != string(auditAfter) {
		t.Fatal("audit drift", e)
	}
	_ = d2Start(t, next, ctx, src, 3)
}
