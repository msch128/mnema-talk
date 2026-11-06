-- A message written while the connection was down is sent again when it
-- returns, and a retry whose first attempt had already reached the server must
-- not post it twice. The client puts a random id on every message it sends
-- (the "nonce"); the author's second send of the same nonce returns the first
-- message instead of creating another. The nonce lives in its own table, not
-- on the message: when the message was deleted meanwhile (a moderator purge),
-- the row stays with message_id NULL and the retry is answered "deleted"
-- instead of posting the text again. Unique per author, so nobody can collide
-- with or probe another member's nonces. Rows older than a day are pruned.
CREATE TABLE message_nonces (
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    nonce      UUID        NOT NULL,
    channel_id UUID        NOT NULL,
    message_id UUID        REFERENCES messages (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, nonce)
);
-- Plain CREATE INDEX like 0091 and 0093.
CREATE INDEX message_nonces_created_idx ON message_nonces (created_at);
CREATE INDEX message_nonces_message_idx ON message_nonces (message_id) WHERE message_id IS NOT NULL;
