-- Member-to-member blocking and message reports (1.1).
--
-- A block is private to the blocker: nothing in any other member's data says
-- who blocked them. It is checked at read and delivery time, so it also covers
-- messages that already exist.
CREATE TABLE user_blocks (
    blocker_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (blocker_id, blocked_id),
    CHECK (blocker_id <> blocked_id)
);

-- Deleting an account looks its blocks up from the blocked side as well.
CREATE INDEX user_blocks_blocked_idx ON user_blocks (blocked_id);

-- A report keeps its own copy of the reported message and a little context
-- (kind 'dm' is a direct message): the author may delete the message
-- afterwards, and a moderator cannot open a direct message conversation any
-- other way. message_id and channel_id are plain ids on purpose, without
-- foreign keys, so a report outlives the message and the conversation.
CREATE TABLE message_reports (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id      UUID REFERENCES users(id) ON DELETE SET NULL,
    reported_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    message_id       UUID NOT NULL,
    channel_id       UUID NOT NULL,
    kind             TEXT NOT NULL CHECK (kind IN ('channel', 'dm')),
    channel_name     TEXT NOT NULL DEFAULT '',
    category         TEXT NOT NULL CHECK (category IN ('spam', 'harassment', 'illegal', 'other')),
    note             TEXT NOT NULL DEFAULT '' CHECK (char_length(note) <= 500),
    snapshot         JSONB NOT NULL,
    status           TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved', 'dismissed')),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    resolved_at      TIMESTAMPTZ,
    resolution_note  TEXT NOT NULL DEFAULT '' CHECK (char_length(resolution_note) <= 500)
);

CREATE INDEX message_reports_status_idx ON message_reports (status, created_at DESC, id DESC);
-- One open report per reporter and message: repeating it adds nothing.
CREATE UNIQUE INDEX message_reports_open_once_idx ON message_reports (reporter_id, message_id) WHERE status = 'open';
CREATE INDEX message_reports_reported_idx ON message_reports (reported_user_id);
