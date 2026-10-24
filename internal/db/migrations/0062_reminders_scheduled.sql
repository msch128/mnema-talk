-- Reminders ("Remind me") and scheduled messages ("Send later").
--
-- A reminder points at a message and never copies it: what the member sees
-- when it falls due is decided by their access at that moment. The message may
-- be gone by then (ON DELETE SET NULL); the reminder is still delivered, as "a
-- message you can no longer see". A delivered reminder stays (delivered_at set)
-- until the member dismisses it, so a member who was offline finds it.
CREATE TABLE message_reminders (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id   UUID REFERENCES messages(id) ON DELETE SET NULL,
    remind_at    TIMESTAMPTZ NOT NULL,
    delivered_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX message_reminders_user ON message_reminders (user_id, remind_at);
-- The delivery sweep reads only the reminders still waiting.
CREATE INDEX message_reminders_due ON message_reminders (remind_at) WHERE delivered_at IS NULL;

-- A scheduled message is the author's own text for a channel or conversation,
-- sent by the server at send_at as the author through the normal send path.
-- parent_id and reply_to_id carry no foreign key: a deleted target must refuse
-- the send, not turn a thread reply into a top-level message. Every check
-- (permissions, slow mode, timeout, blocks) happens at send time.
-- A refused one is not lost: it becomes 'failed' with a reason code and stays
-- as a draft the author can edit, reschedule or delete.
CREATE TABLE scheduled_messages (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id     UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    parent_id      UUID,
    reply_to_id    UUID,
    content        TEXT NOT NULL CHECK (char_length(btrim(content)) > 0),
    send_at        TIMESTAMPTZ NOT NULL,
    state          TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'failed')),
    failure_reason TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX scheduled_messages_user ON scheduled_messages (user_id, send_at);
CREATE INDEX scheduled_messages_due ON scheduled_messages (send_at) WHERE state = 'pending';
