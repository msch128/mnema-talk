-- idx_messages_channel_created (0001, channel_id + created_at DESC; the copy
-- 0005 meant to add was a no-op) is covered by messages_channel_unread_idx
-- (0076): same leading columns, and an index scans in both directions. Both
-- were maintained on every message insert, so the older one only costs writes.
DROP INDEX IF EXISTS idx_messages_channel_created;
