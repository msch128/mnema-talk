-- ==========================================================
-- Mnema Talk - Initial Database Schema (PostgreSQL 17)
-- ==========================================================

-- Enable pgcrypto for UUID generation if not already active
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- 1. Users
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(32) NOT NULL UNIQUE,
    display_name VARCHAR(64) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(16) NOT NULL DEFAULT 'user' CHECK (role IN ('admin', 'user')),
    avatar_s3_key VARCHAR(512),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);

-- 2. Registration Invites (Only created by Admin 'Herzog')
CREATE TABLE IF NOT EXISTS invites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(64) NOT NULL UNIQUE,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    max_uses INT DEFAULT NULL, -- NULL = unlimited
    uses_count INT NOT NULL DEFAULT 0,
    expires_at TIMESTAMPTZ DEFAULT NULL, -- NULL = no expiry
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_invites_code ON invites(code);

-- 3. Channel Categories (e.g., 'Text Kanäle', 'Voice Hangouts')
CREATE TABLE IF NOT EXISTS categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(64) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 4. Channels (Text & Voice)
CREATE TABLE IF NOT EXISTS channels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id UUID REFERENCES categories(id) ON DELETE SET NULL,
    name VARCHAR(64) NOT NULL,
    type VARCHAR(16) NOT NULL DEFAULT 'text' CHECK (type IN ('text', 'voice')),
    topic VARCHAR(255) NOT NULL DEFAULT '',
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_channels_category ON channels(category_id);

-- 5. Chat Messages
CREATE TABLE IF NOT EXISTS messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_id UUID NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content TEXT NOT NULL DEFAULT '',
    is_pinned BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_messages_channel_created ON messages(channel_id, created_at DESC);

-- 6. Media & Image Uploads (S3-tracked, managed via Admin Dashboard)
CREATE TABLE IF NOT EXISTS media (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    uploader_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id UUID REFERENCES channels(id) ON DELETE SET NULL,
    message_id UUID REFERENCES messages(id) ON DELETE CASCADE,
    s3_bucket VARCHAR(128) NOT NULL,
    s3_key VARCHAR(512) NOT NULL UNIQUE,
    original_filename VARCHAR(255) NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL,
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index for selective pruning & 30-day retention cleanup
CREATE INDEX IF NOT EXISTS idx_media_created_at ON media(created_at);
CREATE INDEX IF NOT EXISTS idx_media_uploader ON media(uploader_id);
CREATE INDEX IF NOT EXISTS idx_media_channel ON media(channel_id);

-- 7. Server Settings (Retention period, server name, etc.)
CREATE TABLE IF NOT EXISTS server_settings (
    key VARCHAR(64) PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Insert Default Settings
INSERT INTO server_settings (key, value) VALUES
    ('server_name', 'Mnema Talk'),
    ('retention_days', '30'),
    ('max_upload_size_mb', '50')
ON CONFLICT (key) DO NOTHING;

-- Seed Default Categories and Channels if none exist
DO $$
DECLARE
    text_cat_id UUID;
    voice_cat_id UUID;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM categories) THEN
        INSERT INTO categories (id, name, sort_order)
        VALUES (gen_random_uuid(), 'Text-Kanäle', 0)
        RETURNING id INTO text_cat_id;

        INSERT INTO channels (id, category_id, name, type, topic, sort_order)
        VALUES 
            (gen_random_uuid(), text_cat_id, 'general', 'text', 'Allgemeine Diskussionen & Chat', 0),
            (gen_random_uuid(), text_cat_id, 'medien', 'text', 'Bilder, Screenshots & Clips', 1);

        INSERT INTO categories (id, name, sort_order)
        VALUES (gen_random_uuid(), 'Voice-Hangouts', 1)
        RETURNING id INTO voice_cat_id;

        INSERT INTO channels (id, category_id, name, type, topic, sort_order)
        VALUES 
            (gen_random_uuid(), voice_cat_id, 'Lounge', 'voice', 'Offener Sprach-Hangout', 0),
            (gen_random_uuid(), voice_cat_id, 'Gaming 4K', 'voice', 'Source-Quality Screen & Gaming Hangout', 1);
    END IF;
END $$;

