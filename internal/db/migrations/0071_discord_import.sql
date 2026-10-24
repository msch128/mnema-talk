-- Discord server structure imports (doc/import.md). Each import is recorded
-- with the exact objects it created, so "Undo import" can remove those and
-- nothing else. The object ids carry no foreign key on purpose: an object an
-- administrator deleted by hand since is simply gone and gets skipped on undo.
CREATE TABLE discord_imports (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_code   TEXT NOT NULL CHECK (char_length(template_code) BETWEEN 1 AND 64),
    template_name   TEXT NOT NULL DEFAULT '',
    created_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    created_by_name TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    categories      INT NOT NULL DEFAULT 0 CHECK (categories >= 0),
    channels        INT NOT NULL DEFAULT 0 CHECK (channels >= 0),
    roles           INT NOT NULL DEFAULT 0 CHECK (roles >= 0),
    undone_at       TIMESTAMPTZ,
    undone_by       UUID REFERENCES users(id) ON DELETE SET NULL
);
CREATE INDEX discord_imports_created_idx ON discord_imports (created_at DESC);

CREATE TABLE discord_import_objects (
    import_id UUID NOT NULL REFERENCES discord_imports(id) ON DELETE CASCADE,
    kind      TEXT NOT NULL CHECK (kind IN ('category', 'channel', 'role')),
    object_id UUID NOT NULL,
    PRIMARY KEY (import_id, kind, object_id)
);
