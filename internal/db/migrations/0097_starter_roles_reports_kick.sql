-- The built-in Moderator and Admin roles (0034) get Manage reports (1 << 21)
-- and Kick members (1 << 23): the people who moderate members are the ones who
-- read member reports and sign a member out without a lasting sanction. Until
-- now neither role could, so a Moderator saw no reports and /kick refused them.
--
-- Roles are only seeded by migrations (0034), so a fresh install gets the new
-- bits through this file as well, and the seed there stays untouched.
--
-- The roles table has no flag or key for "built in", so the grant recognises
-- the rows 0034 seeded by what that file wrote: the oldest role of that name,
-- still holding Timeout members (1 << 18), with the seed's colour (Moderator
-- #C4A35A, Admin #2C4A7C), hoisted and not mentionable. A custom or imported
-- role that merely shares the name (a Discord import maps its moderate-members
-- permission to Timeout members) is created later or looks different and stays
-- as it is, as do @everyone and bot roles. The OR makes it safe to run twice
-- and keeps whatever else the Owner has set on these roles.
UPDATE roles r
SET permissions = r.permissions | (1 << 21) | (1 << 23)
WHERE r.name IN ('Moderator', 'Admin')
  AND NOT r.is_everyone
  AND NOT r.managed
  AND (r.permissions & (1 << 18)) <> 0
  AND r.hoist
  AND NOT r.mentionable
  AND r.color = CASE r.name WHEN 'Moderator' THEN ((196 << 16) | (163 << 8) | 90)
                            ELSE ((44 << 16) | (74 << 8) | 124) END
  AND r.id = (SELECT o.id FROM roles o WHERE o.name = r.name
              ORDER BY o.created_at, o.id LIMIT 1);
