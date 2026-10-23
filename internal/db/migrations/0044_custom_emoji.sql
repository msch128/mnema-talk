-- Custom emoji per instance. The image lives in object storage under emoji/
-- (re-encoded by the server, never the uploaded bytes); the row names it.
-- Messages and reactions refer to an emoji by id (<:name:id>), so renaming or
-- deleting one never breaks stored text.
CREATE TABLE custom_emoji (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL CHECK (name ~ '^[a-z0-9_]{2,32}$'),
    content_type TEXT NOT NULL CHECK (content_type IN ('image/png', 'image/gif')),
    animated     BOOLEAN NOT NULL DEFAULT FALSE,
    s3_key       TEXT NOT NULL UNIQUE,
    -- First frame of an animation, for members who prefer reduced motion.
    still_s3_key TEXT,
    size_bytes   INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    created_by   UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX custom_emoji_name_idx ON custom_emoji (name);

-- A reaction can be a custom emoji token (<:name:uuid>, 41 to 73 characters).
ALTER TABLE message_reactions ALTER COLUMN emoji TYPE VARCHAR(80);
