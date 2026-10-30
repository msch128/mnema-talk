-- Several attachments per message (2.2). The composer uploads each file while
-- the member is still writing: a staged file is a media row with no message
-- yet (staged = TRUE), visible only to its uploader, and swept away after a
-- timeout when no message claims it. Sending claims it in the message's
-- transaction and gives it its position in the message.
ALTER TABLE media ADD COLUMN IF NOT EXISTS staged BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE media ADD COLUMN IF NOT EXISTS position SMALLINT NOT NULL DEFAULT 0;

-- The sweep and the per-member cap on unsent files only look at staged rows.
CREATE INDEX IF NOT EXISTS idx_media_staged ON media (created_at) WHERE staged;
