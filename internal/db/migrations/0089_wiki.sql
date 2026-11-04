-- The wiki (doc/wiki.md): one tree of long-lived pages per community, with a
-- revision for every save. It is a community-level section, not a channel.
--
-- Pages keep their slug for good: a rename leaves a redirect (wiki_redirects),
-- and a deleted page (deleted_at) keeps its slug so it can be restored. Every
-- save, restore and title change is a revision (page_id, rev); pages.rev is the
-- latest one and doubles as the version an editor must name when saving
-- (base_rev), so a concurrent save is refused instead of overwriting.

CREATE TABLE wiki_pages (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug       VARCHAR(80) NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    title      VARCHAR(100) NOT NULL CHECK (char_length(btrim(title)) > 0),
    parent_id  UUID REFERENCES wiki_pages(id) ON DELETE RESTRICT,
    position   INT NOT NULL DEFAULT 0,
    content    TEXT NOT NULL DEFAULT '' CHECK (octet_length(content) <= 51200),
    rev        INT NOT NULL DEFAULT 1,
    locked     BOOLEAN NOT NULL DEFAULT FALSE,
    deleted_at TIMESTAMPTZ,
    deleted_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX wiki_pages_slug ON wiki_pages (slug);
CREATE INDEX wiki_pages_parent ON wiki_pages (parent_id, position, title);

-- Search matches substrings (ILIKE), like message search (0007, 0022).
CREATE INDEX wiki_pages_title_trgm ON wiki_pages USING gin (title gin_trgm_ops);
CREATE INDEX wiki_pages_content_trgm ON wiki_pages USING gin (content gin_trgm_ops);

CREATE TABLE wiki_revisions (
    page_id       UUID NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE,
    rev           INT NOT NULL,
    -- NULL once the editor's account was erased: the text stays, detached from the person.
    editor_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    title         VARCHAR(100) NOT NULL,
    content       TEXT NOT NULL CHECK (octet_length(content) <= 51200),
    summary       VARCHAR(200) NOT NULL DEFAULT '',
    -- The revision a restore copied (kept while the restore is kept).
    restored_from INT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (page_id, rev)
);

CREATE INDEX wiki_revisions_editor ON wiki_revisions (editor_id) WHERE editor_id IS NOT NULL;

-- Old addresses of renamed pages.
CREATE TABLE wiki_redirects (
    slug    VARCHAR(80) PRIMARY KEY CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    page_id UUID NOT NULL REFERENCES wiki_pages(id) ON DELETE CASCADE
);

CREATE INDEX wiki_redirects_page ON wiki_redirects (page_id);

-- New permissions (internal/perm): ViewWiki (1 << 29) goes to @everyone, so the
-- wiki is open to read for everybody; EditWiki (1 << 27) and ManageWiki
-- (1 << 28) go by default to every role that can already moderate members, that
-- is a role holding Timeout members (1 << 18), as ManageEvents did (0070):
-- moderators are who revert vandalism. The grant follows that capability, not a
-- role name. @everyone and managed (bot) roles never get them; every other
-- member needs a role that is given them.
UPDATE roles SET permissions = permissions | (1 << 29) WHERE is_everyone;
UPDATE roles
SET permissions = permissions | (1 << 27) | (1 << 28)
WHERE NOT is_everyone
  AND NOT managed
  AND (permissions & (1 << 18)) <> 0;
