-- Notification levels per channel and category, with timed mutes.
--
-- channel_reads.notify_level gains 'inherit' (follow the category, then 'all').
-- Rows that only carried the old default 'all' (created by reading a channel)
-- become 'inherit', so a category setting reaches them; the column default
-- stays 'all' and the application inserts 'inherit' itself.
ALTER TABLE channel_reads DROP CONSTRAINT IF EXISTS channel_reads_notify_level_check;
ALTER TABLE channel_reads ADD CONSTRAINT channel_reads_notify_level_check
    CHECK (notify_level IN ('all', 'mentions', 'mute', 'inherit'));
UPDATE channel_reads SET notify_level = 'inherit' WHERE notify_level = 'all';

-- A mute with an end; NULL with level 'mute' is "until unmuted".
ALTER TABLE channel_reads ADD COLUMN IF NOT EXISTS muted_until TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS category_notify (
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category_id  UUID NOT NULL REFERENCES categories(id) ON DELETE CASCADE,
    notify_level VARCHAR(16) NOT NULL DEFAULT 'inherit'
        CHECK (notify_level IN ('all', 'mentions', 'mute', 'inherit')),
    muted_until  TIMESTAMPTZ,
    PRIMARY KEY (user_id, category_id)
);
