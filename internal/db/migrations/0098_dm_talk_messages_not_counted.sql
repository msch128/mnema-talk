-- The message total on a profile and in the member list is public, but the chat
-- of a conversation's call (a Talk with dm_id) is as private as the
-- conversation. Those messages carry in_dm = FALSE (readers look for the
-- conversation's members on the message's own channel), so the counting
-- triggers could not tell them apart. messages.counted says whether a message
-- belongs in the public total; it is decided once, when the row is inserted,
-- because the Talk (and with it the lookup) is already gone when a cascade
-- deletes its messages.
ALTER TABLE messages ADD COLUMN counted BOOLEAN NOT NULL DEFAULT TRUE;

UPDATE messages SET counted = FALSE
WHERE channel_id IN (SELECT id FROM channels WHERE dm_id IS NOT NULL);

CREATE OR REPLACE FUNCTION messages_set_counted() RETURNS trigger AS $$
BEGIN
    IF EXISTS (SELECT 1 FROM channels WHERE id = NEW.channel_id AND dm_id IS NOT NULL) THEN
        NEW.counted := FALSE;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS messages_counted_ins ON messages;
CREATE TRIGGER messages_counted_ins BEFORE INSERT ON messages
    FOR EACH ROW EXECUTE FUNCTION messages_set_counted();

CREATE OR REPLACE FUNCTION messages_count_insert() RETURNS trigger AS $$
BEGIN
    UPDATE users u SET message_count = u.message_count + d.n
    FROM (SELECT user_id, COUNT(*) AS n FROM new_rows WHERE NOT in_dm AND counted GROUP BY user_id) d
    WHERE u.id = d.user_id;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION messages_count_delete() RETURNS trigger AS $$
BEGIN
    UPDATE users u SET message_count = GREATEST(u.message_count - d.n, 0)
    FROM (SELECT user_id, COUNT(*) AS n FROM old_rows WHERE NOT in_dm AND counted GROUP BY user_id) d
    WHERE u.id = d.user_id;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- One grouped recompute (safe to run again): the totals so far included the
-- messages in conversations' calls.
UPDATE users u SET message_count = COALESCE(c.n, 0)
FROM users x
LEFT JOIN (SELECT user_id, COUNT(*) AS n FROM messages WHERE NOT in_dm AND counted GROUP BY user_id) c ON c.user_id = x.id
WHERE u.id = x.id AND u.message_count IS DISTINCT FROM COALESCE(c.n, 0);
