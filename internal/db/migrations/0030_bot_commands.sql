-- Slash commands of bots.
--
-- A bot registers commands over its API; members see them in the composer of
-- every channel the bot can view and send in. An admin can switch a command
-- off (disabled) without the bot being able to switch it back on: a bot
-- re-registering its commands keeps the flag. Interactions (a member running a
-- command) are short-lived and kept in memory, not here.

CREATE TABLE bot_commands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    name VARCHAR(32) NOT NULL CHECK (name ~ '^[a-z0-9_-]{1,32}$'),
    description VARCHAR(100) NOT NULL CHECK (char_length(btrim(description)) > 0),
    options JSONB NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(options) = 'array'),
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (bot_id, name)
);
