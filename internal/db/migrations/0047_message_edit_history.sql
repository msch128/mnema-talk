-- Edit history for moderators. Every edit keeps the version it replaced:
-- the text, when that version was written (edited_at; the message's creation
-- time for the original) and by whom. Only members with Manage messages in
-- the channel can read it through the API, it is not searched or exported to
-- anyone but the author, and it goes with the message (cascade) and with the
-- author's account. The application keeps at most 50 versions per message and,
-- when EDIT_HISTORY_RETENTION_DAYS is set, drops versions replaced longer ago.
CREATE TABLE message_edits (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    message_id  UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    editor_id   UUID NOT NULL REFERENCES users(id),
    content     TEXT NOT NULL,
    edited_at   TIMESTAMPTZ NOT NULL,
    replaced_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX message_edits_message_idx ON message_edits (message_id, id DESC);
CREATE INDEX message_edits_editor_idx ON message_edits (editor_id);
CREATE INDEX message_edits_replaced_idx ON message_edits (replaced_at);
