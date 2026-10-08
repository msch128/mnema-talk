-- Persistence-only native session foundation. No native HTTP routes are
-- exposed by this migration; browser cookie sessions remain unchanged.
CREATE TABLE native_session_families (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    client_instance_id UUID NOT NULL,
    issued_token_version INT NOT NULL CHECK (issued_token_version >= 0),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    refresh_after TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    revoke_reason TEXT,
    CHECK (expires_at > created_at AND expires_at <= created_at + INTERVAL '720 hours'),
    CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CHECK ((revoked_at IS NULL AND revoke_reason IS NULL) OR
        (revoked_at IS NOT NULL AND revoke_reason IS NOT NULL AND revoke_reason IN
            ('logout', 'refresh_reuse', 'version_mismatch', 'rotation_limit')))
);
CREATE INDEX native_families_user ON native_session_families(user_id);
CREATE INDEX native_families_expiry ON native_session_families(expires_at);

CREATE TABLE native_access_tokens (
    token_hash BYTEA PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    family_id UUID NOT NULL REFERENCES native_session_families(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    CHECK (expires_at > created_at AND expires_at <= created_at + INTERVAL '5 minutes')
);
CREATE INDEX native_access_family ON native_access_tokens(family_id);
CREATE INDEX native_access_expiry ON native_access_tokens(expires_at);

CREATE TABLE native_refresh_tokens (
    token_hash BYTEA PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    family_id UUID NOT NULL REFERENCES native_session_families(id) ON DELETE CASCADE,
    sequence BIGINT NOT NULL CHECK (sequence BETWEEN 0 AND 65535),
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    UNIQUE (family_id, sequence),
    CHECK (expires_at > created_at),
    CHECK (consumed_at IS NULL OR consumed_at >= created_at)
);
CREATE UNIQUE INDEX native_refresh_live ON native_refresh_tokens(family_id)
    WHERE consumed_at IS NULL;
CREATE INDEX native_refresh_expiry ON native_refresh_tokens(expires_at);
