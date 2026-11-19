-- GET /api/read-state reads every channel's messages since the member's read
-- marker. Its mention counts join message_mentions on messages.id, which
-- messages_channel_unread_idx (0076) does not hold, so that half fetched
-- every unread message from the table. With id in the index the summary runs
-- from the index alone, in one pass per channel (readstate.ReadStateSQL): for
-- a member away from most channels of a million-message community it went
-- from 0.66 s to 0.11 s. counted lets the member list's totals without hidden
-- channels (chat.MessageCountsByAuthor) read the index alone as well. This
-- replaces 0076's index, which has the same leading columns. The migrator
-- runs each file in a transaction, so this cannot be CONCURRENTLY: building
-- it took about 0.45 s on a million messages.
CREATE INDEX messages_channel_unread_id_idx
    ON messages (channel_id, created_at) INCLUDE (user_id, parent_id, reply_to_id, id, counted);
DROP INDEX IF EXISTS messages_channel_unread_idx;
