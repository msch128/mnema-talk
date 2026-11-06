-- The per-member daily upload budget summed media rows, so deleting a message
-- (which cascades to its attachments) refunded the bytes. Usage is booked in
-- its own table that deletions of files and messages do not touch; the staged
-- sweep prunes rows older than the 24 hour window.
CREATE TABLE upload_usage (
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    bytes      BIGINT      NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX upload_usage_user_idx ON upload_usage (user_id, created_at);
