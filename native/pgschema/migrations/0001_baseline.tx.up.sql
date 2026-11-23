-- 基线 v1（P1）：全部表、索引、tenant_id 与归属触发器、默认租户；RLS 策略已建但未启用（P5 启用）。
-- 文件名 .tx.up.sql：bun/migrate 在一个事务内执行整个文件；运行器在同一事务内登记迁移版本。
-- 首次生产部署前可原地修改（开发库删库重建）；首发后只增不改（0002_…）。
-- 由 var/p1b-pg-infra/gen/genbaseline.py 生成后人工审阅；列与 webapi.SchemaModule()/上游 coreSchema 一致性由 pgschema 测试兜底。
-- 注意：bun 会格式化 `?`，本文件不得使用 jsonb `?` 运算符。

-- ===== 身份函数（§4.3/§4.4） =====
-- app.tenant_id 在会话里被 set_config(..., true) 用过一次后，事务结束会回到空串而非“未定义”，
-- 所以一律用 missing_ok 读取并把空串视为未设置。
CREATE FUNCTION wscyber_current_tenant() RETURNS text
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT NULLIF(current_setting('app.tenant_id', true), '') $$;

CREATE FUNCTION wscyber_require_tenant() RETURNS text
    LANGUAGE plpgsql STABLE
    AS $$
DECLARE
    t text := wscyber_current_tenant();
BEGIN
    IF t IS NULL THEN
        RAISE EXCEPTION 'wscyber: app.tenant_id 未设置，无法确定新行所属租户'
            USING ERRCODE = 'WT001';
    END IF;
    RETURN t;
END
$$;

CREATE FUNCTION wscyber_can_see(row_tenant text) RETURNS boolean
    LANGUAGE sql STABLE PARALLEL SAFE
    AS $$ SELECT COALESCE(current_setting('app.role', true), '') IN ('admin', 'system')
              OR row_tenant = wscyber_current_tenant() $$;

-- 子表 BEFORE INSERT：参数 (父表, 父键列, 子表外键列)。
-- 外键列非空 → 必须找到父行，tenant_id 取父行值；显式给出的 tenant_id 或会话 app.tenant_id 与父行不一致即拒绝。
-- 外键列为空 → 回退到 app.tenant_id（未设置即报错）。
CREATE FUNCTION wscyber_inherit_tenant() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    parent_table text := TG_ARGV[0];
    parent_column text := TG_ARGV[1];
    child_column text := TG_ARGV[2];
    key text;
    parent_tenant text;
    session_tenant text := wscyber_current_tenant();
BEGIN
    -- 以参数传入整行再取列，避免 to_jsonb(NEW) 把大 bytea 列整体编码。
    EXECUTE format('SELECT ($1).%I::text', child_column) INTO key USING NEW;
    IF key IS NULL OR key = '' THEN
        IF NEW.tenant_id IS NULL THEN
            NEW.tenant_id := wscyber_require_tenant();
        ELSIF session_tenant IS NOT NULL AND NEW.tenant_id <> session_tenant THEN
            RAISE EXCEPTION 'wscyber: %.tenant_id 与会话租户不一致', TG_TABLE_NAME USING ERRCODE = 'WT002';
        END IF;
        RETURN NEW;
    END IF;
    EXECUTE format('SELECT tenant_id FROM %I WHERE %I = $1', parent_table, parent_column)
        INTO parent_tenant USING key;
    IF parent_tenant IS NULL THEN
        RAISE EXCEPTION 'wscyber: %.% = % 没有对应的 % 父行，无法确定租户', TG_TABLE_NAME, child_column, key, parent_table
            USING ERRCODE = 'WT003';
    END IF;
    IF NEW.tenant_id IS NOT NULL AND NEW.tenant_id <> parent_tenant THEN
        RAISE EXCEPTION 'wscyber: %.tenant_id 与父行 % 不一致', TG_TABLE_NAME, parent_table USING ERRCODE = 'WT002';
    END IF;
    IF session_tenant IS NOT NULL AND session_tenant <> parent_tenant THEN
        RAISE EXCEPTION 'wscyber: 会话租户与父行 % 的租户不一致', parent_table USING ERRCODE = 'WT002';
    END IF;
    NEW.tenant_id := parent_tenant;
    RETURN NEW;
