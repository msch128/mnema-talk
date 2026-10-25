-- Direct message polish (1.2): what a member does to their own list of
-- conversations. All of it is the member's own view; nothing here deletes or
-- changes a conversation or its messages.
ALTER TABLE dm_members
    -- Set when the member closes the conversation in their list; cleared by
    -- the next message written in it (see chat.gateDM), by the member opening
    -- it again, or by favouriting it.
    ADD COLUMN hidden_at TIMESTAMPTZ,
    -- Place in the member's favourites (0 is first); NULL: not a favourite.
    ADD COLUMN favourite_pos SMALLINT CHECK (favourite_pos >= 0);

ALTER TABLE users
    -- Mute all direct messages: no push, no sound, no highlight; unread still
    -- counts. A conversation's own notification setting overrides it.
    ADD COLUMN dm_mute_all BOOLEAN NOT NULL DEFAULT FALSE,
    -- Whether the member folded the "Direct messages" section of the sidebar.
    ADD COLUMN dm_list_collapsed BOOLEAN NOT NULL DEFAULT FALSE;
