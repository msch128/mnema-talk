-- Migration 0069: Forum channels
-- Adds 'forum' channel type, post title and lock status to messages, and forum tags.

ALTER TABLE channels DROP CONSTRAINT IF EXISTS channels_type_check;
ALTER TABLE channels ADD CONSTRAINT channels_type_check CHECK (type IN ('text', 'voice', 'dm', 'forum'));

ALTER TABLE messages ADD COLUMN IF NOT EXISTS title VARCHAR(100);
ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_locked BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS forum_tags (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    name VARCHAR(20) NOT NULL,
    emoji VARCHAR(32) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(channel_id, name)
);

CREATE INDEX IF NOT EXISTS idx_forum_tags_channel ON forum_tags(channel_id);

CREATE TABLE IF NOT EXISTS forum_post_tags (
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tag_id UUID NOT NULL REFERENCES forum_tags(id) ON DELETE CASCADE,
    PRIMARY KEY (message_id, tag_id)
);

CREATE INDEX IF NOT EXISTS idx_forum_post_tags_tag ON forum_post_tags(tag_id);
