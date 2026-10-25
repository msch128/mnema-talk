-- Speaking canonical URLs (1.2): every channel except a direct message gets a
-- slug ("general") that the web client uses in /c/:slug and /v/:slug. The id
-- stays the key everywhere; the slug is only an address.
ALTER TABLE channels ADD COLUMN slug TEXT;
CREATE UNIQUE INDEX channels_slug_idx ON channels (slug) WHERE slug IS NOT NULL;

-- Every slug a channel ever had. A slug stays reserved after a rename, so an
-- old link never opens a different channel. The rows go with the channel.
CREATE TABLE channel_slugs (
    slug       TEXT PRIMARY KEY,
    channel_id UUID NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX channel_slugs_channel_idx ON channel_slugs (channel_id, created_at);

-- Backfill in a fixed order (created_at, id) so a re-run elsewhere agrees.
-- The rules mirror chat.Slugify: lowercase, ä/ö/ü/ß spelled out, common
-- accents stripped, [a-z0-9-] only, collapsed dashes, 60 characters at most,
-- "channel" for an empty result, "-2", "-3" ... on a collision.
DO $$
DECLARE
    r RECORD;
    base TEXT;
    cand TEXT;
    n INT;
BEGIN
    FOR r IN SELECT id, name FROM channels WHERE type <> 'dm' ORDER BY created_at, id LOOP
        base := lower(r.name);
        base := replace(replace(replace(replace(base, 'ä', 'ae'), 'ö', 'oe'), 'ü', 'ue'), 'ß', 'ss');
        base := translate(base,
            'àáâãåāăąçćčďéèêëēěęíìîïłñńňóòôõøőřśšşťúùûűůýÿźžżđ',
            'aaaaaaaacccdeeeeeeeiiiilnnnooooooorsssstuuuuuyyzzzd');
        base := regexp_replace(base, '[^a-z0-9]+', '-', 'g');
        base := trim(both '-' from base);
        base := trim(both '-' from left(base, 60));
        IF base = '' THEN base := 'channel'; END IF;
        -- Never look like a legacy /c/<uuid> link.
        IF base ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN base := 'channel-' || base; END IF;
        cand := base;
        n := 1;
        WHILE EXISTS (SELECT 1 FROM channel_slugs WHERE slug = cand) LOOP
            n := n + 1;
            cand := trim(both '-' from left(base, 60 - length(n::text) - 1)) || '-' || n::text;
        END LOOP;
        INSERT INTO channel_slugs (slug, channel_id) VALUES (cand, r.id);
        UPDATE channels SET slug = cand WHERE id = r.id;
    END LOOP;
END $$;
