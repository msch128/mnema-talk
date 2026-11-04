-- Deleting a message makes Postgres check every table that references it. Four
-- of them had no index on the referencing column, so each deleted message
-- scanned the whole table: retention, auto-delete, moderation and channel
-- deletes cost O(messages x rows). saved_messages and forum_post_reads are
-- keyed by user first, message_reminders had only per-user and due indexes, and
-- thread_follows had only a partial index (WHERE following) that the check
-- cannot use. With these, the lookup per deleted message is one index probe.

CREATE INDEX saved_messages_message_idx ON saved_messages (message_id);
CREATE INDEX forum_post_reads_post_idx ON forum_post_reads (post_id);
CREATE INDEX message_reminders_message_idx ON message_reminders (message_id) WHERE message_id IS NOT NULL;

-- The full index also answers "who follows this thread" (thread_id, following),
-- so it replaces the partial one instead of adding a second copy to every write.
CREATE INDEX thread_follows_thread_idx ON thread_follows (thread_id);
DROP INDEX idx_thread_follows_thread;
