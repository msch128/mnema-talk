-- Profile banner: the media ID of the picture across the top of a member's
-- profile (the file is a media row stored under banners/). NULL means none.
ALTER TABLE users ADD COLUMN IF NOT EXISTS banner_s3_key VARCHAR(512);

-- A deleted account is final (see 0043): it keeps no banner either.
CREATE OR REPLACE FUNCTION users_deleted_is_final() RETURNS trigger AS $$
BEGIN
    IF OLD.deleted_at IS NOT NULL AND (
           NEW.deleted_at IS DISTINCT FROM OLD.deleted_at
        OR NEW.disabled_at IS NULL
        OR NEW.password_hash IS DISTINCT FROM OLD.password_hash
        OR NEW.username IS DISTINCT FROM OLD.username
        OR NEW.role IS DISTINCT FROM OLD.role
        OR NEW.email IS NOT NULL
        OR NEW.avatar_s3_key IS NOT NULL
        OR NEW.banner_s3_key IS NOT NULL
        OR NEW.banned_at IS NOT NULL) THEN
        RAISE EXCEPTION 'a deleted account cannot be changed' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
