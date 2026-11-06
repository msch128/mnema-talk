-- Time in Talks on a profile and in the member list is public, but users.
-- voice_seconds also holds time spent in Talks that @everyone cannot see. The
-- public total is kept apart and only counts stays in Talks everyone can see
-- (decided when the stay starts). No per-stay history exists, so the earlier
-- public part cannot be told from the rest: the public total starts at zero
-- rather than carrying hidden time over. voice_seconds stays as the private
-- total (stays in conversations' calls are not counted in either).
ALTER TABLE users ADD COLUMN public_voice_seconds BIGINT NOT NULL DEFAULT 0;
