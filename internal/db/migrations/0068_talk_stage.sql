-- Stages. A Talk (voice channel) can be a Stage: nobody speaks, shares a camera
-- or a screen there without talk power, which the members who hold Manage stage
-- hand out and take back for the length of the member's stay (doc/talks.md).
-- Talk power and raised hands live in memory only: nothing about a Stage session
-- is stored. A lobby or a temporary Talk is never a Stage.
ALTER TABLE channels
    ADD COLUMN is_stage BOOLEAN NOT NULL DEFAULT FALSE,
    ADD CONSTRAINT channels_stage_is_voice CHECK (NOT is_stage OR type = 'voice'),
    ADD CONSTRAINT channels_stage_plain CHECK (NOT (is_stage AND (is_lobby OR is_temp)));
