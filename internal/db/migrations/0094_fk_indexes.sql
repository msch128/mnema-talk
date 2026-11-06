-- Foreign keys with cascade or SET NULL actions whose referencing column led no
-- index. Each delete of the parent row (a channel, user, poll, media row)
-- scanned the whole referencing table to find the rows to remove or null.
CREATE INDEX poll_anon_ballots_poll_idx ON poll_anon_ballots (poll_id);
CREATE INDEX channel_reads_channel_idx ON channel_reads (channel_id);
CREATE INDEX message_reactions_user_idx ON message_reactions (user_id);
CREATE INDEX messages_webhook_idx ON messages (webhook_id) WHERE webhook_id IS NOT NULL;
CREATE INDEX messages_pinned_by_idx ON messages (pinned_by) WHERE pinned_by IS NOT NULL;
CREATE INDEX events_cover_media_idx ON events (cover_media_id) WHERE cover_media_id IS NOT NULL;
CREATE INDEX scheduled_messages_channel_idx ON scheduled_messages (channel_id);
