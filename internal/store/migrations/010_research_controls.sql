-- 人工文本原件与实际模型输入关联；不改历史观察、绑定或预算。
CREATE TABLE research_supplements (
 id text PRIMARY KEY,
 mission_id text NOT NULL REFERENCES missions(id),
 thread_id text NOT NULL REFERENCES threads(id),
 source text NOT NULL CHECK(octet_length(source) BETWEEN 1 AND 256),
 content text NOT NULL CHECK(octet_length(content) BETWEEN 1 AND 16384),
 content_sha256 text NOT NULL CHECK(content_sha256 ~ '^[0-9a-f]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX research_supplements_thread ON research_supplements(thread_id,created_at,id);
CREATE TABLE model_call_supplements (
 call_id text NOT NULL REFERENCES model_calls(id),
 supplement_id text NOT NULL REFERENCES research_supplements(id),
 PRIMARY KEY(call_id,supplement_id)
);
CREATE INDEX model_call_supplements_source ON model_call_supplements(supplement_id,call_id);

-- 扩展同一全局提交键域，旧mission回执原样保留；动态归属约束替代单一mission外键。
ALTER TABLE operations DROP CONSTRAINT operations_resource_type_check;
ALTER TABLE operations DROP CONSTRAINT operations_resource_id_fkey;
ALTER TABLE operations ADD CONSTRAINT operations_resource_type_check CHECK(resource_type IN ('mission','thread','research_supplement'));
CREATE FUNCTION wscyber_operation_resource() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.resource_type='mission' AND EXISTS(SELECT 1 FROM missions WHERE id=NEW.resource_id))
 OR (NEW.resource_type='thread' AND EXISTS(SELECT 1 FROM threads WHERE id=NEW.resource_id))
 OR (NEW.resource_type='research_supplement' AND EXISTS(SELECT 1 FROM research_supplements WHERE id=NEW.resource_id)) THEN RETURN NEW; END IF;
 RAISE EXCEPTION '提交回执资源不存在' USING ERRCODE='23503';
END;
$$;
CREATE TRIGGER operations_resource BEFORE INSERT ON operations FOR EACH ROW EXECUTE FUNCTION wscyber_operation_resource();

CREATE FUNCTION wscyber_supplement_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP <> 'INSERT' THEN RAISE EXCEPTION '人工补充原件不可修改或删除' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM threads WHERE id=NEW.thread_id AND mission_id=NEW.mission_id) THEN
 RAISE EXCEPTION '人工补充归属无效' USING ERRCODE='23503'; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER research_supplements_guard BEFORE INSERT OR UPDATE OR DELETE ON research_supplements FOR EACH ROW EXECUTE FUNCTION wscyber_supplement_guard();
