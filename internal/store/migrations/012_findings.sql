-- D首片：稳定身份与不可变候选历史；不改旧原件/001–011。
CREATE TABLE findings (
 id text PRIMARY KEY,
 number bigint GENERATED ALWAYS AS IDENTITY UNIQUE CHECK(number>0),
 mission_id text NOT NULL REFERENCES missions(id),
 thread_id text NOT NULL REFERENCES threads(id),
 current_revision integer NOT NULL CHECK(current_revision>0),
 current_revision_id text NOT NULL,
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE TABLE finding_revisions (
 id text PRIMARY KEY,
 finding_id text NOT NULL REFERENCES findings(id),
 revision integer NOT NULL CHECK(revision>0),
 attempt_id text NOT NULL UNIQUE REFERENCES attempts(id),
 call_id text NOT NULL UNIQUE REFERENCES model_calls(id),
 proposal_hash text NOT NULL CHECK(proposal_hash ~ '^[a-f0-9]{64}$'),
 content_sha256 text NOT NULL CHECK(content_sha256 ~ '^[a-f0-9]{64}$'),
 content jsonb NOT NULL CHECK(jsonb_typeof(content)='object' AND octet_length(content::text)<=49152),
 evidence jsonb NOT NULL CHECK(jsonb_typeof(evidence)='array' AND jsonb_array_length(evidence) BETWEEN 1 AND 16 AND octet_length(evidence::text)<=32768),
 created_at timestamptz NOT NULL,
 UNIQUE(finding_id,revision), UNIQUE(finding_id,revision,id)
);
ALTER TABLE findings ADD CONSTRAINT findings_current_revision_fk
 FOREIGN KEY(id,current_revision,current_revision_id) REFERENCES finding_revisions(finding_id,revision,id) DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX findings_mission_number ON findings(mission_id,number DESC);
CREATE INDEX findings_thread_number ON findings(mission_id,thread_id,number DESC);
CREATE INDEX findings_context_recent ON findings(thread_id,updated_at DESC,current_revision_id DESC);
ALTER TABLE events ADD COLUMN finding_id text REFERENCES findings(id), ADD COLUMN finding_revision_id text REFERENCES finding_revisions(id),
 ADD CONSTRAINT events_finding_pair CHECK((finding_id IS NULL)=(finding_revision_id IS NULL));
CREATE UNIQUE INDEX events_finding_revision ON events(finding_revision_id) WHERE finding_revision_id IS NOT NULL;
CREATE FUNCTION wscyber_finding_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION '发现身份不可删除或回收' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' AND (NEW.id,NEW.number,NEW.mission_id,NEW.thread_id,NEW.created_at) IS DISTINCT FROM (OLD.id,OLD.number,OLD.mission_id,OLD.thread_id,OLD.created_at) THEN
  RAISE EXCEPTION '发现身份不可改变' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' AND (NEW.current_revision<>OLD.current_revision+1 OR NEW.current_revision_id=OLD.current_revision_id) THEN
  RAISE EXCEPTION '发现更新必须追加修订' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM threads WHERE id=NEW.thread_id AND mission_id=NEW.mission_id) THEN
  RAISE EXCEPTION '发现方向归属不符' USING ERRCODE='23503'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER findings_guard BEFORE INSERT OR UPDATE OR DELETE ON findings FOR EACH ROW EXECUTE FUNCTION wscyber_finding_guard();
CREATE FUNCTION wscyber_finding_revision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION '发现历史不可修改或删除' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM findings f JOIN attempts a ON a.id=NEW.attempt_id AND a.thread_id=f.thread_id
 JOIN model_calls c ON c.id=NEW.call_id AND c.attempt_id=a.id AND c.thread_id=f.thread_id AND c.mission_id=f.mission_id
 WHERE f.id=NEW.finding_id AND a.status='running' AND c.purpose='research' AND c.state='completed' AND c.outcome='valid_response' AND c.attempted=true) THEN
  RAISE EXCEPTION '候选来源尝试或调用无效' USING ERRCODE='23503'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER finding_revisions_guard BEFORE INSERT OR UPDATE OR DELETE ON finding_revisions FOR EACH ROW EXECUTE FUNCTION wscyber_finding_revision_guard();
CREATE FUNCTION wscyber_finding_event_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.finding_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM findings f JOIN finding_revisions r ON r.finding_id=f.id
 WHERE f.id=NEW.finding_id AND r.id=NEW.finding_revision_id AND f.mission_id=NEW.mission_id AND f.thread_id=NEW.thread_id
 AND NEW.kind IN ('finding.created','finding.updated')) THEN
 RAISE EXCEPTION '发现事件归属无效' USING ERRCODE='23503'; END IF;
 RETURN NEW;
END; $$;
CREATE TRIGGER events_finding_guard BEFORE INSERT OR UPDATE ON events FOR EACH ROW EXECUTE FUNCTION wscyber_finding_event_guard();
