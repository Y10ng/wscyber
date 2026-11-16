-- 非秘密用户声明，创建事务固定；无记录表示未配置，不补造历史分享意图。
CREATE TABLE mission_research_briefs (
 mission_id text PRIMARY KEY REFERENCES missions(id),
 content text NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 32768 AND jsonb_typeof(content::jsonb)='object'),
 content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$')
);
CREATE TRIGGER mission_research_briefs_immutable BEFORE UPDATE OR DELETE ON mission_research_briefs
 FOR EACH ROW EXECUTE FUNCTION wscyber_guard_operation();
