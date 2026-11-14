-- A bot can be given access to every channel in one step (doc/bots.md, "Give
-- a bot access to every channel"). The flag records the choice: the bot's own
-- role then holds its channel permissions server-wide, and a channel that
-- plain members cannot see (@everyone is denied View channels there) stays
-- hidden from the bot unless an overwrite of its own role allows it.
ALTER TABLE bots ADD COLUMN access_all BOOLEAN NOT NULL DEFAULT FALSE;
