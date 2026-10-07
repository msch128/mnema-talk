-- Optional member limit per voice channel, set by the admin. 0 = no limit
-- (the default); 1-999 = at most that many members in the room at once.
ALTER TABLE channels ADD COLUMN IF NOT EXISTS user_limit INTEGER NOT NULL DEFAULT 0
    CONSTRAINT channels_user_limit_range CHECK (user_limit BETWEEN 0 AND 999);
