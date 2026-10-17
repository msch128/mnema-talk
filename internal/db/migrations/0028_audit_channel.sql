-- The channel an audit entry is about (message deletions and edits, Talk
-- presence), so the log can be filtered by what the reader may view.
ALTER TABLE audit_log ADD COLUMN channel_id UUID;
CREATE INDEX audit_log_channel ON audit_log (channel_id, id DESC) WHERE channel_id IS NOT NULL;
