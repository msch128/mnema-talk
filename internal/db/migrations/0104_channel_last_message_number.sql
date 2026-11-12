-- The number of each channel's newest message, written in the message's own
-- transaction while that holds the channel row's lock (messages.insertMessageTx):
-- the messages of one channel commit in the order of their numbers, so
-- GET /api/read-state can say exactly which messages its counts include (all
-- up to this number, none above it) and a client counts no message twice or
-- not at all when the answer and the message's event cross.
-- A conversation's messages count down (dm_number_seq), so the backfill gives a
-- conversation its oldest message's number and the post path its newest; both
-- are unused, since GET /api/read-state leaves conversations out.
ALTER TABLE channels ADD COLUMN last_message_number BIGINT NOT NULL DEFAULT 0;

UPDATE channels c SET last_message_number = newest.n
FROM (SELECT channel_id, max(number) AS n FROM messages GROUP BY channel_id) newest
WHERE newest.channel_id = c.id;
