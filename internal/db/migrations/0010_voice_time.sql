-- Total time a user has spent in voice rooms, added up when a stay ends.
ALTER TABLE users ADD COLUMN IF NOT EXISTS voice_seconds BIGINT NOT NULL DEFAULT 0;