END
$$;

CREATE FUNCTION wscyber_tenant_immutable() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
        RAISE EXCEPTION 'wscyber: 不允许修改 %.tenant_id', TG_TABLE_NAME USING ERRCODE = 'WT004';
    END IF;
    RETURN NEW;
END
$$;

-- ===== 租户与用户（§4.1） =====
CREATE TABLE tenants (
    id text PRIMARY KEY,
    name text NOT NULL UNIQUE,
    created_at text NOT NULL,
    archived boolean NOT NULL DEFAULT false
);

CREATE TABLE users (
    id text PRIMARY KEY,
    tenant_id text REFERENCES tenants (id),
    username text NOT NULL UNIQUE,
    display_name text NOT NULL,
    role text NOT NULL CHECK (role IN ('admin', 'user')),
    password_hash text NOT NULL,
    disabled boolean NOT NULL DEFAULT false,
    created_at text NOT NULL,
    updated_at text NOT NULL,
    CHECK (role = 'admin' OR tenant_id IS NOT NULL)
);
CREATE INDEX users_tenant ON users (tenant_id);

CREATE TABLE user_sessions (
    token_hash bytea PRIMARY KEY,
    user_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at text NOT NULL,
    expires_at text NOT NULL,
    last_seen_at text NOT NULL
);
CREATE INDEX user_sessions_user ON user_sessions (user_id);

CREATE TABLE webapi_counters (
    tenant_id text NOT NULL REFERENCES tenants (id),
    name text NOT NULL,
    value bigint NOT NULL,
    PRIMARY KEY (tenant_id, name)
);

