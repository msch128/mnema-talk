-- The community icon the Owner (or an administrator) uploads. The pictures
-- live in object storage under community/icon/<version>/; the version names
-- them and doubles as the cache key clients see. NULL: no icon, the Mnema
-- icon stays.
ALTER TABLE instance_settings
    ADD COLUMN community_icon_version TEXT,
    ADD COLUMN community_icon_updated_at TIMESTAMPTZ;
