-- Migration 0083: raid protection (doc/moderation.md).
--
--  * join_limit / join_window_minutes: at most join_limit new members within
--    join_window_minutes; further sign-ups wait. 0 turns the throttle off.
--  * lockdown*: the Owner's and Administrators' one-click switch. While it is
--    on nobody new can sign up; lockdown_pause_invites also stops creating
--    invites. lockdown_until ends it by itself (NULL: until switched off).
--  * min_age_*_minutes: how long after joining a new member may write, react
--    and speak in Talks. 0 turns a gate off. Members with Bypass new member
--    limits (permission bit 26), Administrators and the Owner are exempt.
ALTER TABLE instance_settings
    ADD COLUMN join_limit INT NOT NULL DEFAULT 0 CHECK (join_limit BETWEEN 0 AND 1000),
    ADD COLUMN join_window_minutes INT NOT NULL DEFAULT 10 CHECK (join_window_minutes BETWEEN 1 AND 1440),
    ADD COLUMN lockdown BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN lockdown_pause_invites BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN lockdown_until TIMESTAMPTZ,
    ADD COLUMN lockdown_started_at TIMESTAMPTZ,
    ADD COLUMN min_age_write_minutes INT NOT NULL DEFAULT 0 CHECK (min_age_write_minutes BETWEEN 0 AND 43200),
    ADD COLUMN min_age_react_minutes INT NOT NULL DEFAULT 0 CHECK (min_age_react_minutes BETWEEN 0 AND 43200),
    ADD COLUMN min_age_speak_minutes INT NOT NULL DEFAULT 0 CHECK (min_age_speak_minutes BETWEEN 0 AND 43200);

-- New permission BypassNewMemberLimits (1 << 26): exempts a role's members from
-- the minimum membership age. Moderators are the people who vouch for others, so
-- it goes by default to every role that can already time members out (1 << 18),
-- as ManageEvents did (0070). @everyone and managed (bot) roles never get it.
UPDATE roles
SET permissions = permissions | (1 << 26)
WHERE NOT is_everyone
  AND NOT managed
  AND (permissions & (1 << 18)) <> 0;