INSERT INTO tenants (id, name, created_at)
VALUES ('default', '默认租户', to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'));

-- ===== 上游原生四表（coreSchema） =====
CREATE TABLE chat_sessions (
    id text NOT NULL,
    node_id text NOT NULL,
    status text NOT NULL,
    archived boolean NOT NULL,
    title text NOT NULL,
    agent_name text NOT NULL,
    session_json text NOT NULL,
    created_at text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL DEFAULT wscyber_require_tenant() REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE chat_aop_events (
    id text NOT NULL,
    session_id text NOT NULL,
    event_id text NOT NULL,
    cursor bigint NOT NULL,
    turn_id text NOT NULL,
    emitter text NOT NULL,
    sequence bigint NOT NULL,
    event_proto bytea NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id),
    CONSTRAINT chat_aop_events_aop_event_cursor UNIQUE (session_id, cursor),
    CONSTRAINT chat_aop_events_session_fk FOREIGN KEY (session_id) REFERENCES chat_sessions (id) ON DELETE CASCADE
);
CREATE TABLE aop_request_ledger (
    request_id text NOT NULL,
    method text NOT NULL,
    request_hash bytea NOT NULL,
    response_json text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL DEFAULT wscyber_require_tenant() REFERENCES tenants (id),
    PRIMARY KEY (request_id)
);
CREATE TABLE raw_artifacts (
    cursor bigint GENERATED BY DEFAULT AS IDENTITY NOT NULL,
    event_id text NOT NULL,
    event_proto bytea NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL DEFAULT wscyber_require_tenant() REFERENCES tenants (id),
    PRIMARY KEY (cursor),
    CONSTRAINT raw_artifacts_event_id_key UNIQUE (event_id)
);
-- ===== webapi 44 表（webapi.SchemaModule().Tables；列与模型一一对应） =====
CREATE TABLE webapi_asset_credentials (
    id text NOT NULL,
    asset_id text NOT NULL,
    label text NOT NULL,
    username text NOT NULL,
    secret_cipher text NOT NULL,
    note text NOT NULL,
    created_at text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_asset_explore (
    asset_id text NOT NULL,
    max_rounds bigint NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (asset_id)
);
CREATE TABLE webapi_asset_scenarios (
    asset_id text NOT NULL,
    scenario text NOT NULL,
    done_criteria text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (asset_id)
);
CREATE TABLE webapi_asset_schedules (
    asset_id text NOT NULL,
    cron text NOT NULL,
    enabled boolean NOT NULL,
    next_run_at text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (asset_id)
);
CREATE TABLE webapi_asset_tasks (
    task_id text NOT NULL,
    asset_id text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (task_id)
);
CREATE TABLE webapi_asset_vuln_members (
    finding_id text NOT NULL,
    vuln_id text NOT NULL,
    asset_id text NOT NULL,
    task_id text NOT NULL,
    link text NOT NULL,
    created_at text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (finding_id)
);
CREATE TABLE webapi_asset_vulns (
    id text NOT NULL,
    asset_id text NOT NULL,
    endpoint_key text NOT NULL,
    title_override text NOT NULL,
    created_at text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_assets (
    id text NOT NULL,
    name text NOT NULL,
    owner text NOT NULL,
    environment text NOT NULL,
    known_info text NOT NULL,
    objective text NOT NULL,
    origins_json text NOT NULL,
    forbidden text NOT NULL,
    scope text NOT NULL,
    approval text NOT NULL,
    archived boolean NOT NULL,
    created_at text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL DEFAULT wscyber_require_tenant() REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_audit_events (
    id text NOT NULL,
    at text NOT NULL,
    actor text NOT NULL,
    action text NOT NULL,
    target_kind text NOT NULL,
    target_id text NOT NULL,
    task_id text NOT NULL,
    asset_id text NOT NULL,
    summary text NOT NULL,
    detail_json text NOT NULL,
    tenant_id text DEFAULT wscyber_current_tenant() REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_authz_compares (
    id text NOT NULL,
    task_id text NOT NULL,
    session_id text NOT NULL,
    original_evidence_id text NOT NULL,
    label text NOT NULL,
    variant_evidence_id text NOT NULL,
    status text NOT NULL,
    hint text NOT NULL,
    http_status bigint,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_board_endpoints (
    id text NOT NULL,
    task_id text NOT NULL,
    endpoint_key text NOT NULL,
    method text NOT NULL,
    origin text NOT NULL,
    path text NOT NULL,
    params_json text NOT NULL,
    status_codes_json text NOT NULL,
    auth_seen text NOT NULL,
    request_count bigint NOT NULL,
    first_seen_at text NOT NULL,
    last_seen_at text NOT NULL,
    source text NOT NULL,
    note text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_board_facts (
    id text NOT NULL,
    task_id text NOT NULL,
    kind text NOT NULL,
    subject text NOT NULL,
    statement text NOT NULL,
    confidence text NOT NULL,
    evidence_ids_json text NOT NULL,
    status text NOT NULL,
    session_id text NOT NULL,
    turn_id text NOT NULL,
    call_id text NOT NULL,
    created_at text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_board_ingest (
    task_id text NOT NULL,
    last_created_at text NOT NULL,
    last_id text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (task_id)
);
CREATE TABLE webapi_board_oracle_events (
    id text NOT NULL,
    oracle_id text NOT NULL,
    task_id text NOT NULL,
    from_status text NOT NULL,
    to_status text NOT NULL,
    actor text NOT NULL,
    session_id text NOT NULL,
    at text NOT NULL,
    summary text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_board_oracles (
    id text NOT NULL,
    task_id text NOT NULL,
    number bigint NOT NULL,
    question text NOT NULL,
    category text NOT NULL,
    endpoint_keys_json text NOT NULL,
    priority text NOT NULL,
    status text NOT NULL,
    origin text NOT NULL,
    claimed_session_id text NOT NULL,
    result_summary text NOT NULL,
    evidence_ids_json text NOT NULL,
    finding_id text NOT NULL,
    dedupe_key text NOT NULL,
    created_at text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_egress_flows (
    id text NOT NULL,
    task_id text NOT NULL,
    session_id text NOT NULL,
    turn_id text NOT NULL,
    call_id text NOT NULL,
    tool text NOT NULL,
    method text NOT NULL,
    url text NOT NULL,
    host text NOT NULL,
    decision text NOT NULL,
    sent boolean,
    http_status bigint,
    error text NOT NULL,
    flow_pb bytea NOT NULL,
    request_bytes bigint NOT NULL,
    response_bytes bigint NOT NULL,
    preview_truncated boolean NOT NULL,
    created_at text NOT NULL,
    completed_at text,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_finding_revisions (
    id text NOT NULL,
    finding_id text NOT NULL,
    task_id text NOT NULL,
    number bigint NOT NULL,
    revision bigint NOT NULL,
    session_id text NOT NULL,
    turn_id text NOT NULL,
    call_id text NOT NULL,
    payload_json text NOT NULL,
    title text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_findings (
    id text NOT NULL,
    task_id text NOT NULL,
    number bigint NOT NULL,
    status text NOT NULL,
    current_revision bigint NOT NULL,
    current_revision_id text NOT NULL,
    created_at text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_notification_ops (
    notification_id text NOT NULL,
    title text NOT NULL,
    asset_id text NOT NULL,
    session_id text NOT NULL,
    payload_json text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (notification_id)
);
CREATE TABLE webapi_notifications (
    id text NOT NULL,
    dedupe_key text NOT NULL,
    kind text NOT NULL,
    task_id text NOT NULL,
    finding_id text NOT NULL,
    verification_id text NOT NULL,
    vuln_id text NOT NULL,
    status text NOT NULL,
    attempts bigint NOT NULL,
    next_attempt_at text,
    last_error text NOT NULL,
    created_at text NOT NULL,
    sent_at text,
    tenant_id text NOT NULL DEFAULT wscyber_require_tenant() REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_notify_ops_settings (
    id text NOT NULL,
    notify_approval boolean NOT NULL,
    notify_schedule_failed boolean NOT NULL,
    notify_weekly_digest boolean NOT NULL,
    weekly_digest_since text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL DEFAULT wscyber_require_tenant() REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_notify_settings (
    id text NOT NULL,
    webhook_cipher text NOT NULL,
    enabled boolean NOT NULL,
    require_confirm boolean NOT NULL,
    base_url text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL DEFAULT wscyber_require_tenant() REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_replays (
    id text NOT NULL,
    vuln_key text NOT NULL,
    step bigint NOT NULL,
    task_id text NOT NULL,
    evidence_id text NOT NULL,
    original_evidence_id text NOT NULL,
    method text NOT NULL,
    url text NOT NULL,
    status_code bigint NOT NULL,
    verdict_hint text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_report_drafts (
    report_id text NOT NULL,
    session_id text NOT NULL,
    turn_id text NOT NULL,
    call_id text NOT NULL,
    payload_json text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (report_id)
);
CREATE TABLE webapi_reports (
    id text NOT NULL,
    task_id text NOT NULL,
    session_id text NOT NULL,
    items_json text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_requests (
    id text NOT NULL,
    task_id text NOT NULL,
    session_id text NOT NULL,
    turn_id text NOT NULL,
    call_id text NOT NULL,
    fingerprint text NOT NULL,
    method text NOT NULL,
    url text NOT NULL,
    status text NOT NULL,
    reason text NOT NULL,
    sent boolean,
    status_code bigint,
    preview bytea NOT NULL,
    preview_truncated boolean NOT NULL,
    capture_truncated boolean NOT NULL,
    flow_pb bytea NOT NULL,
    created_at text NOT NULL,
    completed_at text,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_retest_results (
    retest_id text NOT NULL,
    session_id text NOT NULL,
    turn_id text NOT NULL,
    call_id text NOT NULL,
    payload_json text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (retest_id)
);
CREATE TABLE webapi_retests (
    id text NOT NULL,
    task_id text NOT NULL,
    finding_id text NOT NULL,
    source_revision_id text NOT NULL,
    session_id text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_schedule_followups (
    id text NOT NULL,
    run_id text NOT NULL,
    kind text NOT NULL,
    task_id text NOT NULL,
    finding_id text NOT NULL,
    revision_id text NOT NULL,
    target_id text NOT NULL,
    status text NOT NULL,
    error text NOT NULL,
    created_at text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_schedule_options (
    asset_id text NOT NULL,
    auto_verify boolean NOT NULL,
    auto_retest boolean NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (asset_id)
);
CREATE TABLE webapi_schedule_run_followups (
    run_id text NOT NULL,
    asset_id text NOT NULL,
    task_id text NOT NULL,
    auto_verify boolean NOT NULL,
    auto_retest boolean NOT NULL,
    planned_at text NOT NULL,
    completed_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (run_id)
);
CREATE TABLE webapi_schedule_runs (
    id text NOT NULL,
    asset_id text NOT NULL,
    scheduled_for text NOT NULL,
    task_id text NOT NULL,
    status text NOT NULL,
    error text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_subagent_reservations (
    parent_session_id text NOT NULL,
    parent_call_id text NOT NULL,
    task_id text NOT NULL,
    oracle_id text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (parent_session_id, parent_call_id)
);
CREATE TABLE webapi_subsession_usage (
    id text NOT NULL,
    session_id text NOT NULL,
    task_id text NOT NULL,
    model text NOT NULL,
    input_tokens bigint NOT NULL,
    output_tokens bigint NOT NULL,
    total_tokens bigint NOT NULL,
    cache_read_tokens bigint NOT NULL,
    cache_write_tokens bigint NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_task_explore (
    task_id text NOT NULL,
    max_rounds bigint NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (task_id)
);
CREATE TABLE webapi_task_policies (
    task_id text NOT NULL,
    scope text NOT NULL,
    approval text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (task_id)
);
CREATE TABLE webapi_task_policy_events (
    id text NOT NULL,
    task_id text NOT NULL,
    scope text NOT NULL,
    approval text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_task_scenarios (
    task_id text NOT NULL,
    scenario text NOT NULL,
    done_criteria text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (task_id)
);
CREATE TABLE webapi_task_subsessions (
    session_id text NOT NULL,
    task_id text NOT NULL,
    parent_session_id text NOT NULL,
    parent_call_id text NOT NULL,
    oracle_id text NOT NULL,
    agent_name text NOT NULL,
    status text NOT NULL,
    stop_reason text NOT NULL,
    error text NOT NULL,
    created_at text NOT NULL,
    ended_at text,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (session_id)
);
CREATE TABLE webapi_tasks (
    id text NOT NULL,
    known_info text NOT NULL,
    objective text NOT NULL,
    origins_json text NOT NULL,
    initial_session_id text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL DEFAULT wscyber_require_tenant() REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_verification_results (
    verification_id text NOT NULL,
    session_id text NOT NULL,
    turn_id text NOT NULL,
    call_id text NOT NULL,
    payload_json text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (verification_id)
);
CREATE TABLE webapi_verifications (
    id text NOT NULL,
    task_id text NOT NULL,
    finding_id text NOT NULL,
    source_revision_id text NOT NULL,
    session_id text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_vuln_sla (
    id bigint NOT NULL,
    critical_days bigint NOT NULL,
    high_days bigint NOT NULL,
    medium_days bigint NOT NULL,
    low_days bigint NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL DEFAULT wscyber_require_tenant() REFERENCES tenants (id),
    PRIMARY KEY (id)
);
CREATE TABLE webapi_vuln_triage (
    vuln_key text NOT NULL,
    asset_id text NOT NULL,
    severity text NOT NULL,
    disposition text NOT NULL,
    owner text NOT NULL,
    due_date text NOT NULL,
    note text NOT NULL,
    created_at text NOT NULL,
    updated_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (vuln_key)
);
-- ===== 索引（上游 6 + webapi 39 + 新增 2；名称沿用模型声明） =====
CREATE INDEX idx_sessions_updated ON chat_sessions (tenant_id, updated_at DESC);
CREATE INDEX idx_sessions_node_id ON chat_sessions (node_id);
CREATE INDEX idx_aop_events_session ON chat_aop_events (session_id, cursor);
CREATE INDEX idx_aop_events_turn ON chat_aop_events (turn_id);
CREATE UNIQUE INDEX idx_aop_events_event_id ON chat_aop_events (session_id, event_id);
CREATE INDEX idx_raw_artifacts_created ON raw_artifacts (created_at);
CREATE UNIQUE INDEX webapi_requests_call ON webapi_requests (task_id, turn_id, call_id);
CREATE INDEX webapi_requests_task_created ON webapi_requests (task_id, created_at DESC);
CREATE INDEX webapi_requests_unknown ON webapi_requests (task_id, fingerprint, status);
CREATE UNIQUE INDEX webapi_findings_number ON webapi_findings (tenant_id, number);
CREATE UNIQUE INDEX webapi_finding_revisions_call ON webapi_finding_revisions (task_id, session_id, turn_id, call_id);
CREATE UNIQUE INDEX webapi_finding_revisions_revision ON webapi_finding_revisions (finding_id, revision);
CREATE UNIQUE INDEX webapi_verifications_session ON webapi_verifications (session_id);
CREATE INDEX webapi_verifications_finding_created ON webapi_verifications (finding_id, created_at DESC, id DESC);
CREATE UNIQUE INDEX webapi_reports_session ON webapi_reports (session_id);
CREATE INDEX webapi_reports_task_created ON webapi_reports (task_id, created_at DESC, id DESC);
CREATE UNIQUE INDEX webapi_retests_session ON webapi_retests (session_id);
CREATE INDEX webapi_retests_finding_created ON webapi_retests (finding_id, created_at DESC, id DESC);
CREATE INDEX webapi_task_policy_events_task_created ON webapi_task_policy_events (task_id, created_at DESC, id DESC);
CREATE INDEX webapi_asset_credentials_asset ON webapi_asset_credentials (asset_id, created_at);
CREATE INDEX webapi_asset_tasks_asset_created ON webapi_asset_tasks (asset_id, created_at DESC);
CREATE UNIQUE INDEX webapi_schedule_runs_asset_scheduled ON webapi_schedule_runs (asset_id, scheduled_for);
CREATE UNIQUE INDEX webapi_schedule_followups_item ON webapi_schedule_followups (run_id, kind, finding_id);
CREATE INDEX webapi_asset_vulns_asset ON webapi_asset_vulns (asset_id);
CREATE INDEX webapi_asset_vuln_members_vuln ON webapi_asset_vuln_members (vuln_id);
CREATE INDEX webapi_asset_vuln_members_asset ON webapi_asset_vuln_members (asset_id);
CREATE INDEX webapi_egress_flows_task_created ON webapi_egress_flows (task_id, created_at);
CREATE INDEX webapi_egress_flows_session ON webapi_egress_flows (session_id);
CREATE UNIQUE INDEX webapi_notifications_dedupe ON webapi_notifications (dedupe_key, kind, verification_id);
CREATE INDEX webapi_notifications_due ON webapi_notifications (status, next_attempt_at);
CREATE INDEX webapi_notifications_created ON webapi_notifications (tenant_id, created_at DESC, id DESC);
CREATE INDEX webapi_audit_events_at ON webapi_audit_events (tenant_id, at, id);
CREATE INDEX webapi_audit_events_action ON webapi_audit_events (action);
CREATE INDEX webapi_audit_events_task ON webapi_audit_events (task_id);
CREATE INDEX webapi_audit_events_asset ON webapi_audit_events (asset_id);
CREATE UNIQUE INDEX webapi_board_endpoints_task_key ON webapi_board_endpoints (task_id, endpoint_key);
CREATE INDEX webapi_board_facts_task_created ON webapi_board_facts (task_id, created_at DESC, id DESC);
CREATE UNIQUE INDEX webapi_board_oracles_number ON webapi_board_oracles (task_id, number);
CREATE UNIQUE INDEX webapi_board_oracles_dedupe ON webapi_board_oracles (task_id, dedupe_key);
CREATE INDEX webapi_board_oracle_events_oracle ON webapi_board_oracle_events (oracle_id, at);
CREATE INDEX webapi_replays_vuln_created ON webapi_replays (vuln_key, created_at DESC, id DESC);
CREATE INDEX webapi_task_subsessions_task_created ON webapi_task_subsessions (task_id, created_at DESC);
CREATE INDEX webapi_subsession_usage_task_created ON webapi_subsession_usage (task_id, created_at);
CREATE INDEX webapi_authz_compares_task_created ON webapi_authz_compares (task_id, created_at DESC);
CREATE INDEX webapi_authz_compares_original ON webapi_authz_compares (original_evidence_id);
CREATE INDEX webapi_tasks_tenant_created ON webapi_tasks (tenant_id, created_at DESC);
CREATE INDEX webapi_assets_tenant_updated ON webapi_assets (tenant_id, updated_at DESC);

-- ===== tenant_id 归属触发器（§4.2） =====
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON chat_aop_events FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('chat_sessions', 'id', 'session_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_asset_credentials FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_assets', 'id', 'asset_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_asset_explore FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_assets', 'id', 'asset_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_asset_scenarios FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_assets', 'id', 'asset_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_asset_schedules FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_assets', 'id', 'asset_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_asset_tasks FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_asset_vuln_members FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_assets', 'id', 'asset_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_asset_vulns FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_assets', 'id', 'asset_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_authz_compares FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_board_endpoints FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_board_facts FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_board_ingest FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_board_oracle_events FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_board_oracles FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_egress_flows FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_finding_revisions FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_findings FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_notification_ops FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_notifications', 'id', 'notification_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_replays FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_report_drafts FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_reports', 'id', 'report_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_reports FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_requests FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_retest_results FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_retests', 'id', 'retest_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_retests FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_schedule_followups FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_schedule_runs', 'id', 'run_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_schedule_options FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_assets', 'id', 'asset_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_schedule_run_followups FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_assets', 'id', 'asset_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_schedule_runs FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_assets', 'id', 'asset_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_subagent_reservations FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_subsession_usage FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_task_explore FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_task_policies FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_task_policy_events FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_task_scenarios FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_task_subsessions FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_verification_results FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_verifications', 'id', 'verification_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_verifications FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_vuln_triage FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_assets', 'id', 'asset_id');
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON chat_sessions FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON chat_aop_events FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON aop_request_ledger FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON raw_artifacts FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_asset_credentials FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_asset_explore FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_asset_scenarios FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_asset_schedules FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_asset_tasks FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_asset_vuln_members FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_asset_vulns FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_assets FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_audit_events FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_authz_compares FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_board_endpoints FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_board_facts FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_board_ingest FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_board_oracle_events FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_board_oracles FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_egress_flows FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_finding_revisions FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_findings FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_notification_ops FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_notifications FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_notify_ops_settings FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_notify_settings FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_replays FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_report_drafts FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_reports FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_requests FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_retest_results FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_retests FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_schedule_followups FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_schedule_options FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_schedule_run_followups FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_schedule_runs FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_subagent_reservations FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_subsession_usage FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_task_explore FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_task_policies FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_task_policy_events FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_task_scenarios FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_task_subsessions FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_tasks FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_verification_results FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_verifications FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_vuln_sla FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_vuln_triage FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_counters FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();

-- ===== RLS 策略（§4.3；P5 才 ENABLE/FORCE，此处只建策略） =====
CREATE POLICY tenant_rw ON chat_sessions USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON chat_aop_events USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON aop_request_ledger USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON raw_artifacts USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_asset_credentials USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_asset_explore USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_asset_scenarios USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_asset_schedules USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_asset_tasks USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_asset_vuln_members USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_asset_vulns USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_assets USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_audit_events USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_authz_compares USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_board_endpoints USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_board_facts USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_board_ingest USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_board_oracle_events USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_board_oracles USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_egress_flows USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_finding_revisions USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_findings USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_notification_ops USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_notifications USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_notify_ops_settings USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_notify_settings USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_replays USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_report_drafts USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_reports USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_requests USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_retest_results USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_retests USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_schedule_followups USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_schedule_options USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_schedule_run_followups USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_schedule_runs USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_subagent_reservations USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_subsession_usage USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_task_explore USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_task_policies USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_task_policy_events USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_task_scenarios USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_task_subsessions USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_tasks USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_verification_results USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_verifications USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_vuln_sla USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_vuln_triage USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_counters USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));

CREATE POLICY tenant_self ON tenants
    USING (COALESCE(current_setting('app.role', true), '') IN ('admin', 'system') OR id = wscyber_current_tenant())
    WITH CHECK (COALESCE(current_setting('app.role', true), '') IN ('admin', 'system'));
CREATE POLICY user_self ON users
    USING (COALESCE(current_setting('app.role', true), '') IN ('admin', 'system') OR id = NULLIF(current_setting('app.user_id', true), ''))
    WITH CHECK (COALESCE(current_setting('app.role', true), '') IN ('admin', 'system'));
CREATE POLICY system_only ON user_sessions
    USING (COALESCE(current_setting('app.role', true), '') = 'system')
    WITH CHECK (COALESCE(current_setting('app.role', true), '') = 'system');
