-- Web Push (RFC 8030, 8291, 8292).
--
-- push_vapid holds the server's VAPID key pair: generated on first start,
-- one row, never rotated by the app. Browsers bind a subscription to the public
-- key, so the key must outlive restarts and travel with backups (it lives in
-- the same database as the subscriptions it belongs to). The private key is
-- never logged or returned by the API.
CREATE TABLE push_vapid (
    singleton   BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    public_key  TEXT NOT NULL,
    private_key TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One row per browser (device) a member enabled push on. The endpoint is the
-- push service's address for that browser; p256dh and auth are the browser's
-- public key and secret for encrypting the payload (RFC 8291). A subscription
-- belongs to exactly one account: registering the same endpoint again (another
-- account signed in on that browser) moves it, so signing in as someone else
-- never keeps delivering the previous account's messages.
CREATE TABLE push_subscriptions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint        TEXT NOT NULL UNIQUE CHECK (char_length(endpoint) <= 2048),
    p256dh          TEXT NOT NULL,
    auth            TEXT NOT NULL,
    label           VARCHAR(64) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_success_at TIMESTAMPTZ,
    -- Failed deliveries in a row; a subscription that keeps failing is removed.
    failures        INT NOT NULL DEFAULT 0 CHECK (failures >= 0)
);

CREATE INDEX push_subscriptions_user_idx ON push_subscriptions (user_id);

-- Whether a push carries the text of the message (true) or only the channel
-- and sender (false). The operator can force "no preview" with
-- PUSH_PREVIEWS=off regardless of this setting.
ALTER TABLE users ADD COLUMN push_preview BOOLEAN NOT NULL DEFAULT TRUE;
