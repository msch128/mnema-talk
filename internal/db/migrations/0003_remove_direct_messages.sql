-- Direct messages were removed from the product. Their channels (and, via
-- ON DELETE CASCADE, their messages, reactions and media rows) go away, the
-- participant table is dropped and channels are limited to text and voice again.
DELETE FROM channels WHERE type = 'dm';
DROP TABLE IF EXISTS dm_channels;

ALTER TABLE channels DROP CONSTRAINT IF EXISTS channels_type_check;
ALTER TABLE channels ADD CONSTRAINT channels_type_check CHECK (type IN ('text', 'voice'));
