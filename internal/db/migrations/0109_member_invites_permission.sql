-- Permission CreateMemberInvites (1 << 30), "create invites": lets a member
-- create invites within the bounds of 0108 and see and revoke their own. It is
-- separate from CreateInvites (1 << 13, "manage invites"), which stays the
-- right to list, edit and revoke every invite.
--
-- Who holds it after this migration:
--  * a fresh install (no account yet): @everyone, as perm.DefaultEveryone says;
--  * an existing install: only the roles that already hold Manage invites,
--    so nobody gains the right to bring people in and an upgrade changes
--    nothing for members. The Owner can turn it on for @everyone or a role.

UPDATE roles
SET permissions = permissions | (1 << 30)
WHERE NOT is_everyone
  AND NOT managed
  AND (permissions & (1 << 13)) <> 0;

UPDATE roles
SET permissions = permissions | (1 << 30)
WHERE is_everyone
  AND NOT EXISTS (SELECT 1 FROM users);
