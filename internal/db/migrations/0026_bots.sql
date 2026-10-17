-- Bot accounts.
--
-- A bot is a hidden account (users.is_bot, no password: it can never sign in,
-- sessions and login skip bots) plus a row here. Like a member it gets its
-- permissions from roles (user_roles) and channel overwrites, through the one
-- permission engine (internal/perm); unlike a member it does not inherit
-- @everyone, so a new bot can do nothing until an admin gives it a role.
-- allowed_intents is the set of gateway intents (Discord's bit numbers) the
-- bot may ask for. API tokens are random 256-bit secrets; only a SHA-256 hash
-- is stored.

CREATE TABLE bots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(32) NOT NULL CHECK (char_length(btrim(name)) > 0),
    allowed_intents BIGINT NOT NULL DEFAULT 0 CHECK (allowed_intents >= 0),
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_active_at TIMESTAMPTZ
);

CREATE TABLE bot_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    bot_id UUID NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
    name VARCHAR(32) NOT NULL DEFAULT '',
    token_hash BYTEA NOT NULL UNIQUE,
    -- The last characters of the token, to tell tokens apart in the list.
    hint VARCHAR(8) NOT NULL,
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX bot_tokens_bot ON bot_tokens(bot_id);
