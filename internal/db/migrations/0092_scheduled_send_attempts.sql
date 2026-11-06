-- A scheduled message whose send failed for a reason of our own (the database,
-- a timeout) was retried every sweep for ever. attempts counts those failures
-- and retry_at holds the next try, so the retries back off and, after a few,
-- the message becomes a failed draft like a refused one. A new send time
-- resets both.
ALTER TABLE scheduled_messages
    ADD COLUMN attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN retry_at TIMESTAMPTZ;
