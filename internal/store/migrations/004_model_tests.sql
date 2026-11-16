-- 连通测试持久身份与有界队列；不保存提示词、供应商正文、秘密或假用量。
CREATE TABLE model_tests (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    config_id text NOT NULL,
    config_revision bigint NOT NULL CHECK (config_revision BETWEEN 1 AND 9007199254740991),
    purpose text NOT NULL DEFAULT 'connectivity_test' CHECK (purpose = 'connectivity_test'),
    request_version integer NOT NULL DEFAULT 1 CHECK (request_version = 1),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','completed')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    started_at timestamptz,
    completed_at timestamptz,
    outcome text CHECK (outcome IN ('not_sent','transport_unknown','http_error','invalid_response','valid_response')),
    http_status integer CHECK (http_status BETWEEN 100 AND 999),
    attempted boolean,
    FOREIGN KEY (config_id,config_revision) REFERENCES model_config_versions(config_id,revision),
    CHECK (
        (status = 'pending' AND started_at IS NULL AND completed_at IS NULL AND outcome IS NULL AND http_status IS NULL AND attempted IS NULL)
        OR (status = 'running' AND started_at IS NOT NULL AND completed_at IS NULL AND outcome IS NULL AND http_status IS NULL AND attempted IS NULL)
        OR (status = 'completed' AND completed_at IS NOT NULL AND outcome IS NOT NULL)
    )
);
CREATE INDEX model_tests_unfinished ON model_tests(created_at,id) WHERE status <> 'completed';

-- 身份不可改、终态不可覆盖；恢复只可将未完成记录转为保守终态。
CREATE FUNCTION protect_model_test() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'model test receipts cannot be deleted';
    END IF;
    IF OLD.status = 'completed'
       OR ROW(NEW.id,NEW.config_id,NEW.config_revision,NEW.purpose,NEW.request_version,NEW.created_at)
          IS DISTINCT FROM ROW(OLD.id,OLD.config_id,OLD.config_revision,OLD.purpose,OLD.request_version,OLD.created_at)
       OR NOT ((OLD.status = 'pending' AND NEW.status IN ('running','completed'))
               OR (OLD.status = 'running' AND NEW.status = 'completed'))
       OR (OLD.started_at IS NOT NULL AND NEW.started_at IS DISTINCT FROM OLD.started_at) THEN
        RAISE EXCEPTION 'invalid model test transition';
    END IF;
    RETURN NEW;
END $$;
CREATE TRIGGER model_test_transition BEFORE UPDATE OR DELETE ON model_tests
    FOR EACH ROW EXECUTE FUNCTION protect_model_test();
