-- Polls (the /poll command and the composer's poll button).
--
-- A poll belongs to one message; the message's content is the question, so
-- search, push, notifications and the data export see it like any text. The
-- options are fixed once the poll exists. A vote is a row (option, member);
-- the primary key makes a second vote for the same option impossible, and
-- the voting code replaces a member's set of votes under a per-member lock.
-- The poll ends at closes_at; ending it early moves closes_at to now.
CREATE TABLE polls (
    message_id UUID PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    multiple   BOOLEAN NOT NULL DEFAULT FALSE,
    -- Anonymous polls never reveal who voted for what, to anyone.
    anonymous  BOOLEAN NOT NULL DEFAULT FALSE,
    closes_at  TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE poll_options (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    poll_id  UUID NOT NULL REFERENCES polls(message_id) ON DELETE CASCADE,
    position SMALLINT NOT NULL CHECK (position BETWEEN 0 AND 9),
    label    VARCHAR(55) NOT NULL CHECK (char_length(btrim(label)) > 0),
    UNIQUE (poll_id, position)
);

CREATE TABLE poll_votes (
    option_id  UUID NOT NULL REFERENCES poll_options(id) ON DELETE CASCADE,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (option_id, user_id)
);

CREATE INDEX poll_votes_user ON poll_votes (user_id);
