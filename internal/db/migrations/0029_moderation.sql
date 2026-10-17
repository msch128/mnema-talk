-- Timeouts and bans (doc/moderation.md).
--
-- A timeout silences a member until timeout_until (no posting, reacting,
-- speaking, video or joining a Talk); it ends by itself and is enforced from
-- the permission snapshot, so every surface honours it.
--
-- A ban is a disabled account with a record: banned_at marks it, disabled_at
-- (set at the same time) is what blocks sessions, WebSockets and sign-in
-- everywhere, so no code path can forget it. banned_until NULL is permanent.
-- invite_id remembers which invite the account registered with, so a ban can
-- revoke it and the same code cannot be used again.
ALTER TABLE users
    ADD COLUMN timeout_until  TIMESTAMPTZ,
    ADD COLUMN timeout_reason TEXT NOT NULL DEFAULT '',
    ADD COLUMN timeout_by     UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN banned_at      TIMESTAMPTZ,
    ADD COLUMN banned_until   TIMESTAMPTZ,
    ADD COLUMN ban_reason     TEXT NOT NULL DEFAULT '',
    ADD COLUMN banned_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN invite_id      UUID REFERENCES invites(id) ON DELETE SET NULL;

CREATE INDEX users_timeout ON users (timeout_until) WHERE timeout_until IS NOT NULL;
CREATE INDEX users_banned ON users (banned_at) WHERE banned_at IS NOT NULL;
