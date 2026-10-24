-- Custom status expiry (1.2): a status line may clear itself at a chosen time.
--
-- The client computes the absolute time from the member's own time zone and
-- sends it; the server stores it. Reads treat a status whose time has passed as
-- empty (see auth userColumns), and a periodic sweep clears the row and
-- broadcasts the usual user_update. NULL means the status never expires.
ALTER TABLE users ADD COLUMN status_expires_at TIMESTAMPTZ;

CREATE INDEX users_status_expires_idx ON users (status_expires_at) WHERE status_expires_at IS NOT NULL;

-- Slow mode may now last up to 24 hours (it was 6 hours since 0031).
ALTER TABLE channels DROP CONSTRAINT channels_slow_mode_seconds_check;
ALTER TABLE channels ADD CONSTRAINT channels_slow_mode_seconds_check CHECK (slow_mode_seconds BETWEEN 0 AND 86400);
