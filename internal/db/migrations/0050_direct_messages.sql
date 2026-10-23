-- Direct messages (1.1): conversations of two to ten members, kept in the
-- channels table (type 'dm') so messages, reactions, attachments, polls and
-- threads use the one message pipeline. They are a deliberate reversal of
-- migration 0003, which removed the earlier 1:1 model; see doc/architecture.md,
-- "Direct messages". A conversation never has a category, a name or
-- permission overwrites, and every query that lists channels leaves 'dm' out.
ALTER TABLE channels DROP CONSTRAINT IF EXISTS channels_type_check;
ALTER TABLE channels ADD CONSTRAINT channels_type_check CHECK (type IN ('text', 'voice', 'dm'));

-- Who is in a conversation. The only access rule for a direct message: a
-- member of the row may read and (subject to permissions) write, nobody else,
-- the Owner and Administrators included.
CREATE TABLE dm_members (
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_by   UUID REFERENCES users(id) ON DELETE SET NULL,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (channel_id, user_id)
);

CREATE INDEX dm_members_user_idx ON dm_members (user_id);
CREATE INDEX dm_members_added_by_idx ON dm_members (added_by);

ALTER TABLE channels
    -- Sorted ids of the two members of a 1:1 conversation, so the pair has
    -- exactly one; NULL for a group.
    ADD COLUMN dm_key TEXT,
    -- Who started it: the one the first-message cap applies to.
    ADD COLUMN dm_created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    -- Whether anybody but the starter has written in it, and how many
    -- messages the starter sent before that. Counted here, not from the
    -- messages, so deleting messages does not reset the cap.
    ADD COLUMN dm_replied BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN dm_creator_sent INT NOT NULL DEFAULT 0;

CREATE UNIQUE INDEX channels_dm_key_idx ON channels (dm_key) WHERE dm_key IS NOT NULL;
CREATE INDEX channels_dm_created_idx ON channels (dm_created_by, created_at) WHERE type = 'dm';

-- Numbers of conversations and of their messages count downwards from -1, in
-- a sequence of their own: drawing them from the public ones would show, as
-- gaps in the numbering of channels and messages, how much is said in private.
CREATE SEQUENCE dm_number_seq AS BIGINT INCREMENT BY -1 START WITH -1 MINVALUE -9223372036854775807 MAXVALUE -1;

-- Who may start a conversation with a member: 'everyone' (the default) or
-- 'nobody'. Existing conversations are not affected.
ALTER TABLE users ADD COLUMN dm_policy TEXT NOT NULL DEFAULT 'everyone' CHECK (dm_policy IN ('everyone', 'nobody'));

-- The Owner's community switch. Off: existing conversations stay readable and
-- nothing new can be written in them or started.
ALTER TABLE instance_settings ADD COLUMN direct_messages_enabled BOOLEAN NOT NULL DEFAULT TRUE;

-- The new permission Send direct messages (bit 1 << 22) is on for @everyone,
-- like everything members could do before: an upgrade changes no role of
-- anybody else.
UPDATE roles SET permissions = permissions | (1 << 22) WHERE is_everyone;

-- Messages of a conversation say so themselves, so counting and filtering do
-- not depend on a join that is gone when a conversation is deleted.
ALTER TABLE messages ADD COLUMN in_dm BOOLEAN NOT NULL DEFAULT FALSE;

-- A member's message total counts the messages of channels, not those of
-- their private conversations (the total is public).
CREATE OR REPLACE FUNCTION messages_count_insert() RETURNS trigger AS $$
BEGIN
    UPDATE users u SET message_count = u.message_count + d.n
    FROM (SELECT user_id, COUNT(*) AS n FROM new_rows WHERE NOT in_dm GROUP BY user_id) d
    WHERE u.id = d.user_id;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION messages_count_delete() RETURNS trigger AS $$
BEGIN
    UPDATE users u SET message_count = GREATEST(u.message_count - d.n, 0)
    FROM (SELECT user_id, COUNT(*) AS n FROM old_rows WHERE NOT in_dm GROUP BY user_id) d
    WHERE u.id = d.user_id;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;
