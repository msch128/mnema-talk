-- Avatars used to be stored exactly as uploaded, with their EXIF block (GPS
-- position, camera model). They are now redrawn on upload, and a one-time job
-- at server start redraws the ones stored before. This marks the avatars that
-- are clean: NULL on an account with an avatar means "not redrawn yet".
ALTER TABLE users ADD COLUMN IF NOT EXISTS avatar_reencoded_at TIMESTAMPTZ;
