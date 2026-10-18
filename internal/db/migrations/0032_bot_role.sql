-- The built-in Bot role.
--
-- A managed role every bot holds and nobody else can. It cannot be renamed,
-- removed from a bot or deleted; its colour, position, hoist and permissions
-- are editable like any role's (permission edits run through the central bot
-- invariant in internal/perm). It starts with no permissions, so a bot still
-- does nothing until an admin grants it something, and it is hoisted so bots
-- are listed together in the member list.

ALTER TABLE roles ADD COLUMN managed BOOLEAN NOT NULL DEFAULT FALSE;
CREATE UNIQUE INDEX roles_one_managed ON roles ((TRUE)) WHERE managed;

-- Make room at the bottom of the hierarchy (just above @everyone).
UPDATE roles SET position = position + 1 WHERE NOT is_everyone;
INSERT INTO roles (name, color, position, permissions, hoist, mentionable, managed)
VALUES ('Bot', 0, 1, 0, TRUE, FALSE, TRUE);

-- Only bots may hold a managed role; a managed role cannot be deleted.
CREATE FUNCTION user_roles_managed_guard() RETURNS trigger AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM roles WHERE id = NEW.role_id AND managed)
       AND NOT EXISTS (SELECT 1 FROM bots WHERE user_id = NEW.user_id) THEN
        RAISE EXCEPTION 'the Bot role is held by bots only';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER user_roles_managed_guard BEFORE INSERT ON user_roles
    FOR EACH ROW EXECUTE FUNCTION user_roles_managed_guard();

CREATE FUNCTION roles_managed_delete_guard() RETURNS trigger AS $$
BEGIN
    IF OLD.managed THEN
        RAISE EXCEPTION 'the Bot role cannot be deleted';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER roles_managed_delete_guard BEFORE DELETE ON roles
    FOR EACH ROW EXECUTE FUNCTION roles_managed_delete_guard();

-- Backfill: every existing bot holds it.
INSERT INTO user_roles (user_id, role_id)
SELECT b.user_id, r.id FROM bots b CROSS JOIN roles r WHERE r.managed
ON CONFLICT DO NOTHING;
