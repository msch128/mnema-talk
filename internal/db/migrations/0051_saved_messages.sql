-- Saved messages (1.1): a private bookmark list per member. The row holds a
-- reference and the member's own note, never a copy of the message, so what a
-- member sees of a saved message is decided by their access at read time.
-- Deleting the message (or its channel) or the account removes the row.
CREATE TABLE saved_messages (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    note       TEXT NOT NULL DEFAULT '' CHECK (char_length(note) <= 200),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, message_id)
);

-- The list reads one member's rows newest first.
CREATE INDEX saved_messages_user_idx ON saved_messages (user_id, created_at DESC, message_id DESC);
