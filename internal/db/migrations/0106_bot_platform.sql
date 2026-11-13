-- The bot platform (doc/bots.md, doc/bot-platform.md).
--
-- A bot added from a manifest keeps the manifest's description and homepage,
-- shown in the Bots tab; a bot created by hand has neither.
ALTER TABLE bots
    ADD COLUMN description TEXT NOT NULL DEFAULT '' CHECK (char_length(description) <= 200),
    ADD COLUMN homepage TEXT NOT NULL DEFAULT '' CHECK (char_length(homepage) <= 512);

-- The components (buttons, select menus) of a bot's message, as validated
-- JSON in Discord's shape. A click is checked against this row, so a button
-- on an old message keeps working across restarts: every click creates a new
-- interaction. The row goes with its message.
CREATE TABLE message_components (
    message_id UUID PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    components JSONB NOT NULL CHECK (jsonb_typeof(components) = 'array'),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
