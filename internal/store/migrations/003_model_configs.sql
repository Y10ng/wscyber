-- MC-1：配置内容整体加密，历史版本不可变；不改变已有研究/动作记录。
CREATE TABLE model_configs (
    id text PRIMARY KEY CHECK (id ~ '^[0-9a-f]{32}$'),
    current_revision bigint NOT NULL CHECK (current_revision > 0 AND current_revision < 9007199254740992),
    enabled boolean NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE model_config_versions (
    config_id text NOT NULL REFERENCES model_configs(id),
    revision bigint NOT NULL CHECK (revision > 0 AND revision < 9007199254740992),
    ciphertext bytea NOT NULL CHECK (octet_length(ciphertext) BETWEEN 30 AND 98333),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (config_id, revision)
);

ALTER TABLE model_configs ADD CONSTRAINT model_configs_current_version
    FOREIGN KEY (id, current_revision) REFERENCES model_config_versions(config_id, revision)
    DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION reject_model_version_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'model configuration versions are immutable' USING ERRCODE = '23514';
END
$$;
CREATE TRIGGER model_config_version_immutable
    BEFORE UPDATE OR DELETE ON model_config_versions
    FOR EACH ROW EXECUTE FUNCTION reject_model_version_mutation();

CREATE TABLE model_settings (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0 AND revision < 9007199254740992),
    default_config_id text REFERENCES model_configs(id)
);
INSERT INTO model_settings(singleton) VALUES (true);
