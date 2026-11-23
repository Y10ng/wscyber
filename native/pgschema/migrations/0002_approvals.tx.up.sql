-- 0002（R2）：审批记录与运营通知待登记持久化。首发后只增不改：本迁移只新建表、索引、触发器与策略，不改 0001 的任何对象。
-- 契约见 native/POLICY.md「审批记录」与 native/NOTIFY.md「运营通知」。

-- 审批记录：危险操作挂起时落一行（pending），批准/拒绝/超时/取消时更新为终态。内存运行时仍负责真正挂起/放行调用，本表是权威记录。
-- 时间列为定宽 UTC 毫秒文本（同 webapi_notifications，字符串序即时间序）。
CREATE TABLE webapi_approvals (
    id text NOT NULL,
    operation_id text NOT NULL,
    task_id text NOT NULL,
    session_id text NOT NULL,
    parent_session_id text NOT NULL,
    turn_id text NOT NULL,
    call_id text NOT NULL,
    tool text NOT NULL,
    summary text NOT NULL,
    reason text NOT NULL,
    status text NOT NULL,
    decided_by text NOT NULL,
    status_reason text NOT NULL,
    hub_instance text NOT NULL,
    created_at text NOT NULL,
    expires_at text NOT NULL,
    decided_at text,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (id),
    CHECK (status IN ('pending', 'approved', 'rejected', 'expired', 'cancelled')),
    CHECK (decided_by IN ('', 'user', 'system')),
    CHECK ((status = 'pending') = (decided_at IS NULL))
);
CREATE INDEX webapi_approvals_task_created ON webapi_approvals (task_id, created_at DESC, id DESC);
CREATE INDEX webapi_approvals_session ON webapi_approvals (session_id);
CREATE INDEX webapi_approvals_parent_session ON webapi_approvals (parent_session_id);
CREATE INDEX webapi_approvals_operation ON webapi_approvals (operation_id);
CREATE INDEX webapi_approvals_status_created ON webapi_approvals (tenant_id, status, created_at DESC, id DESC);

-- 运营通知待登记：取代进程内重试队列。一行＝一项尚未落到 webapi_notifications 的登记（register_pending），
-- 投递 goroutine 按 next_attempt_at 取到期行登记，成功或放弃即删除；失败 attempts+1 并推迟。
CREATE TABLE webapi_notify_registrations (
    key text NOT NULL,
    kind text NOT NULL,
    approval_id text NOT NULL,
    args_json text NOT NULL,
    attempts bigint NOT NULL,
    next_attempt_at text NOT NULL,
    last_error text NOT NULL,
    created_at text NOT NULL,
    tenant_id text NOT NULL REFERENCES tenants (id),
    PRIMARY KEY (key)
);
CREATE INDEX webapi_notify_registrations_due ON webapi_notify_registrations (next_attempt_at, key);
CREATE INDEX webapi_notify_registrations_approval ON webapi_notify_registrations (approval_id);

-- tenant_id 归属（同基线 §4.2）：审批随任务（无任务的调用 task_id 为空，回退到 app.tenant_id）；待登记随审批（非审批类 approval_id 为空，回退到 app.tenant_id）。
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_approvals FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_tasks', 'id', 'task_id');
CREATE TRIGGER wscyber_tenant_inherit BEFORE INSERT ON webapi_notify_registrations FOR EACH ROW EXECUTE FUNCTION wscyber_inherit_tenant('webapi_approvals', 'id', 'approval_id');
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_approvals FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();
CREATE TRIGGER wscyber_tenant_immutable BEFORE UPDATE OF tenant_id ON webapi_notify_registrations FOR EACH ROW EXECUTE FUNCTION wscyber_tenant_immutable();

-- RLS 策略（P5 才 ENABLE/FORCE，同基线）。
CREATE POLICY tenant_rw ON webapi_approvals USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
CREATE POLICY tenant_rw ON webapi_notify_registrations USING (wscyber_can_see(tenant_id)) WITH CHECK (wscyber_can_see(tenant_id));
