-- 显式Skill是文本数据；旧普通资料绝不从文件名自动转换。
ALTER TABLE library_items ADD COLUMN classification text NOT NULL DEFAULT 'material'
 CHECK (classification IN ('material','skill'));
ALTER TABLE library_items ADD CONSTRAINT library_skill_file CHECK (classification='material' OR kind='file');
CREATE INDEX library_live_skills ON library_items(id) WHERE deleted_at IS NULL AND classification='skill';

CREATE TABLE skill_activities (
 seq bigserial UNIQUE NOT NULL,
 attempt_id text PRIMARY KEY REFERENCES attempts(id),
 mission_id text NOT NULL REFERENCES missions(id),
 thread_id text NOT NULL REFERENCES threads(id),
 kind text NOT NULL CHECK (kind IN ('skill_discover','skill_load','skill_release')),
 request_hash text NOT NULL,
 request jsonb NOT NULL,
 result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX skill_activities_mission ON skill_activities(mission_id,seq);
CREATE INDEX skill_activities_thread ON skill_activities(thread_id,seq DESC);
CREATE TRIGGER skill_activities_immutable BEFORE UPDATE OR DELETE ON skill_activities
 FOR EACH ROW EXECUTE FUNCTION wscyber_guard_operation();

CREATE TABLE skill_loads (
 load_id text PRIMARY KEY REFERENCES skill_activities(attempt_id),
 mission_id text NOT NULL REFERENCES missions(id),
 thread_id text NOT NULL REFERENCES threads(id),
 attempt_id text NOT NULL UNIQUE REFERENCES attempts(id),
 purpose text NOT NULL CHECK (octet_length(purpose) BETWEEN 1 AND 1024),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 item_id text NOT NULL CHECK (item_id ~ '^[0-9a-f]{32}$'),
 source_token text NOT NULL CHECK (source_token ~ '^[0-9a-f]{32}$'),
 name text NOT NULL,
 path text NOT NULL,
 description text NOT NULL,
 format text NOT NULL CHECK (format IN ('markdown','text')),
 content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
 byte_size integer NOT NULL CHECK (byte_size BETWEEN 1 AND 32768 AND byte_size=octet_length(content)),
 content text NOT NULL
);
CREATE INDEX skill_loads_mission_item ON skill_loads(mission_id,item_id,created_at DESC);
CREATE TRIGGER skill_loads_immutable BEFORE UPDATE OR DELETE ON skill_loads
 FOR EACH ROW EXECUTE FUNCTION wscyber_guard_operation();

-- 唯一可变的活跃引用不充当历史或计数权威。
CREATE TABLE thread_skill_context (
 load_id text PRIMARY KEY REFERENCES skill_loads(load_id),
 thread_id text NOT NULL REFERENCES threads(id)
);
CREATE INDEX thread_skill_context_thread ON thread_skill_context(thread_id);
CREATE TABLE model_call_skills (
 call_id text NOT NULL REFERENCES model_calls(id),
 load_id text NOT NULL REFERENCES skill_loads(load_id),
 PRIMARY KEY(call_id,load_id)
);
CREATE INDEX model_call_skills_load ON model_call_skills(load_id,call_id);
CREATE TRIGGER model_call_skills_immutable BEFORE UPDATE OR DELETE ON model_call_skills
 FOR EACH ROW EXECUTE FUNCTION wscyber_guard_operation();
