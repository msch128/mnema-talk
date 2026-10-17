-- Roles and permissions.
--
-- The Owner stays users.role = 'admin' (ADMIN_USERNAME); everything below the
-- Owner is a custom role. @everyone is the one implicit role every member
-- has; it is seeded with exactly what members could already do, so an
-- upgrade changes nothing until the Owner edits roles. permissions, allow and
-- deny are bit sets (see internal/perm); bit 0 is Administrator.

CREATE TABLE roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(32) NOT NULL CHECK (char_length(btrim(name)) > 0),
    color INT NOT NULL DEFAULT 0 CHECK (color BETWEEN 0 AND 16777215),
    -- Higher is more powerful; @everyone is 0, custom roles count up from 1.
    position INT NOT NULL,
    permissions BIGINT NOT NULL DEFAULT 0 CHECK (permissions >= 0),
    hoist BOOLEAN NOT NULL DEFAULT FALSE,
    mentionable BOOLEAN NOT NULL DEFAULT FALSE,
    is_everyone BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK ((is_everyone AND position = 0) OR (NOT is_everyone AND position > 0))
);
CREATE UNIQUE INDEX roles_one_everyone ON roles ((TRUE)) WHERE is_everyone;

-- ViewChannel, SendMessages, AttachFiles, AddReactions, MentionEveryone,
-- Connect, Speak, Video (perm.DefaultEveryone).
INSERT INTO roles (name, position, permissions, is_everyone) VALUES ('@everyone', 0, 990, TRUE);

CREATE TABLE user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);
CREATE INDEX user_roles_role ON user_roles(role_id);

-- Allow/deny of one role or one member on one category or channel. A channel
-- inherits its category's overwrites and its own ones win.
CREATE TABLE permission_overwrites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID REFERENCES channels(id) ON DELETE CASCADE,
    category_id UUID REFERENCES categories(id) ON DELETE CASCADE,
    role_id UUID REFERENCES roles(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    allow BIGINT NOT NULL DEFAULT 0 CHECK (allow >= 0),
    deny BIGINT NOT NULL DEFAULT 0 CHECK (deny >= 0),
    CHECK ((channel_id IS NULL) <> (category_id IS NULL)),
    CHECK ((role_id IS NULL) <> (user_id IS NULL)),
    CHECK ((allow & deny) = 0)
);
CREATE UNIQUE INDEX overwrites_channel_role ON permission_overwrites(channel_id, role_id) WHERE channel_id IS NOT NULL AND role_id IS NOT NULL;
CREATE UNIQUE INDEX overwrites_channel_user ON permission_overwrites(channel_id, user_id) WHERE channel_id IS NOT NULL AND user_id IS NOT NULL;
CREATE UNIQUE INDEX overwrites_category_role ON permission_overwrites(category_id, role_id) WHERE category_id IS NOT NULL AND role_id IS NOT NULL;
CREATE UNIQUE INDEX overwrites_category_user ON permission_overwrites(category_id, user_id) WHERE category_id IS NOT NULL AND user_id IS NOT NULL;
CREATE INDEX overwrites_role ON permission_overwrites(role_id) WHERE role_id IS NOT NULL;
CREATE INDEX overwrites_user ON permission_overwrites(user_id) WHERE user_id IS NOT NULL;
