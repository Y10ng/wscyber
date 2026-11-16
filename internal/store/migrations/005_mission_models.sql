-- A组：任务选模与任务创建同事务绑定；历史任务不回填、不改变原环境兼容路径。
CREATE TABLE mission_models (
    mission_id text PRIMARY KEY REFERENCES missions(id),
    source text NOT NULL CHECK (source IN ('managed','environment')),
    config_id text,
    config_revision bigint,
    environment_fingerprint text,
    FOREIGN KEY (config_id,config_revision) REFERENCES model_config_versions(config_id,revision),
    CHECK ((source='managed' AND config_id IS NOT NULL AND config_revision IS NOT NULL AND environment_fingerprint IS NULL)
        OR (source='environment' AND config_id IS NULL AND config_revision IS NULL AND environment_fingerprint IS NOT NULL))
);
CREATE FUNCTION reject_mission_model_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'mission model bindings are immutable' USING ERRCODE = '23514';
END
$$;
CREATE TRIGGER mission_model_immutable BEFORE UPDATE OR DELETE ON mission_models
    FOR EACH ROW EXECUTE FUNCTION reject_mission_model_mutation();
