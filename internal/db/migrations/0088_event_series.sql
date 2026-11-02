-- Migration 0088: recurring events.
--
-- event_series holds what a series repeats: its frequency, the wall-clock
-- time zone its occurrences keep (a 19:00 event stays at 19:00 across a
-- change to or from summer time), the anchor that fixes every start
-- (occurrence n starts n steps after the anchor, so a monthly series on the
-- 31st never drifts), and the template new occurrences copy: title,
-- description, place, duration and roles. Occurrences are ordinary events
-- (events.series_id, events.series_index); the scheduler creates them a
-- bounded time ahead. Sign-ups never carry over, each occurrence starts with
-- the role definitions only.

CREATE TABLE event_series (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    frequency        VARCHAR(10) NOT NULL CHECK (frequency IN ('weekly', 'biweekly', 'monthly')),
    timezone         VARCHAR(64) NOT NULL DEFAULT 'UTC',
    anchor_start     TIMESTAMPTZ NOT NULL,
    anchor_index     INT NOT NULL DEFAULT 0,
    next_index       INT NOT NULL DEFAULT 1,
    end_index        INT,
    until_at         TIMESTAMPTZ,
    duration_seconds INT CHECK (duration_seconds IS NULL OR duration_seconds >= 0),
    title            VARCHAR(100) NOT NULL,
    description      VARCHAR(2000) NOT NULL DEFAULT '',
    channel_id       UUID REFERENCES channels(id) ON DELETE CASCADE,
    location         VARCHAR(255),
    roles            JSONB NOT NULL DEFAULT '[]',
    status           VARCHAR(10) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'ended')),
    created_by       UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX event_series_active ON event_series (status) WHERE status = 'active';

ALTER TABLE events ADD COLUMN series_id UUID REFERENCES event_series(id) ON DELETE SET NULL;
ALTER TABLE events ADD COLUMN series_index INT;
CREATE UNIQUE INDEX events_series_index ON events (series_id, series_index) WHERE series_id IS NOT NULL;
