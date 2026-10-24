-- Automod (1.2): rules that check every new or edited message and every join,
-- and the entries they add to the moderation queue (doc/moderation.md).
--
-- A rule is one row; the settings of its kind live in config (the server
-- validates them). Roles and channels listed in exempt_* are skipped by the
-- rule; they are plain ids on purpose, so a deleted role or channel leaves a
-- harmless stale id instead of blocking its own deletion.
CREATE TABLE automod_rules (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind             TEXT NOT NULL CHECK (kind IN ('words', 'links', 'mentions', 'new_account', 'raid')),
    name             TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 60),
    enabled          BOOLEAN NOT NULL DEFAULT TRUE,
    action           TEXT NOT NULL DEFAULT 'block' CHECK (action IN ('block', 'flag')),
    timeout_seconds  INT NOT NULL DEFAULT 0 CHECK (timeout_seconds = 0 OR timeout_seconds BETWEEN 5 AND 2419200),
    config           JSONB NOT NULL DEFAULT '{}'::jsonb,
    exempt_roles     UUID[] NOT NULL DEFAULT '{}',
    exempt_channels  UUID[] NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Raid protection watches the whole server, so there is at most one rule.
CREATE UNIQUE INDEX automod_rules_one_raid_idx ON automod_rules ((TRUE)) WHERE kind = 'raid';

-- One row. version counts every change to the rules, so the server's compiled
-- copy knows when to load them again. raid_until is when the running raid
-- restriction ends (a raid is running while it lies in the future) and
-- raid_report the queue entry that announced it.
CREATE TABLE automod_state (
    singleton   BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    version     BIGINT NOT NULL DEFAULT 1,
    raid_until  TIMESTAMPTZ,
    raid_report UUID
);
INSERT INTO automod_state (singleton) VALUES (TRUE);

-- Conservative defaults, for new servers and upgrades alike (doc/upgrade.md).
-- The word list starts empty and the rule is off until the Owner adds words.
INSERT INTO automod_rules (kind, name, enabled, action, config) VALUES
    ('mentions', 'Mention spam', TRUE, 'block', '{"max_mentions": 10}'),
    ('new_account', 'New accounts', TRUE, 'block', '{"max_age_seconds": 600, "restrict": "links"}'),
    ('raid', 'Raid protection', TRUE, 'flag', '{"joins": 10, "window_seconds": 10, "restrict_seconds": 600}'),
    ('words', 'Blocked words', FALSE, 'block', '{"words": []}'),
    ('links', 'Links', FALSE, 'block', '{"mode": "block_list", "domains": []}');

-- Queue entries made by automod. A hit has no reporter; the rule name is kept
-- as text so the entry outlives the rule. Entries of one rule and one member
-- merge while open (hit_count), so a spammer cannot flood the queue.
-- A blocked message was never stored, and a raid alert is about no message:
-- both ids may be empty then (kind 'alert' has neither, a blocked message
-- only its channel).
ALTER TABLE message_reports
    ADD COLUMN source TEXT NOT NULL DEFAULT 'member' CHECK (source IN ('member', 'automod')),
    ADD COLUMN automod_rule TEXT NOT NULL DEFAULT '',
    ADD COLUMN automod_outcome TEXT NOT NULL DEFAULT '' CHECK (automod_outcome IN ('', 'blocked', 'flagged', 'alert')),
    ADD COLUMN hit_count INT NOT NULL DEFAULT 1,
    ALTER COLUMN message_id DROP NOT NULL,
    ALTER COLUMN channel_id DROP NOT NULL;

ALTER TABLE message_reports DROP CONSTRAINT message_reports_category_check;
ALTER TABLE message_reports ADD CONSTRAINT message_reports_category_check
    CHECK (category IN ('spam', 'harassment', 'illegal', 'other', 'automod'));
ALTER TABLE message_reports DROP CONSTRAINT message_reports_kind_check;
ALTER TABLE message_reports ADD CONSTRAINT message_reports_kind_check
    CHECK (kind IN ('channel', 'dm', 'alert'));
ALTER TABLE message_reports ADD CONSTRAINT message_reports_ids_check
    CHECK (source = 'automod' OR (message_id IS NOT NULL AND channel_id IS NOT NULL));

CREATE UNIQUE INDEX message_reports_automod_open_idx ON message_reports (reported_user_id, automod_rule)
    WHERE source = 'automod' AND status = 'open' AND reported_user_id IS NOT NULL;
