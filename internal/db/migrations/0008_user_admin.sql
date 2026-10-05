-- Admin user management: a disabled account cannot log in and loses its
-- sessions; last_seen_at is written when a user's last connection closes.
ALTER TABLE users ADD COLUMN IF NOT EXISTS disabled_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;
