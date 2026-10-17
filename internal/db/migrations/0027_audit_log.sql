-- Append-only record of administrative and moderation actions (doc/moderation.md).
-- Rows are inserted once and never changed; only the optional retention job
-- (AUDIT_RETENTION_DAYS, default 0 = keep forever) deletes old ones. There is
-- deliberately no foreign key to users: an entry must outlive the account.
CREATE TABLE audit_log (
    id          BIGSERIAL PRIMARY KEY,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    actor_id    UUID,
    actor_name  TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL CHECK (char_length(action) BETWEEN 1 AND 64),
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    target_name TEXT NOT NULL DEFAULT '',
    details     JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_id  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_created ON audit_log (created_at DESC, id DESC);
CREATE INDEX audit_log_actor ON audit_log (actor_id, id DESC);
CREATE INDEX audit_log_action ON audit_log (action, id DESC);
CREATE INDEX audit_log_target ON audit_log (target_type, target_id, id DESC);

CREATE FUNCTION audit_log_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_log entries are immutable';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_log_no_update BEFORE UPDATE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_immutable();
