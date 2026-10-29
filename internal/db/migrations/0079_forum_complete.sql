-- Migration 0079: forum channels, the rest of the way.
--
--  * forum_tags: a colour (a name from a fixed palette, '' = none), a manual
--    position and a moderated flag (only members with Manage messages apply or
--    remove such a tag).
--  * forum_settings: per forum, whether a post needs a tag and the guidelines
--    shown above the new-post form.
--  * messages.is_resolved: a post marked as solved/answered.
--  * forum_post_reads: when a member last read a post, for the per-post unread
--    state and "jump to the first unread reply".

ALTER TABLE forum_tags ADD COLUMN IF NOT EXISTS color VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE forum_tags ADD COLUMN IF NOT EXISTS position INT NOT NULL DEFAULT 0;
ALTER TABLE forum_tags ADD COLUMN IF NOT EXISTS moderated BOOLEAN NOT NULL DEFAULT FALSE;

-- Existing tags keep the order they had (by name).
UPDATE forum_tags t SET position = r.rn
FROM (SELECT id, row_number() OVER (PARTITION BY channel_id ORDER BY name) AS rn FROM forum_tags) r
WHERE r.id = t.id AND t.position = 0;

CREATE TABLE IF NOT EXISTS forum_settings (
    channel_id UUID PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
    require_tag BOOLEAN NOT NULL DEFAULT FALSE,
    guidelines VARCHAR(1000) NOT NULL DEFAULT ''
);

ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_resolved BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS forum_post_reads (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    post_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    last_read_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (user_id, post_id)
);
