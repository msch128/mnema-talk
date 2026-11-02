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
