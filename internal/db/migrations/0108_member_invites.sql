-- Member invites (doc/roles-and-permissions.md, doc/invites.md).
--
--  * invites.channel_id: the channel an invite lands the joiner in. NULL: the
--    default channel. It goes when the channel does, and the link keeps
--    working.
--  * instance_settings.member_invite_*: the bounds the Owner and
--    Administrators set for invites that members create: the longest expiry,
--    the most uses of one invite, how many of a member's invites may be
--    active at once and how many one member may create per hour.
--  * member_invite_creations: one row per invite a member created, kept for
--    a day. The per-hour limit counts it, not the invites table, so revoking
--    an invite and creating the next one does not get around the limit.
-- The permission that goes with them is granted in 0109.
ALTER TABLE invites
    ADD COLUMN channel_id UUID REFERENCES channels(id) ON DELETE SET NULL;

ALTER TABLE instance_settings
    ADD COLUMN member_invite_max_hours INT NOT NULL DEFAULT 168 CHECK (member_invite_max_hours BETWEEN 1 AND 8760),
    ADD COLUMN member_invite_max_uses INT NOT NULL DEFAULT 25 CHECK (member_invite_max_uses BETWEEN 1 AND 1000),
    ADD COLUMN member_invite_active_cap INT NOT NULL DEFAULT 10 CHECK (member_invite_active_cap BETWEEN 1 AND 100),
    ADD COLUMN member_invite_per_hour INT NOT NULL DEFAULT 10 CHECK (member_invite_per_hour BETWEEN 1 AND 100);

CREATE TABLE member_invite_creations (
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX member_invite_creations_user ON member_invite_creations (user_id, created_at);
