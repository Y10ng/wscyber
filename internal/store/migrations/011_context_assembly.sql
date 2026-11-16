-- 仅模型路径使用的必需候选索引；保留历史原件和原查询契约。
CREATE INDEX observations_context_required ON observations(thread_id,created_at,id)
 WHERE kind IN ('counterevidence','contradiction','refutation');
CREATE INDEX actions_context_required ON actions(thread_id,created_at,id)
 WHERE status IN ('pending','approved','running','outcome_unknown');
CREATE TABLE model_call_contexts (
 call_id text PRIMARY KEY REFERENCES model_calls(id),
 assembly jsonb NOT NULL CHECK(jsonb_typeof(assembly)='object' AND octet_length(assembly::text)<=98304)
);
CREATE FUNCTION wscyber_context_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP <> 'INSERT' THEN RAISE EXCEPTION '调用上下文装配记录不可修改或删除' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM model_calls c WHERE c.id=NEW.call_id AND c.purpose='research'
  AND c.mission_id=NEW.assembly->>'mission_id' AND c.thread_id=NEW.assembly->>'thread_id'
  AND c.attempt_id=NEW.assembly->>'attempt_id') THEN
  RAISE EXCEPTION '上下文装配归属无效' USING ERRCODE='23503'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER model_call_contexts_guard BEFORE INSERT OR UPDATE OR DELETE ON model_call_contexts
 FOR EACH ROW EXECUTE FUNCTION wscyber_context_guard();
