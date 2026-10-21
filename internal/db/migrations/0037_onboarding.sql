-- Onboarding: the community's name, welcome text and rules, and what each
-- member has accepted. Nothing here changes behavior until the Owner writes
-- rules: an empty rules_text enforces nothing, so no upgrade can lock anyone out.
--
-- rules_version counts the published versions of the rules (0 = never set);
-- a member is "pending" while their rules_accepted_version is below it.
ALTER TABLE instance_settings
    ADD COLUMN community_name TEXT NOT NULL DEFAULT '',
    ADD COLUMN welcome_text TEXT NOT NULL DEFAULT '',
    ADD COLUMN rules_text TEXT NOT NULL DEFAULT '',
    ADD COLUMN rules_version INT NOT NULL DEFAULT 0 CHECK (rules_version >= 0),
    ADD COLUMN start_channel_id UUID REFERENCES channels(id) ON DELETE SET NULL;

-- onboarding holds the member's own first-run state (dismissed tips, done
-- steps); the web app owns its keys.
ALTER TABLE users
    ADD COLUMN rules_accepted_version INT NOT NULL DEFAULT 0 CHECK (rules_accepted_version >= 0),
    ADD COLUMN onboarding JSONB NOT NULL DEFAULT '{}'::jsonb;
