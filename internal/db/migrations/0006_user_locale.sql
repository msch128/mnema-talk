-- UI language per account. Empty = not chosen yet; the client then picks one
-- from the browser language (German by default) and stores it.
ALTER TABLE users ADD COLUMN IF NOT EXISTS locale VARCHAR(8) NOT NULL DEFAULT '';
