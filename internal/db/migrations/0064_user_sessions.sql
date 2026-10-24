-- The browser sessions of each member, for "Where am I signed in" and the
-- inactivity sign-out. The session itself stays the signed cookie; this table
-- only describes it (jti is the cookie's session id).
--
-- A row appears when the session is issued at sign-in. A session that was
-- issued before this table existed gets its row on its first authenticated
-- request after the upgrade, with last_active_at = NOW(): its inactivity clock
-- starts at the upgrade, nobody is signed out by it.
--
-- device is a coarse label ("Firefox on Linux") parsed from the user agent;
-- neither the raw user agent nor an IP address is stored. last_active_at is
-- refreshed at most every few minutes per session. A row whose token_version
-- no longer matches the member's was ended by a password change or "sign out
-- everywhere" and is removed by the sweeper.
CREATE TABLE user_sessions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    jti            TEXT NOT NULL UNIQUE CHECK (jti <> ''),
    device         VARCHAR(64) NOT NULL DEFAULT '',
    token_version  INT NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ NOT NULL
);

CREATE INDEX user_sessions_user_idx ON user_sessions (user_id);
CREATE INDEX user_sessions_activity_idx ON user_sessions (last_active_at);
