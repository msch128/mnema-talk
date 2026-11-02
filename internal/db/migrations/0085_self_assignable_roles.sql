-- Migration 0085: roles members may take on themselves.
--
--  * roles.self_assignable: an admin with Manage roles marks a role that any
--    member can add to or remove from their own account (a pronoun, a game, a
--    notification group). Off by default, and never on for @everyone or a
--    managed (bot) role. What such a role may hold (no Administrator, no
--    admin-area permission) is enforced by the server when the flag or the
--    permissions change.
--  * roles.description: one line that tells members what the role is for; shown
--    in the picker.

ALTER TABLE roles ADD COLUMN IF NOT EXISTS self_assignable BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE roles ADD COLUMN IF NOT EXISTS description VARCHAR(200) NOT NULL DEFAULT '';

DO $$
BEGIN
	IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'roles_self_assignable_plain') THEN
		ALTER TABLE roles ADD CONSTRAINT roles_self_assignable_plain CHECK (NOT self_assignable OR (NOT is_everyone AND NOT managed));
	END IF;
END $$;
