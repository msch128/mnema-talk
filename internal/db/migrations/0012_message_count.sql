-- Messages per author, kept current by triggers, so the member list does not
-- count every message on each request.
ALTER TABLE users ADD COLUMN IF NOT EXISTS message_count BIGINT NOT NULL DEFAULT 0;

UPDATE users u SET message_count = c.n
FROM (SELECT user_id, COUNT(*) AS n FROM messages GROUP BY user_id) c
WHERE c.user_id = u.id;

CREATE OR REPLACE FUNCTION messages_count_insert() RETURNS trigger AS $$
BEGIN
    UPDATE users u SET message_count = u.message_count + d.n
    FROM (SELECT user_id, COUNT(*) AS n FROM new_rows GROUP BY user_id) d
    WHERE u.id = d.user_id;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION messages_count_delete() RETURNS trigger AS $$
BEGIN
    UPDATE users u SET message_count = GREATEST(u.message_count - d.n, 0)
    FROM (SELECT user_id, COUNT(*) AS n FROM old_rows GROUP BY user_id) d
    WHERE u.id = d.user_id;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql;

-- Statement-level with transition tables: deleting a channel or a thread
-- (cascades included) costs one UPDATE per author, not one per message.
DROP TRIGGER IF EXISTS messages_count_ins ON messages;
CREATE TRIGGER messages_count_ins AFTER INSERT ON messages
    REFERENCING NEW TABLE AS new_rows
    FOR EACH STATEMENT EXECUTE FUNCTION messages_count_insert();

DROP TRIGGER IF EXISTS messages_count_del ON messages;
CREATE TRIGGER messages_count_del AFTER DELETE ON messages
    REFERENCING OLD TABLE AS old_rows
    FOR EACH STATEMENT EXECUTE FUNCTION messages_count_delete();
