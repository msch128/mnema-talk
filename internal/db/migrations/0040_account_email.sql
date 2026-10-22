-- Optional e-mail address per account (account recovery, never required).
--
-- An address is stored as typed (at most 254 characters) and only counts once
-- email_verified_at is set, which needs a working SMTP setup. Two accounts may
-- carry the same unverified address, so adding one never reveals whether
-- somebody else uses it; only a verified address is unique, because password
-- reset finds the account by it. The address is never part of any API response
-- about other members.
ALTER TABLE users
    ADD COLUMN email TEXT,
    ADD COLUMN email_verified_at TIMESTAMPTZ,
    ADD CONSTRAINT users_email_length CHECK (email IS NULL OR char_length(email) BETWEEN 3 AND 254),
    ADD CONSTRAINT users_email_verified_needs_email CHECK (email_verified_at IS NULL OR email IS NOT NULL);

CREATE UNIQUE INDEX users_email_verified_key ON users (LOWER(email)) WHERE email_verified_at IS NOT NULL;

-- Single-use links sent by e-mail: confirming an address and resetting a
-- password. Only the SHA-256 of the 256-bit random token is stored, so a
-- database copy cannot be used to take over an account. A verification token
-- is bound to the address it was sent to; using it after the address changed
-- does nothing.
CREATE TABLE account_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purpose    TEXT NOT NULL CHECK (purpose IN ('email_verify', 'password_reset')),
    token_hash BYTEA NOT NULL UNIQUE,
    email      TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ
);

CREATE INDEX account_tokens_user_idx ON account_tokens (user_id, purpose, created_at DESC);
CREATE INDEX account_tokens_expires_idx ON account_tokens (expires_at);
