-- Speaking canonical URLs (1.2): every channel except a direct message gets a
-- slug ("general") that the web client uses in /c/:slug and /v/:slug. The id
-- stays the key everywhere; the slug is only an address.
ALTER TABLE channels ADD COLUMN slug TEXT;
CREATE UNIQUE INDEX channels_slug_idx ON channels (slug) WHERE slug IS NOT NULL;

-- The former slugs a channel keeps (the newest ten). A slug stays reserved after a rename, so an
-- old link never opens a different channel. The rows go with the channel.
CREATE TABLE channel_slugs (
    slug       TEXT PRIMARY KEY,
    channel_id UUID NOT NULL REFERENCES channels (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- When the slug was last current; orders the former slugs (a channel keeps
    -- its newest ten).
    used_at    TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX channel_slugs_channel_idx ON channel_slugs (channel_id, used_at);

-- Backfill in a fixed order (created_at, id) so a re-run elsewhere agrees.
-- The rules mirror chat.Slugify: lowercase, ä/ö/ü/ß spelled out, common
-- accents stripped, [a-z0-9-] only, collapsed dashes, 60 characters at most,
-- "channel" for an empty result, "-" and the first characters of the channel's own id on a collision.
DO $$
DECLARE
    r RECORD;
    base TEXT;
    cand TEXT;
    n INT;
    sfx TEXT;
BEGIN
    FOR r IN SELECT id, name, is_temp FROM channels WHERE type <> 'dm' ORDER BY created_at, id LOOP
        IF r.is_temp THEN
            -- A temporary Talk has a fixed slug and no history.
            UPDATE channels SET slug = 'talk-' || left(replace(r.id::text, '-', ''), 8) WHERE id = r.id;
            CONTINUE;
        END IF;
        base := lower(r.name);
        base := replace(replace(replace(replace(base, 'ä', 'ae'), 'ö', 'oe'), 'ü', 'ue'), 'ß', 'ss');
        base := translate(base,
            'àáâãåāăąçćčďéèêëēěęíìîïłñńňóòôõøőřśšşťúùûűůýÿźžżđ',
            'aaaaaaaacccdeeeeeeeiiiilnnnooooooorsssstuuuuuyyzzzd');
        base := regexp_replace(base, '[^a-z0-9]+', '-', 'g');
        base := trim(both '-' from base);
        base := trim(both '-' from left(base, 60));
        IF base = '' THEN base := 'channel'; END IF;
        -- Never look like a temporary Talk's fixed slug.
        IF base ~ '^talk-[0-9a-f]{8}$' THEN base := base || '-x'; END IF;
        -- Never look like a legacy /c/<uuid> link.
        IF base ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN base := 'channel-' || base; END IF;
        cand := base;
        n := 0;
        WHILE EXISTS (SELECT 1 FROM channel_slugs WHERE slug = cand) AND n < 5 LOOP
            n := n + 1;
            sfx := '-' || left(replace(r.id::text, '-', ''), (ARRAY[4, 6, 8, 12, 32])[n]);
            cand := trim(both '-' from left(base, 60 - length(sfx))) || sfx;
        END LOOP;
        INSERT INTO channel_slugs (slug, channel_id) VALUES (cand, r.id);
        UPDATE channels SET slug = cand WHERE id = r.id;
    END LOOP;
END $$;
