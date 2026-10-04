-- Session revocation: every JWT carries the user's token_version; bumping it
-- (password change, logout everywhere) invalidates all existing sessions.
ALTER TABLE users ADD COLUMN IF NOT EXISTS token_version INT NOT NULL DEFAULT 0;

-- Attachments are looked up per message on every history load.
CREATE INDEX IF NOT EXISTS idx_media_message ON media(message_id);

-- Settings were never read by the application; retention and limits come from env.
DROP TABLE IF EXISTS server_settings;
