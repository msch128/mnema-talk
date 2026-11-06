-- The built-in Moderator and Admin roles (0034) get Manage reports (1 << 21)
-- and Kick members (1 << 23): the people who moderate members are the ones who
-- read member reports and sign a member out without a lasting sanction. Until
-- now neither role could, so a Moderator saw no reports and /kick refused them.
--
-- Roles are only seeded by migrations (0034), so a fresh install gets the new
-- bits through this file as well, and the seed there stays untouched.
--
-- The roles table has no flag or key for "built in", and the Owner may rename
-- either role, so the grant follows the name together with what the role does:
-- an ordinary role (not @everyone, not managed) called Moderator or Admin that
-- still holds Timeout members (1 << 18), as the seed gave both. A custom role
-- with another name, a role that was renamed away or stripped of Timeout
-- members, @everyone and bot roles stay as they are. The OR makes it safe to
-- run twice and keeps whatever else the Owner has set on these roles.
UPDATE roles
SET permissions = permissions | (1 << 21) | (1 << 23)
WHERE name IN ('Moderator', 'Admin')
  AND NOT is_everyone
  AND NOT managed
  AND (permissions & (1 << 18)) <> 0;
