-- 独立候选验证：同一研究执行底座，固定源修订，不更改发现状态。
CREATE TABLE finding_verifications (
 id text PRIMARY KEY,
 seq bigserial UNIQUE NOT NULL,
 mission_id text NOT NULL REFERENCES missions(id),
 finding_id text NOT NULL REFERENCES findings(id),
 source_revision_id text NOT NULL REFERENCES finding_revisions(id),
 thread_id text NOT NULL UNIQUE REFERENCES threads(id),
 max_steps integer NOT NULL CHECK(max_steps BETWEEN 1 AND 8),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX finding_verifications_page ON finding_verifications(mission_id,finding_id,seq DESC);
CREATE TRIGGER verification_immutable BEFORE UPDATE OR DELETE ON finding_verifications FOR EACH ROW EXECUTE FUNCTION reject_model_version_mutation();
CREATE TABLE verification_results (
 verification_id text PRIMARY KEY REFERENCES finding_verifications(id),
 attempt_id text NOT NULL UNIQUE REFERENCES attempts(id),
 call_id text NOT NULL UNIQUE REFERENCES model_calls(id),
 proposal_hash text NOT NULL,
 verdict text NOT NULL CHECK(verdict IN ('supported','counterevidence','inconclusive')),
 conclusion jsonb NOT NULL CHECK(octet_length(conclusion::text)<=32768),
 evidence jsonb NOT NULL CHECK(jsonb_typeof(evidence)='array'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER verification_result_immutable BEFORE UPDATE OR DELETE ON verification_results FOR EACH ROW EXECUTE FUNCTION reject_model_version_mutation();
CREATE TABLE verification_ends (
 verification_id text PRIMARY KEY REFERENCES finding_verifications(id),
 reason text NOT NULL CHECK(reason='budget_exhausted'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER verification_end_immutable BEFORE UPDATE OR DELETE ON verification_ends FOR EACH ROW EXECUTE FUNCTION reject_model_version_mutation();
CREATE FUNCTION wscyber_verification_owner() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM findings f JOIN finding_revisions r ON r.finding_id=f.id JOIN threads t ON t.mission_id=f.mission_id WHERE f.id=NEW.finding_id AND f.mission_id=NEW.mission_id AND r.id=NEW.source_revision_id AND t.id=NEW.thread_id) THEN RAISE EXCEPTION '验证归属无效' USING ERRCODE='23503'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER verification_owner BEFORE INSERT ON finding_verifications FOR EACH ROW EXECUTE FUNCTION wscyber_verification_owner();
CREATE FUNCTION wscyber_verification_end_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM finding_verifications v JOIN threads t ON t.id=v.thread_id WHERE v.id=NEW.verification_id AND t.step_count>=v.max_steps
 AND NOT EXISTS(SELECT 1 FROM verification_results WHERE verification_id=v.id)
 AND NOT EXISTS(SELECT 1 FROM attempts WHERE thread_id=t.id AND status='running')
 AND NOT EXISTS(SELECT 1 FROM actions WHERE thread_id=t.id AND (status IN ('pending','approved','running') OR (status='outcome_unknown' AND resolved_at IS NULL)))) THEN RAISE EXCEPTION '验证仍有预算或未决执行' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER verification_end_guard BEFORE INSERT ON verification_ends FOR EACH ROW EXECUTE FUNCTION wscyber_verification_end_guard();
CREATE FUNCTION wscyber_verification_result_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM finding_verifications v JOIN attempts a ON a.thread_id=v.thread_id JOIN model_calls c ON c.attempt_id=a.id WHERE v.id=NEW.verification_id AND a.id=NEW.attempt_id AND a.status='running' AND c.id=NEW.call_id AND c.thread_id=v.thread_id AND c.mission_id=v.mission_id AND c.purpose='verification' AND c.state='completed' AND c.outcome='valid_response' AND c.attempted=true AND NOT EXISTS(SELECT 1 FROM verification_ends WHERE verification_id=v.id)) THEN RAISE EXCEPTION '验证结论来源无效' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER verification_result_guard BEFORE INSERT ON verification_results FOR EACH ROW EXECUTE FUNCTION wscyber_verification_result_guard();
ALTER TABLE operations DROP CONSTRAINT operations_resource_type_check;
ALTER TABLE operations ADD CONSTRAINT operations_resource_type_check CHECK(resource_type IN ('mission','thread','research_supplement','verification'));
CREATE OR REPLACE FUNCTION wscyber_operation_resource() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.resource_type='mission' AND EXISTS(SELECT 1 FROM missions WHERE id=NEW.resource_id))
 OR (NEW.resource_type='thread' AND EXISTS(SELECT 1 FROM threads WHERE id=NEW.resource_id))
 OR (NEW.resource_type='research_supplement' AND EXISTS(SELECT 1 FROM research_supplements WHERE id=NEW.resource_id))
 OR (NEW.resource_type='verification' AND EXISTS(SELECT 1 FROM finding_verifications WHERE id=NEW.resource_id)) THEN RETURN NEW; END IF;
 RAISE EXCEPTION '提交回执资源不存在' USING ERRCODE='23503';
END;
$$;
CREATE OR REPLACE FUNCTION wscyber_context_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP <> 'INSERT' THEN RAISE EXCEPTION '调用上下文装配记录不可修改或删除' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM model_calls c WHERE c.id=NEW.call_id AND c.purpose IN ('research','verification')
 AND c.mission_id=NEW.assembly->>'mission_id' AND c.thread_id=NEW.assembly->>'thread_id'
 AND c.attempt_id=NEW.assembly->>'attempt_id') THEN
 RAISE EXCEPTION '上下文装配归属无效' USING ERRCODE='23503'; END IF;
 RETURN NEW;
END;
$$;
ALTER TABLE model_calls DROP CONSTRAINT model_calls_purpose_check;
ALTER TABLE model_calls ADD CONSTRAINT model_calls_purpose_check CHECK(purpose IN ('research','verification','connectivity_test'));
-- 006的未命名归属CHECK由表达式定位，避免依赖自动生成序号。
DO $$ DECLARE item record; BEGIN
 FOR item IN SELECT conname FROM pg_constraint WHERE conrelid='model_calls'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%purpose%research%mission_id%' LOOP
  EXECUTE format('ALTER TABLE model_calls DROP CONSTRAINT %I',item.conname);
 END LOOP;
END $$;
ALTER TABLE model_calls ADD CONSTRAINT model_calls_purpose_owner CHECK (
 (purpose IN ('research','verification') AND mission_id IS NOT NULL AND thread_id IS NOT NULL AND attempt_id IS NOT NULL AND test_id IS NULL)
 OR (purpose='connectivity_test' AND mission_id IS NULL AND thread_id IS NULL AND attempt_id IS NULL AND test_id IS NOT NULL AND NOT record_model_content)
);
