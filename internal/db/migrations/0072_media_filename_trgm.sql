-- Trigram index on media.original_filename for case-insensitive substring filename searches.
CREATE INDEX IF NOT EXISTS media_original_filename_trgm_idx ON media USING GIN (original_filename gin_trgm_ops);
