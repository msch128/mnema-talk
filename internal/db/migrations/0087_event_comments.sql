-- Migration 0087: comments on community events.
--
-- A flat discussion under each event (no replies): up to 2000 characters
-- per comment, at most 500 comments per event (enforced by the API).
-- event_comment_mutes: a member who switched the notices of an event's
-- comments off. A comment's author is deleted with the account (see account
-- erasure), the comments of an event go with the event.

CREATE TABLE event_comments (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id   UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content    VARCHAR(2000) NOT NULL CHECK (char_length(btrim(content)) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    edited_at  TIMESTAMPTZ
);

CREATE INDEX event_comments_event ON event_comments (event_id, created_at, id);
CREATE INDEX event_comments_user ON event_comments (user_id);

CREATE TABLE event_comment_mutes (
    event_id UUID NOT NULL REFERENCES events(id) ON DELETE CASCADE,
    user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (event_id, user_id)
);
