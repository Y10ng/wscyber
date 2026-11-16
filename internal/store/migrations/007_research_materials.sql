-- C1单管理员资料库；文件是数据，不是宿主路径或可执行插件。
CREATE TABLE library_items (
 id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
 parent_id text REFERENCES library_items(id),
 kind text NOT NULL CHECK (kind IN ('folder','file')),
 name text NOT NULL CHECK (octet_length(name) BETWEEN 1 AND 256),
 description text NOT NULL DEFAULT '' CHECK (octet_length(description)<=1024),
 format text NOT NULL CHECK (format IN ('','markdown','text')),
 content text NOT NULL DEFAULT '' CHECK (octet_length(content)<=32768),
 content_sha256 text NOT NULL DEFAULT '',
 byte_size integer NOT NULL CHECK (byte_size BETWEEN 0 AND 32768),
 token text NOT NULL CHECK (token ~ '^[0-9a-f]{32}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 deleted_at timestamptz,
 CHECK (parent_id IS NULL OR parent_id<>id),
 CHECK (deleted_at IS NOT NULL OR (kind='folder' AND content='' AND format='' AND byte_size=0 AND content_sha256='') OR
  (kind='file' AND format<>'' AND byte_size=octet_length(content) AND byte_size>0 AND content_sha256 ~ '^[0-9a-f]{64}$'))
);
CREATE UNIQUE INDEX library_live_names ON library_items(COALESCE(parent_id,''),name) WHERE deleted_at IS NULL;
CREATE INDEX library_live_parent ON library_items(parent_id,id) WHERE deleted_at IS NULL;

-- 与历史mission-only operations分开，不改旧回执/FK；正文不进入回执。
CREATE TABLE library_operations (
 key text PRIMARY KEY CHECK (key ~ '^[A-Za-z0-9_-]{22,128}$'),
 method text NOT NULL,
 path text NOT NULL,
 request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
 response jsonb NOT NULL CHECK (jsonb_typeof(response)='object')
);
CREATE TRIGGER library_operations_immutable BEFORE UPDATE OR DELETE ON library_operations
 FOR EACH ROW EXECUTE FUNCTION wscyber_guard_operation();

CREATE TABLE mission_research_configs (
 mission_id text PRIMARY KEY REFERENCES missions(id),
 safety_prompt_enabled boolean NOT NULL DEFAULT false,
 safety_prompt text NOT NULL DEFAULT '' CHECK (octet_length(safety_prompt)<=8000),
 CHECK ((safety_prompt_enabled AND length(btrim(safety_prompt))>0) OR (NOT safety_prompt_enabled AND safety_prompt=''))
);
CREATE TRIGGER mission_research_configs_immutable BEFORE UPDATE OR DELETE ON mission_research_configs
 FOR EACH ROW EXECUTE FUNCTION wscyber_guard_operation();

-- 不FK关联实时资料：库删除/改名不改历史任务的来源和原件。
CREATE TABLE mission_materials (
 mission_id text NOT NULL REFERENCES missions(id),
 item_id text NOT NULL CHECK (item_id ~ '^[0-9a-f]{32}$'),
 source_token text NOT NULL CHECK (source_token ~ '^[0-9a-f]{32}$'),
 name text NOT NULL,
 path text NOT NULL,
 description text NOT NULL,
 format text NOT NULL CHECK (format IN ('markdown','text')),
 content text NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 32768),
 content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
 byte_size integer NOT NULL CHECK (byte_size=octet_length(content)),
 PRIMARY KEY (mission_id,item_id)
);
CREATE TRIGGER mission_materials_immutable BEFORE UPDATE OR DELETE ON mission_materials
 FOR EACH ROW EXECUTE FUNCTION wscyber_guard_operation();
