-- A webhook may notify @all and @here only if the member who made or last
-- changed it held "Mention everyone" in its channel at that moment. The flag
-- is stored with the webhook; nothing else about its creator is consulted
-- when it posts (created_by is audit information only).
ALTER TABLE webhooks ADD COLUMN mention_everyone BOOLEAN NOT NULL DEFAULT FALSE;
