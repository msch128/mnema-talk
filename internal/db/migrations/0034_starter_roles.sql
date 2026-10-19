-- Moderator and Admin, assigned to nobody.
--
-- Member is @everyone (migration 0017) and the Owner is the account with
-- users.role = 'admin', so neither is a role here. These two are ordinary
-- roles (not managed): the Owner can rename or delete them. They are hoisted
-- and not mentionable. Neither gets Administrator (that bit is every
-- permission and ignores channel overwrites), Manage bots or Manage webhooks.
--
-- A name that already exists is skipped, so an upgrade does not change that
-- role's permissions, position or members and does not reorder anyone else.
-- Missing names land at the top, Moderator first and then Admin, so when both
-- are new Admin is one position higher.
--
-- Bits are the same shifts as internal/perm (iota from bit 0). Moderator:
-- Manage messages (5), Disconnect members (10), View audit log (17), Timeout
-- members (18). Admin adds Manage channels (11), Manage roles (12), Manage
-- invites (13), Manage members (15) and Ban members (19).
-- Colours: Moderator #C4A35A (muted amber), Admin #2C4A7C (a deeper blue).

INSERT INTO roles (name, color, position, permissions, hoist, mentionable, managed)
SELECT 'Moderator',
       ((196 << 16) | (163 << 8) | 90),
       COALESCE((SELECT MAX(position) FROM roles), 0) + 1,
       ((1 << 5) | (1 << 10) | (1 << 17) | (1 << 18)),
       TRUE, FALSE, FALSE
WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = 'Moderator');

INSERT INTO roles (name, color, position, permissions, hoist, mentionable, managed)
SELECT 'Admin',
       ((44 << 16) | (74 << 8) | 124),
       COALESCE((SELECT MAX(position) FROM roles), 0) + 1,
       ((1 << 5) | (1 << 10) | (1 << 17) | (1 << 18)
        | (1 << 11) | (1 << 12) | (1 << 13) | (1 << 15) | (1 << 19)),
       TRUE, FALSE, FALSE
WHERE NOT EXISTS (SELECT 1 FROM roles WHERE name = 'Admin');
