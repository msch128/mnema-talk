-- Migration 0086: roles with slots for community events.
--
--  * event_roles: the roles of one event (a name, an emoji, a capacity or
--    unlimited, a position); at most 12 per event, enforced by the API.
--  * event_signups: who signed up for which role. state is 'confirmed' or
--    'waitlist'; one sign-up per member and event. A role that is full puts
--    the next sign-up on the waitlist, and the oldest waitlisted member moves
--    up (first in, first out by created_at) when a slot frees.
--  * event_role_templates: sets of roles a moderator picks instead of typing
--    them again. Nothing is visible to members until an administrator enables
--    a template; the presets below ship disabled.

CREATE TABLE event_roles (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    name       VARCHAR(40) NOT NULL CHECK (char_length(btrim(name)) > 0),
    emoji      VARCHAR(32) NOT NULL DEFAULT '',
    capacity   INT CHECK (capacity IS NULL OR capacity BETWEEN 1 AND 1000),
    position   INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (event_id, name)
);

CREATE INDEX event_roles_event ON event_roles (event_id, position);

CREATE TABLE event_signups (
    event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id    UUID NOT NULL REFERENCES event_roles(id) ON DELETE CASCADE,
    state      VARCHAR(10) NOT NULL CHECK (state IN ('confirmed', 'waitlist')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (event_id, user_id)
);

CREATE INDEX event_signups_role ON event_signups (role_id, state, created_at);
CREATE INDEX event_signups_user ON event_signups (user_id);

CREATE TABLE event_role_templates (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(60) NOT NULL UNIQUE CHECK (char_length(btrim(name)) > 0),
    roles      JSONB NOT NULL DEFAULT '[]',
    enabled    BOOLEAN NOT NULL DEFAULT FALSE,
    preset     BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO event_role_templates (name, roles, preset) VALUES
    ('Party of five', '[{"name":"Tank","emoji":"🛡️","capacity":1},{"name":"Heal","emoji":"💚","capacity":1},{"name":"DPS","emoji":"⚔️","capacity":3}]', TRUE),
    ('Raid of ten', '[{"name":"Tank","emoji":"🛡️","capacity":2},{"name":"Heal","emoji":"💚","capacity":2},{"name":"DPS","emoji":"⚔️","capacity":6}]', TRUE);

-- No overbooking, as a last line behind the application. The API takes row
-- locks on an event's roles before it counts and writes, which is what makes
-- the check exact when sign-ups race; these triggers refuse any write that
-- would leave a role with more confirmed members than its capacity, so a bug
-- or a hand-written statement cannot do it either (a plain trigger sees only
-- committed rows and its own, hence the locks).
CREATE FUNCTION event_role_check_capacity() RETURNS trigger AS $$
DECLARE
    v_role UUID;
    cap INT;
BEGIN
    IF TG_TABLE_NAME = 'event_signups' THEN
        IF NEW.state <> 'confirmed' THEN RETURN NULL; END IF;
        v_role := NEW.role_id;
    ELSE
        v_role := NEW.id;
    END IF;
    SELECT capacity INTO cap FROM event_roles WHERE id = v_role;
    IF cap IS NOT NULL AND (SELECT COUNT(*) FROM event_signups WHERE event_signups.role_id = v_role AND state = 'confirmed') > cap THEN
        RAISE EXCEPTION 'the event role % holds more confirmed members than its capacity of %', v_role, cap USING ERRCODE = 'check_violation';
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER event_signups_capacity AFTER INSERT OR UPDATE OF state, role_id ON event_signups
    FOR EACH ROW EXECUTE FUNCTION event_role_check_capacity();
CREATE TRIGGER event_roles_capacity AFTER UPDATE OF capacity ON event_roles
    FOR EACH ROW EXECUTE FUNCTION event_role_check_capacity();
