-- Community events (Mnema Talk 1.2).
--
-- Community events have a title, markdown description, start time, optional end time,
-- location (either a Talk voice channel in this server or free text), an optional
-- cover image, and status (scheduled, active, completed, cancelled).
--
-- Members can RSVP (interested, going, not_going). Members RSVP'd 'going' or 'interested'
-- receive a reminder 15 minutes before start. At start time, scheduled events become active.
-- If the location is a Talk, only members with ViewChannel permission on that Talk see the
-- event and can join it.
--
-- The permission MANAGE_EVENTS (1 << 25) allows creating, editing, and cancelling
-- events. No role is granted it by name; Administrators and the Owner hold it implicitly.

CREATE TABLE events (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title            VARCHAR(100) NOT NULL CHECK (char_length(btrim(title)) > 0),
    description      VARCHAR(2000) NOT NULL DEFAULT '',
    start_time       TIMESTAMPTZ NOT NULL,
    end_time         TIMESTAMPTZ CHECK (end_time IS NULL OR end_time >= start_time),
    channel_id       UUID REFERENCES channels(id) ON DELETE CASCADE,
    location         VARCHAR(255),
    cover_media_id   UUID REFERENCES media(id) ON DELETE SET NULL,
    status           VARCHAR(20) NOT NULL DEFAULT 'scheduled' CHECK (status IN ('scheduled', 'active', 'completed', 'cancelled')),
    created_by       UUID REFERENCES users(id) ON DELETE SET NULL,
    reminder_sent_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT events_location_check CHECK (
        (channel_id IS NOT NULL AND (location IS NULL OR location = '')) OR
        (channel_id IS NULL AND location IS NOT NULL AND char_length(btrim(location)) > 0)
    )
);

CREATE INDEX events_start_time ON events (start_time);
CREATE INDEX events_status_start ON events (status, start_time);
CREATE INDEX events_channel_id ON events (channel_id) WHERE channel_id IS NOT NULL;

CREATE TABLE event_rsvps (
    event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    response   VARCHAR(20) NOT NULL CHECK (response IN ('interested', 'going', 'not_going')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (event_id, user_id)
);

CREATE INDEX event_rsvps_user ON event_rsvps (user_id);

-- New permission ManageEvents (1 << 25): no role gets it by default;
-- Administrators and the Owner have it implicitly.
