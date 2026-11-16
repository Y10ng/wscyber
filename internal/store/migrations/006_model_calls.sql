-- 实际模型调用账本：原文独立加密，旧调用不补造。
CREATE TABLE model_ledger_settings (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 coverage_started_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
INSERT INTO model_ledger_settings(singleton) VALUES (true);
ALTER TABLE missions ADD COLUMN record_model_content boolean NOT NULL DEFAULT false;

CREATE TABLE model_prices (
 id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
 source text NOT NULL CHECK (source IN ('managed','environment')),
 model_config_id text NOT NULL,
 protocol text NOT NULL CHECK (protocol IN ('openai_chat','anthropic_messages')),
 model text NOT NULL CHECK (length(model) BETWEEN 1 AND 200),
 revision bigint NOT NULL CHECK (revision > 0 AND revision < 9007199254740992),
 enabled boolean NOT NULL,
 terms jsonb NOT NULL CHECK (jsonb_typeof(terms) = 'object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK ((source = 'environment' AND model_config_id = '') OR (source = 'managed' AND model_config_id ~ '^[0-9a-f]{32}$')),
 UNIQUE(source,model_config_id,protocol,model)
);
CREATE TABLE model_calls (
 id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
 source text NOT NULL CHECK (source IN ('managed','environment')),
 model_config_id text REFERENCES model_configs(id),
 config_revision bigint,
 config_name text NOT NULL,
 protocol text NOT NULL CHECK (protocol IN ('openai_chat','anthropic_messages')),
 model text NOT NULL,
 purpose text NOT NULL CHECK (purpose IN ('research','connectivity_test')),
 mission_id text REFERENCES missions(id),
 thread_id text REFERENCES threads(id),
 attempt_id text REFERENCES attempts(id),
 test_id text REFERENCES model_tests(id),
 started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 finished_at timestamptz,
 duration_ms bigint CHECK (duration_ms >= 0),
 state text NOT NULL CHECK (state IN ('running','completed','interrupted')),
 outcome text NOT NULL CHECK (outcome IN ('pending','not_sent','transport_unknown','http_error','invalid_response','valid_response','interrupted')),
 attempted boolean,
 http_status integer CHECK (http_status BETWEEN 100 AND 599),
 record_model_content boolean NOT NULL,
 content_state text NOT NULL CHECK (content_state IN ('disabled','pending','available','partial','missing')),
 usage jsonb NOT NULL,
 cost jsonb NOT NULL,
 finish_hash text,
 CHECK ((source='environment' AND model_config_id IS NULL AND config_revision IS NULL) OR (source='managed' AND model_config_id IS NOT NULL AND config_revision IS NOT NULL)),
 FOREIGN KEY(model_config_id,config_revision) REFERENCES model_config_versions(config_id,revision),
 CHECK ((purpose='research' AND mission_id IS NOT NULL AND thread_id IS NOT NULL AND attempt_id IS NOT NULL AND test_id IS NULL) OR (purpose='connectivity_test' AND mission_id IS NULL AND thread_id IS NULL AND attempt_id IS NULL AND test_id IS NOT NULL AND NOT record_model_content)),
 CHECK ((state='running' AND outcome='pending' AND finished_at IS NULL) OR (state='interrupted' AND outcome='interrupted' AND finished_at IS NOT NULL) OR (state='completed' AND outcome NOT IN ('pending','interrupted') AND finished_at IS NOT NULL AND attempted IS NOT NULL))
);
CREATE UNIQUE INDEX model_calls_attempt ON model_calls(attempt_id) WHERE attempt_id IS NOT NULL;
CREATE UNIQUE INDEX model_calls_test ON model_calls(test_id) WHERE test_id IS NOT NULL;
CREATE INDEX model_calls_started ON model_calls(started_at DESC,id DESC);
CREATE INDEX model_calls_mission_started ON model_calls(mission_id,started_at DESC,id DESC);
CREATE INDEX model_calls_config_started ON model_calls(model_config_id,started_at DESC,id DESC);
CREATE INDEX model_calls_outcome_started ON model_calls(outcome,started_at DESC,id DESC);
CREATE INDEX model_calls_running ON model_calls(id) WHERE state='running';

CREATE TABLE model_call_contents (
 call_id text NOT NULL REFERENCES model_calls(id),
 kind text NOT NULL CHECK (kind IN ('input','response')),
 ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) BETWEEN 29 AND 2097181),
 media_type text NOT NULL CHECK (media_type IN ('application/json','application/octet-stream')),
 complete boolean NOT NULL,
 redacted boolean NOT NULL,
 PRIMARY KEY(call_id,kind)
);
CREATE FUNCTION guard_model_call_content() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM model_calls WHERE id=NEW.call_id AND record_model_content) THEN
  RAISE EXCEPTION 'model content recording disabled' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END
$$;
CREATE TRIGGER model_call_content_recording BEFORE INSERT ON model_call_contents FOR EACH ROW EXECUTE FUNCTION guard_model_call_content();
CREATE TRIGGER model_call_content_immutable BEFORE UPDATE OR DELETE ON model_call_contents FOR EACH ROW EXECUTE FUNCTION reject_model_version_mutation();
CREATE FUNCTION guard_model_call_terminal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state <> 'running' THEN
  RAISE EXCEPTION 'model call terminal immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END
$$;
CREATE TRIGGER model_call_terminal_immutable BEFORE UPDATE OR DELETE ON model_calls FOR EACH ROW EXECUTE FUNCTION guard_model_call_terminal();
