-- Requests from `mnema-talk unlock` (a separate process) to the running server
-- to forget in-memory login lockouts. The server polls and deletes the rows.
CREATE TABLE login_unlock_requests (
    id           BIGSERIAL PRIMARY KEY,
    username     TEXT,
    all_accounts BOOLEAN NOT NULL DEFAULT FALSE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (all_accounts OR username IS NOT NULL)
);
