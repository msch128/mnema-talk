-- A member's server-wide notification default, below channel and category
-- settings: level, an optional timed mute and "suppress @all and @here".
-- No row means all messages and no suppression.
CREATE TABLE IF NOT EXISTS server_notify (
    user_id           UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    notify_level      VARCHAR(16) NOT NULL DEFAULT 'all'
        CHECK (notify_level IN ('all', 'mentions', 'mute')),
    muted_until       TIMESTAMPTZ,
    suppress_everyone BOOLEAN NOT NULL DEFAULT FALSE
);
