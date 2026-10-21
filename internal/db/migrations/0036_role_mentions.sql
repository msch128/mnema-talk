-- Role mentions and the mentions inbox.
--
-- kind says why a user is mentioned: 0 by name (@username), 1 by @all or
-- @here, 2 through a role (@Role). Rows that exist already were all
-- resolved from names or @all/@here; they keep kind 0, which is the safe
-- reading (the inbox shows them either way).
ALTER TABLE message_mentions ADD COLUMN IF NOT EXISTS kind SMALLINT NOT NULL DEFAULT 0
    CHECK (kind IN (0, 1, 2));

-- The inbox lists a user's mentions newest first.
CREATE INDEX IF NOT EXISTS idx_message_mentions_user_message ON message_mentions(user_id, message_id);

-- The roles a message mentions, so the client can draw a role pill from the
-- role's id even after the role was renamed. A deleted role drops out.
CREATE TABLE IF NOT EXISTS message_role_mentions (
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    role_id    UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (message_id, role_id)
);
