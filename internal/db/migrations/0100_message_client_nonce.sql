-- A message written while the connection was down is sent again when it
-- returns, and a retry whose first attempt had already reached the server must
-- not post it twice. The client puts a random id on every message it sends
-- (the "nonce"); the author's second send of the same nonce returns the first
-- message instead of creating another. Unique per author, so nobody can
-- collide with or probe another member's nonces; messages without one (every
-- other writer) are not indexed. Plain CREATE INDEX like 0091 and 0093.
ALTER TABLE messages ADD COLUMN client_nonce UUID;
CREATE UNIQUE INDEX messages_client_nonce_idx ON messages (user_id, client_nonce) WHERE client_nonce IS NOT NULL;
