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
-- still holding Timeout members (1 << 18), created within five minutes of the
-- moment 0034 was applied (schema_migrations.applied_at; roles.created_at is
-- the same transaction's NOW()). A seeded role the Owner renamed therefore
-- does not hand the grant to a new "Moderator" created later, and a custom
-- role that existed before 0034 is not mistaken for the seed. Where the
-- record of 0034 is missing, the age check is skipped. Colour, hoist and mentionable are
-- not checked: an Owner who recoloured or made the seeded role mentionable
-- still moderates with it and must get the grant. A custom or imported role
-- that merely shares the name (a Discord import maps its moderate-members
-- permission to Timeout members) is created later and stays as it is, as do
-- @everyone and bot roles. The OR makes it safe to run twice
-- and keeps whatever else the Owner has set on these roles.
UPDATE roles r
SET permissions = r.permissions | (1 << 21) | (1 << 23)
WHERE r.name IN ('Moderator', 'Admin')
  AND NOT r.is_everyone
  AND NOT r.managed
  AND (r.permissions & (1 << 18)) <> 0
  AND (NOT EXISTS (SELECT 1 FROM schema_migrations m WHERE m.filename = '0034_starter_roles.sql')
       OR EXISTS (SELECT 1 FROM schema_migrations m WHERE m.filename = '0034_starter_roles.sql'
                  AND r.created_at BETWEEN m.applied_at - INTERVAL '5 minutes' AND m.applied_at + INTERVAL '5 minutes'))
  AND r.id = (SELECT o.id FROM roles o WHERE o.name = r.name
              ORDER BY o.created_at, o.id LIMIT 1);
