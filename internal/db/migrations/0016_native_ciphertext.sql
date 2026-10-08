-- Isolated opaque relay foundation; no production route is mounted.
-- The backend cannot authenticate MLS application contents. Clients must verify
-- a native-created encrypted inner envelope and approved channel/group mapping.
CREATE TABLE native_ciphertext_events (
 id UUID PRIMARY KEY,
 number BIGSERIAL NOT NULL UNIQUE,
 channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
 user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 client_event_id UUID NOT NULL,
 group_id BYTEA NOT NULL CHECK (octet_length(group_id) BETWEEN 1 AND 128),
 ciphertext BYTEA NOT NULL CHECK (octet_length(ciphertext) BETWEEN 1 AND 65536),
 created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(user_id,client_event_id)
);
CREATE INDEX native_ciphertext_channel_number ON native_ciphertext_events(channel_id,number);
