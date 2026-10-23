-- Account deletion (GDPR Art. 17) and data export (Art. 20).
--
-- A deleted account stays as an anonymised tombstone row: its messages keep a
-- valid author (so threads stay readable) but lose their content, and every
-- other table loses what referred to the person. deleted_at marks the
-- tombstone; the account is also disabled, so it can never sign in again.
ALTER TABLE users ADD COLUMN deleted_at TIMESTAMPTZ;

CREATE INDEX users_deleted ON users (deleted_at) WHERE deleted_at IS NOT NULL;

-- One export per request: built in the background into object storage under
-- exports/, downloadable once within 24 hours, then removed. requested_at
-- also serves the limit of one request per day.
CREATE TABLE data_exports (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'failed')),
    requested_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ready_at      TIMESTAMPTZ,
    expires_at    TIMESTAMPTZ,
    size_bytes    BIGINT NOT NULL DEFAULT 0,
    s3_key        TEXT,
    -- Set while a download is being sent, so two requests cannot both take the
    -- file; cleared when the copy fails, kept (with downloaded_at) when it ends.
    claimed_at    TIMESTAMPTZ,
    downloaded_at TIMESTAMPTZ
);

CREATE INDEX data_exports_user_idx ON data_exports (user_id, requested_at DESC);
CREATE INDEX data_exports_expiry_idx ON data_exports (expires_at) WHERE s3_key IS NOT NULL;

-- The audit log stays append-only and immutable, with one exception: when a
-- transaction sets mnema.audit_scrub = 'on' it may blank the names and details
-- of entries about a deleted account (never ids, actions or times). Nothing
-- else can be changed that way, and rows are still never inserted by it.
CREATE OR REPLACE FUNCTION audit_log_immutable() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'UPDATE'
       AND current_setting('mnema.audit_scrub', true) = 'on'
       AND NEW.id = OLD.id
       AND NEW.created_at = OLD.created_at
       AND NEW.actor_id IS NOT DISTINCT FROM OLD.actor_id
       AND NEW.action = OLD.action
       AND NEW.target_type = OLD.target_type
       AND NEW.target_id = OLD.target_id
       AND NEW.request_id = OLD.request_id
       AND NEW.channel_id IS NOT DISTINCT FROM OLD.channel_id THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'audit_log entries are immutable';
END;
$$ LANGUAGE plpgsql;

-- A deleted account is final. Whatever application code tries (an enable, a
-- password set or reset, a ban lift), the database refuses to bring a
-- tombstone back: it stays deleted and disabled, with no password, address,
-- avatar, name or role of its own. Other columns (counters) may still change.
CREATE FUNCTION users_deleted_is_final() RETURNS trigger AS $$
BEGIN
    IF OLD.deleted_at IS NOT NULL AND (
           NEW.deleted_at IS DISTINCT FROM OLD.deleted_at
        OR NEW.disabled_at IS NULL
        OR NEW.password_hash IS DISTINCT FROM OLD.password_hash
        OR NEW.username IS DISTINCT FROM OLD.username
        OR NEW.role IS DISTINCT FROM OLD.role
        OR NEW.email IS NOT NULL
        OR NEW.avatar_s3_key IS NOT NULL
        OR NEW.banned_at IS NOT NULL) THEN
        RAISE EXCEPTION 'a deleted account cannot be changed' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER users_deleted_final BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION users_deleted_is_final();
