CREATE TABLE IF NOT EXISTS missions (
    id text PRIMARY KEY,
    title text NOT NULL,
    origin text NOT NULL,
    goal text NOT NULL,
    allowed_origins jsonb NOT NULL CHECK (jsonb_typeof(allowed_origins) = 'array'),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE IF NOT EXISTS threads (
    id text PRIMARY KEY,
    mission_id text NOT NULL REFERENCES missions(id),
    title text NOT NULL,
    question text NOT NULL,
    rationale text NOT NULL,
    next_step text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'ready'
        CHECK (status IN ('ready', 'running', 'waiting_approval', 'blocked', 'paused', 'concluded')),
    priority integer NOT NULL DEFAULT 0,
    auto_run boolean NOT NULL DEFAULT false,
    pause_requested boolean NOT NULL DEFAULT false,
    step_count integer NOT NULL DEFAULT 0 CHECK (step_count >= 0),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (id, mission_id)
);
CREATE INDEX IF NOT EXISTS threads_mission_order ON threads(mission_id, created_at, id);
CREATE INDEX IF NOT EXISTS threads_dispatch_order ON threads(priority DESC, created_at, id)
    WHERE status = 'ready' AND auto_run;

CREATE TABLE IF NOT EXISTS attempts (
    id text PRIMARY KEY,
    thread_id text NOT NULL REFERENCES threads(id),
    status text NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'interrupted', 'waiting_approval')),
    summary text NOT NULL DEFAULT '',
    error text NOT NULL DEFAULT '',
    started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    ended_at timestamptz,
    UNIQUE (id, thread_id),
    CHECK ((status = 'running') = (ended_at IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS attempts_one_running ON attempts(thread_id) WHERE status = 'running';
CREATE INDEX IF NOT EXISTS attempts_thread_order ON attempts(thread_id, started_at, id);

CREATE TABLE IF NOT EXISTS actions (
    id text PRIMARY KEY,
    thread_id text NOT NULL REFERENCES threads(id),
    attempt_id text,
    input jsonb NOT NULL CHECK (jsonb_typeof(input) = 'object'),
    request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    status text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'running', 'succeeded', 'failed', 'rejected', 'outcome_unknown')),
    result jsonb,
    error text NOT NULL DEFAULT '',
    decision_reason text NOT NULL DEFAULT '',
    resolved_at timestamptz,
    resolution_note text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (id, thread_id),
    FOREIGN KEY (attempt_id, thread_id) REFERENCES attempts(id, thread_id),
    CHECK ((resolved_at IS NULL AND resolution_note = '') OR
           (status = 'outcome_unknown' AND resolved_at IS NOT NULL AND btrim(resolution_note) <> ''))
);
CREATE UNIQUE INDEX IF NOT EXISTS actions_one_unsettled ON actions(thread_id)
    WHERE status IN ('pending', 'approved', 'running') OR (status = 'outcome_unknown' AND resolved_at IS NULL);
CREATE INDEX IF NOT EXISTS actions_thread_order ON actions(thread_id, created_at, id);
CREATE INDEX IF NOT EXISTS actions_approved_order ON actions(created_at, id) WHERE status = 'approved';

-- 审批绑定原请求；即使绕过 Repository，更新也不能替换已批准参数或未知效果历史。
CREATE OR REPLACE FUNCTION wscyber_guard_action() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id OR NEW.thread_id IS DISTINCT FROM OLD.thread_id OR
       NEW.attempt_id IS DISTINCT FROM OLD.attempt_id OR NEW.input IS DISTINCT FROM OLD.input OR
       NEW.request_hash IS DISTINCT FROM OLD.request_hash OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION '动作请求创建后不可修改' USING ERRCODE = '23514';
    END IF;
    IF NEW.status <> OLD.status AND NOT (
        (OLD.status = 'pending' AND NEW.status IN ('approved', 'rejected')) OR
        (OLD.status = 'approved' AND NEW.status = 'running') OR
        (OLD.status = 'running' AND NEW.status IN ('succeeded', 'failed', 'outcome_unknown'))
    ) THEN
        RAISE EXCEPTION '动作状态迁移无效' USING ERRCODE = '23514';
    END IF;
    IF OLD.status <> 'pending' AND NEW.decision_reason IS DISTINCT FROM OLD.decision_reason THEN
        RAISE EXCEPTION '已生效的审批不可覆盖' USING ERRCODE = '23514';
    END IF;
    IF OLD.status IN ('succeeded', 'failed', 'rejected', 'outcome_unknown') AND
       (NEW.result IS DISTINCT FROM OLD.result OR NEW.error IS DISTINCT FROM OLD.error) THEN
        RAISE EXCEPTION '动作终态证据不可覆盖' USING ERRCODE = '23514';
    END IF;
    IF OLD.resolved_at IS NOT NULL AND (NEW.resolved_at IS DISTINCT FROM OLD.resolved_at OR
                                       NEW.resolution_note IS DISTINCT FROM OLD.resolution_note) THEN
        RAISE EXCEPTION '人工核对记录不可覆盖' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS actions_immutable_request ON actions;
CREATE TRIGGER actions_immutable_request BEFORE UPDATE ON actions
    FOR EACH ROW EXECUTE FUNCTION wscyber_guard_action();

CREATE TABLE IF NOT EXISTS observations (
    id text PRIMARY KEY,
    thread_id text NOT NULL REFERENCES threads(id),
    attempt_id text,
    action_id text,
    kind text NOT NULL CHECK (btrim(kind) <> ''),
    content text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (attempt_id, thread_id) REFERENCES attempts(id, thread_id),
    FOREIGN KEY (action_id, thread_id) REFERENCES actions(id, thread_id)
);
CREATE INDEX IF NOT EXISTS observations_thread_order ON observations(thread_id, created_at, id);

CREATE TABLE IF NOT EXISTS events (
    id bigserial PRIMARY KEY,
    mission_id text NOT NULL REFERENCES missions(id),
    thread_id text,
    kind text NOT NULL,
    message text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (thread_id, mission_id) REFERENCES threads(id, mission_id)
);
CREATE INDEX IF NOT EXISTS events_mission_order ON events(mission_id, id);
