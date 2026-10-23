-- 0007 and 0022 each created the same GIN trigram index on messages.content
-- (idx_messages_content_trgm and messages_content_trgm_idx), so every message
-- write paid for it twice. Keep the 0022 one and drop the older duplicate.
-- DROP INDEX takes a short exclusive lock on messages; the migrator runs each
-- file in a transaction, so this cannot be CONCURRENTLY.
DROP INDEX IF EXISTS idx_messages_content_trgm;
