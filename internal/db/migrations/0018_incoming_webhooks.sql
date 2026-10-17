-- Incoming webhooks.
--
-- A webhook posts messages into one channel. Its messages are authored by a
-- hidden bot account (users.is_bot) that owns nothing else, cannot sign in
-- and is left out of member lists and mentions, so a webhook never
-- impersonates a member and every existing join on users keeps working.
-- The secret token is only stored as a SHA-256 hash (it is 256 random bits,
-- so a fast hash is enough).

ALTER TABLE users ADD COLUMN is_bot BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    bot_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(80) NOT NULL CHECK (char_length(btrim(name)) > 0),
    token_hash BYTEA NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX webhooks_channel ON webhooks(channel_id);
CREATE UNIQUE INDEX webhooks_bot_user ON webhooks(bot_user_id);

-- webhook_id survives only while the webhook exists; the bot account keeps
-- the history readable afterwards. author_name is the per-message username
-- (Discord's "username" override, or the webhook's name when not given).
ALTER TABLE messages ADD COLUMN webhook_id UUID REFERENCES webhooks(id) ON DELETE SET NULL;
ALTER TABLE messages ADD COLUMN author_name VARCHAR(80);
ALTER TABLE messages ADD COLUMN embeds JSONB NOT NULL DEFAULT '[]'::jsonb;
