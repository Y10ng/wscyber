-- 首轮只为开始探索保存提交回执；不改变旧 POST、旧动作或审批状态。
CREATE TABLE operations (
    key text PRIMARY KEY CHECK (key ~ '^[A-Za-z0-9_-]{22,128}$'),
    method text NOT NULL,
    path text NOT NULL,
    request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    resource_type text NOT NULL CHECK (resource_type = 'mission'),
    resource_id text NOT NULL REFERENCES missions(id),
    response_status integer NOT NULL CHECK (response_status BETWEEN 200 AND 299),
    response jsonb NOT NULL CHECK (jsonb_typeof(response) = 'object'),
    committed_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

-- 成功回执只追加、不修改、不自动回收；防止旧键再次成为新的启动请求。
CREATE FUNCTION wscyber_guard_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '已提交回执不可修改或删除' USING ERRCODE = '23514';
END;
$$;
CREATE TRIGGER operations_immutable BEFORE UPDATE OR DELETE ON operations
FOR EACH ROW EXECUTE FUNCTION wscyber_guard_operation();
