-- Pictures in wiki pages. The file itself is an ordinary media row without a
-- channel (so every member can load it, like an avatar); this table marks it as
-- a wiki picture, for the cap and for removing pictures no revision uses.
CREATE TABLE wiki_images (
    media_id   UUID PRIMARY KEY REFERENCES media(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX wiki_images_created_idx ON wiki_images (created_at);
