-- Browser sessions that were signed out explicitly. The session token carries a
-- per-session id (jti); logging out records it here until the token would have
-- expired anyway, so a copied cookie stops working. Tokens issued before this
-- migration have no jti and stay valid until they expire.
CREATE TABLE revoked_sessions (
    jti        TEXT PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX revoked_sessions_expires_idx ON revoked_sessions (expires_at);
