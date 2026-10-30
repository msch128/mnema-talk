-- A Talk started from a direct message belongs to that conversation: only its
-- members may see or join it (the Owner included), and it goes with the
-- conversation. It is a temporary Talk (is_temp) without a lobby (lobby_id is
-- NULL), so the live side (leader, empty-Talk removal) is the existing one.
ALTER TABLE channels
    ADD COLUMN dm_id UUID REFERENCES channels(id) ON DELETE CASCADE,
    ADD CONSTRAINT channels_dm_talk_is_temp CHECK (dm_id IS NULL OR (is_temp AND type = 'voice'));

CREATE INDEX channels_dm_talk_idx ON channels (dm_id) WHERE dm_id IS NOT NULL;
