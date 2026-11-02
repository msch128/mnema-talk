-- The vanity invite link: one custom slug for the whole community, served at
-- /join/<slug>. NULL: none. The slug is stored lowercase, so the unique index
-- makes it case-insensitive in effect. vanity_accepts_signups decides whether
-- the link works as a permanent public invite (sign-up with the slug as the
-- code); off, it only leads visitors to the normal sign-in page.
ALTER TABLE instance_settings
    ADD COLUMN vanity_slug TEXT CHECK (vanity_slug ~ '^[a-z0-9]([a-z0-9]|-[a-z0-9])*$' AND char_length(vanity_slug) BETWEEN 3 AND 32),
    ADD COLUMN vanity_accepts_signups BOOLEAN NOT NULL DEFAULT false;

CREATE UNIQUE INDEX instance_settings_vanity_slug ON instance_settings (LOWER(vanity_slug)) WHERE vanity_slug IS NOT NULL;
