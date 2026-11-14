-- Slash commands can restrict who may run them by default (doc/bots.md,
-- "default_member_permissions"). NULL means everyone who can use commands
-- today (the default for existing rows, so nothing changes for them). A bitfield
-- string in decimal, stored as BIGINT.
ALTER TABLE bot_commands ADD COLUMN default_member_permissions BIGINT CHECK (default_member_permissions IS NULL OR default_member_permissions >= 0);
