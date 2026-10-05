-- Usernames are unique regardless of case: login and registration compare
-- LOWER(username), so this index both enforces it in the database and serves
-- those lookups. Registration has always checked LOWER(username) before
-- inserting, so existing data should not collide; if two accounts differ only
-- in case, this migration fails and startup stops instead of silently picking
-- one. Resolve that by renaming one of them, then restart.
CREATE UNIQUE INDEX IF NOT EXISTS users_username_lower_key ON users (LOWER(username));

-- Redundant: the UNIQUE constraints from 0001 (users_username_key,
-- invites_code_key) already index these columns.
DROP INDEX IF EXISTS idx_users_username;
DROP INDEX IF EXISTS idx_invites_code;

-- Note: 0005 meant to add messages(channel_id, created_at) for unread counts,
-- but idx_messages_channel_created already existed from 0001 as
-- (channel_id, created_at DESC), so it was a no-op. That index serves range
-- scans in both directions, so no further index is needed.
