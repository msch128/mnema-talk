-- A managed role of each bot's own, like Discord's integration role.
--
-- Every bot gets a role named after it (the name follows a rename), held by
-- that bot only. It cannot be renamed by hand, given to anyone else, removed
-- from the bot or deleted; it goes when the bot is deleted. Its permissions
-- and colour are editable like any role's (permission edits run through the
-- central bot invariant in internal/perm). It sits in the managed block at the
-- bottom of the hierarchy, next to the shared Bot role, so everybody who
-- manages bots outranks it.
--
-- Existing bots get an empty role: their current grants stay where they are,
-- so no bot's effective permissions change. There is deliberately no foreign
-- key to users: a TRUNCATE of users (test resets) would cascade into roles.

ALTER TABLE roles ADD COLUMN bot_user_id UUID;
ALTER TABLE roles ADD CONSTRAINT roles_bot_role_is_managed CHECK (bot_user_id IS NULL OR managed);

-- The shared Bot role is still the only one without an owner.
DROP INDEX roles_one_managed;
CREATE UNIQUE INDEX roles_one_managed ON roles ((TRUE)) WHERE managed AND bot_user_id IS NULL;
CREATE UNIQUE INDEX roles_one_per_bot ON roles (bot_user_id) WHERE bot_user_id IS NOT NULL;

CREATE OR REPLACE FUNCTION user_roles_managed_guard() RETURNS trigger AS $$
DECLARE
    r roles%ROWTYPE;
BEGIN
    SELECT * INTO r FROM roles WHERE id = NEW.role_id;
    IF r.managed THEN
        IF r.bot_user_id IS NOT NULL THEN
            IF r.bot_user_id <> NEW.user_id THEN
                RAISE EXCEPTION 'a bot''s own role is held by that bot only';
            END IF;
        ELSIF NOT EXISTS (SELECT 1 FROM bots WHERE user_id = NEW.user_id) THEN
            RAISE EXCEPTION 'the Bot role is held by bots only';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- A bot's own role goes only with its bot (the bot row is deleted first).
CREATE OR REPLACE FUNCTION roles_managed_delete_guard() RETURNS trigger AS $$
BEGIN
    IF OLD.managed AND (OLD.bot_user_id IS NULL OR EXISTS (SELECT 1 FROM bots WHERE user_id = OLD.bot_user_id)) THEN
        RAISE EXCEPTION 'a managed role cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

-- Backfill: one empty role per existing bot, held by it.
INSERT INTO roles (name, color, position, permissions, hoist, mentionable, managed, bot_user_id)
SELECT left(b.name, 32), 0, 1, 0, FALSE, FALSE, TRUE, b.user_id FROM bots b;

INSERT INTO user_roles (user_id, role_id)
SELECT r.bot_user_id, r.id FROM roles r WHERE r.bot_user_id IS NOT NULL
ON CONFLICT DO NOTHING;
