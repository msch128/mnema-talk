-- Legacy bot tokens (issued before 0039) have no recorded issuer. On first use
-- after the upgrade the server binds each of them to the person who most
-- plausibly issued it (the bot's creator, else the Owner) and sets this flag,
-- so the admin console can tell an inferred binding from a real one and the
-- audit trail is honest about it. Tokens never used since the upgrade keep
-- created_by NULL and are shown as unbound. Nothing is revoked.
ALTER TABLE bot_tokens ADD COLUMN IF NOT EXISTS minter_inferred BOOLEAN NOT NULL DEFAULT FALSE;
