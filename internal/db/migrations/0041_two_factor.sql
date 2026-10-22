-- Optional two-factor sign-in (TOTP, RFC 6238).
--
-- user_totp holds the shared secret, encrypted with a key derived from the
-- server's JWT_SECRET (a database copy alone cannot generate codes). A row is
-- pending until confirmed_at is set by a valid first code; only confirmed rows
-- make sign-in ask for a second factor. last_step is the newest 30 second step
-- already accepted, so a code cannot be used twice.
CREATE TABLE user_totp (
    user_id      UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret_enc   BYTEA NOT NULL,
    confirmed_at TIMESTAMPTZ,
    last_step    BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Single-use recovery codes. Only a keyed hash (HMAC under a key derived from
-- JWT_SECRET) is stored; used codes stay as used rows.
CREATE TABLE recovery_codes (
    user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash BYTEA NOT NULL,
    used_at   TIMESTAMPTZ,
    PRIMARY KEY (user_id, code_hash)
);

-- The step between a correct password and the second factor. The token is 256
-- random bits (only its SHA-256 is stored), valid for a few minutes, bound to
-- the account and to its token_version at that moment, and allows a handful of
-- code attempts in total.
CREATE TABLE login_challenges (
    token_hash    BYTEA PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_version INT NOT NULL,
    attempts      INT NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ NOT NULL
);

-- Every wrong second-factor code, kept so the daily budget per account holds
-- across restarts. noticed marks failures already reported to the member
-- (a security notice at the next sign-in). The code itself is never stored.
CREATE TABLE second_factor_failures (
    id      BIGSERIAL PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind    TEXT NOT NULL CHECK (kind IN ('totp', 'recovery')),
    at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    noticed BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX second_factor_failures_user_idx ON second_factor_failures (user_id, kind, at DESC);

CREATE INDEX login_challenges_user_idx ON login_challenges (user_id, created_at DESC);
CREATE INDEX login_challenges_expires_idx ON login_challenges (expires_at);
