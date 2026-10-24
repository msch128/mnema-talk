-- Private moderator notes about a member (1.2, doc/moderation.md). A note is
-- moderator data: the member it is about never sees it, and it is not part of
-- their data export. A strike is a note that counts (the badge in the member's
-- Moderation tab).
--
-- subject_id and author_id are plain ids, without foreign keys, on purpose:
-- an erased account stays as its tombstone row ("Deleted member"), so the
-- notes about it, and the notes it wrote, keep a name to show and stay for the
-- moderators (doc/moderation.md, "Moderator notes").
CREATE TABLE moderation_notes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_id  UUID NOT NULL,
    author_id   UUID NOT NULL,
    body        TEXT NOT NULL CHECK (char_length(body) BETWEEN 1 AND 1000),
    strike      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX moderation_notes_subject_idx ON moderation_notes (subject_id, created_at DESC, id DESC);
CREATE INDEX moderation_notes_author_idx ON moderation_notes (author_id);
