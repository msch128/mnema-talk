-- Create-a-Talk lobbies. A Talk (voice channel) can be marked as a lobby:
-- joining it opens a temporary Talk in the same category instead of a seat in
-- the lobby. A temporary Talk remembers the lobby it came from and its leader
-- (the member who may rename it and set its member limit). Temporary Talks are
-- removed again when they stay empty (see doc/talks.md).
ALTER TABLE channels
    ADD COLUMN is_lobby  BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN is_temp   BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN lobby_id  UUID REFERENCES channels(id) ON DELETE SET NULL,
    ADD COLUMN leader_id UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD CONSTRAINT channels_lobby_is_voice CHECK (NOT is_lobby OR type = 'voice'),
    ADD CONSTRAINT channels_temp_is_voice CHECK (NOT is_temp OR type = 'voice'),
    ADD CONSTRAINT channels_lobby_not_temp CHECK (NOT (is_lobby AND is_temp));

CREATE INDEX channels_temp_lobby_idx ON channels (lobby_id) WHERE is_temp;
