-- Messages with a lifetime, finer auto-delete, and the size of removed
-- pictures (doc/timed-messages.md).
--
-- Large tables (messages, media): every change below is a catalogue change
-- plus one index build. A new column without a default rewrites nothing, the
-- CHECKs on them are added NOT VALID (every existing row holds NULL there, so
-- none can violate them, and new and updated rows are checked all the same):
-- no scan under the migration's lock. The one scan is the partial index on
-- messages.expires_at, a read of the table that writes an empty index.
--
-- 1. A message with a lifetime. The sender picks the lifetime when sending;
--    the server stores the absolute end, its clock at the insert plus the
--    lifetime (60 seconds to 30 days), and deletes the message then through the
--    path of an author's own delete (internal/chat/expiry). Reads filter by it
--    as well, so a sweep that runs a few seconds late shows nothing expired.
ALTER TABLE messages ADD COLUMN expires_at TIMESTAMPTZ;
ALTER TABLE messages ADD CONSTRAINT messages_lifetime_bounds
    CHECK (expires_at IS NULL OR expires_at - created_at BETWEEN interval '60 seconds' AND interval '30 days') NOT VALID;
-- A message with a lifetime is never pinned: lifetimes are set at sending
-- only, and the pin route refuses such a message with a clear error first.
ALTER TABLE messages ADD CONSTRAINT messages_lifetime_not_pinned
    CHECK (expires_at IS NULL OR NOT is_pinned) NOT VALID;
-- The sweeper reads the next end and the ended rows through this index only.
CREATE INDEX messages_expires_at ON messages (expires_at) WHERE expires_at IS NOT NULL;

-- The lifetime is fixed at sending: no update sets, moves or clears it. The
-- trigger fires only for an UPDATE that names expires_at (none does today).
CREATE FUNCTION message_lifetime_is_fixed() RETURNS trigger AS $$
BEGIN
    IF NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
        RAISE EXCEPTION 'a message''s lifetime is fixed when it is sent' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER message_lifetime_is_fixed BEFORE UPDATE OF expires_at ON messages
    FOR EACH ROW EXECUTE FUNCTION message_lifetime_is_fixed();

-- A scheduled message carries the lifetime its message gets; it starts when
-- the server posts it.
ALTER TABLE scheduled_messages ADD COLUMN lifetime_seconds INT;
ALTER TABLE scheduled_messages ADD CONSTRAINT scheduled_messages_lifetime_bounds
    CHECK (lifetime_seconds IS NULL OR lifetime_seconds BETWEEN 60 AND 2592000) NOT VALID;

-- The community's switch for lifetimes (on by default) and the longest
-- lifetime anybody may choose (30 days by default, which is also the ceiling).
ALTER TABLE instance_settings
    ADD COLUMN message_lifetimes_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN message_lifetime_max_seconds INT NOT NULL DEFAULT 2592000
        CHECK (message_lifetime_max_seconds BETWEEN 60 AND 2592000);

-- 2. Auto-delete in hours, with a scope and a community default.
--
-- channels.auto_delete_hours replaces auto_delete_days (whole days of 1, 7, 30
-- or 90): NULL follows the community default, 0 is off for this channel (the
-- opt-out), anything else is the channel's own period, up to 365 days. The
-- scope says what goes: 'all' the messages, 'attachments' only their files
-- (the text stays and the files show as removed). The API keeps answering
-- auto_delete_days, worked out from the effective setting.
ALTER TABLE channels
    ADD COLUMN auto_delete_hours INT CHECK (auto_delete_hours IS NULL OR auto_delete_hours BETWEEN 0 AND 8760),
    ADD COLUMN auto_delete_scope TEXT NOT NULL DEFAULT 'all' CHECK (auto_delete_scope IN ('all', 'attachments'));

-- Community channels keep their period. A call Talk may still hold one from
-- before the routes refused it (H-L6-07); it was never swept, so it is
-- dropped here instead of carried over.
UPDATE channels SET auto_delete_hours = auto_delete_days * 24
WHERE auto_delete_days > 0 AND type <> 'dm' AND dm_id IS NULL;

-- auto_delete_days = 0 meant off, and from now on a channel without a setting
-- follows the community default. A channel whose last change of the setting
-- (the audit entry channel.auto_delete_set, which records the state after it)
-- turned it off keeps it off: 0, the opt-out. Every other channel follows the
-- default, which is off until the Owner or an Administrator sets one.
UPDATE channels c SET auto_delete_hours = 0
WHERE c.auto_delete_days = 0 AND c.type <> 'dm' AND c.dm_id IS NULL
  AND (SELECT a.details->'after'->>'auto_delete_days' FROM audit_log a
       WHERE a.action = 'channel.auto_delete_set' AND a.target_id = c.id::text
       ORDER BY a.id DESC LIMIT 1) = '0';

-- Conversations and the Talks started from them are private: never swept.
ALTER TABLE channels ADD CONSTRAINT channels_auto_delete_not_private
    CHECK (COALESCE(auto_delete_hours, 0) = 0 OR (type <> 'dm' AND dm_id IS NULL));

-- The old column goes, with its two check constraints (0063). No older
-- binary reads it again: the migration runner refuses to start a version
-- that ships fewer migrations than the database has applied (internal/db,
-- verifyChecksums), and going back is a restore of the pre-update backup
-- (doc/upgrade.md, Rollback).
ALTER TABLE channels DROP COLUMN auto_delete_days;

-- The community default for channels without a setting of their own: off.
ALTER TABLE instance_settings
    ADD COLUMN auto_delete_default_hours INT NOT NULL DEFAULT 0
        CHECK (auto_delete_default_hours BETWEEN 0 AND 8760),
    ADD COLUMN auto_delete_default_scope TEXT NOT NULL DEFAULT 'all'
        CHECK (auto_delete_default_scope IN ('all', 'attachments'));

-- 3. The size of a picture, measured at upload, so a removed one keeps its
--    place in the chat (the placeholder tile takes its size). NULL for
--    anything that is not a picture the server can read, and for files
--    uploaded before this release.
ALTER TABLE media ADD COLUMN width INT, ADD COLUMN height INT;
ALTER TABLE media ADD CONSTRAINT media_picture_size
    CHECK ((width IS NULL OR width BETWEEN 1 AND 65535) AND (height IS NULL OR height BETWEEN 1 AND 65535)) NOT VALID;
