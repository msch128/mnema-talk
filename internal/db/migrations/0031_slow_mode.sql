-- Slow mode: the least number of seconds between two messages of one member
-- in a channel (0 = off). Members who may manage messages in the channel are
-- exempt (doc/moderation.md).
ALTER TABLE channels ADD COLUMN slow_mode_seconds INT NOT NULL DEFAULT 0
    CHECK (slow_mode_seconds BETWEEN 0 AND 21600);

-- When a member last posted in a channel with slow mode on. Kept apart from
-- the messages so that deleting a message does not reset the wait; the row is
-- upserted in the insert's transaction (its row lock serializes concurrent
-- posts) and rows older than the longest slow mode are pruned.
CREATE TABLE slow_mode_posts (
    channel_id   UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_post_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX slow_mode_posts_age ON slow_mode_posts (last_post_at);
