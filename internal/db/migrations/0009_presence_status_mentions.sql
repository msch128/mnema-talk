-- Display names are short labels (24 characters); longer ones are cut.
UPDATE users SET display_name = RTRIM(LEFT(display_name, 24)) WHERE char_length(display_name) > 24;
ALTER TABLE users ALTER COLUMN display_name TYPE VARCHAR(24);

-- The presence a user chose. Being offline is not a choice: it only follows
-- from having no open connection.
ALTER TABLE users ADD COLUMN IF NOT EXISTS presence VARCHAR(8) NOT NULL DEFAULT 'online'
    CHECK (presence IN ('online', 'away', 'dnd', 'focus'));

-- A short custom status line, set by the user (or cleared by an admin).
ALTER TABLE users ADD COLUMN IF NOT EXISTS status_text VARCHAR(32) NOT NULL DEFAULT '';

-- Who a message mentions, resolved when it is posted or edited: @username,
-- @all (every member) and @here (everyone connected at that moment).
CREATE TABLE IF NOT EXISTS message_mentions (
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (message_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_message_mentions_user ON message_mentions(user_id);

-- Carry over the @username mentions of existing messages.
INSERT INTO message_mentions (message_id, user_id)
SELECT m.id, u.id
FROM messages m
JOIN users u ON u.id <> m.user_id
    AND m.content ~* ('(^|[^A-Za-z0-9_.-])@' || replace(u.username, '.', '\.') || '($|[^A-Za-z0-9_.-])')
ON CONFLICT DO NOTHING;
