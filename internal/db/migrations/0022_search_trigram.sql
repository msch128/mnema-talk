-- Message search matches substrings (ILIKE '%term%'). A trigram index lets
-- Postgres answer those from the index instead of scanning every message.
-- pg_trgm ships with stock PostgreSQL and is a trusted extension, so the
-- database owner may create it.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX messages_content_trgm_idx ON messages USING GIN (content gin_trgm_ops);
