-- Short, stable numbers for links (/c/12-general/m/4821) next to the UUIDs,
-- which stay the primary keys. Existing rows are numbered in creation order;
-- new rows draw from an identity sequence that continues after them. Numbers
-- are never reused, so a link to a deleted message never points at another.
ALTER TABLE channels ADD COLUMN IF NOT EXISTS number BIGINT;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS number BIGINT;

UPDATE channels c SET number = n.rn
FROM (SELECT id, ROW_NUMBER() OVER (ORDER BY created_at, id) AS rn FROM channels) n
WHERE c.id = n.id;

UPDATE messages m SET number = n.rn
FROM (SELECT id, ROW_NUMBER() OVER (ORDER BY created_at, id) AS rn FROM messages) n
WHERE m.id = n.id;

ALTER TABLE channels ALTER COLUMN number SET NOT NULL;
ALTER TABLE messages ALTER COLUMN number SET NOT NULL;

DO $$
DECLARE
    next_channel BIGINT := (SELECT COALESCE(MAX(number), 0) + 1 FROM channels);
    next_message BIGINT := (SELECT COALESCE(MAX(number), 0) + 1 FROM messages);
BEGIN
    EXECUTE format('ALTER TABLE channels ALTER COLUMN number ADD GENERATED ALWAYS AS IDENTITY (START WITH %s)', next_channel);
    EXECUTE format('ALTER TABLE messages ALTER COLUMN number ADD GENERATED ALWAYS AS IDENTITY (START WITH %s)', next_message);
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_channels_number ON channels (number);
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_number ON messages (number);
