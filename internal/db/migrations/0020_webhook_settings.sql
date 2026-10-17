-- Per-webhook settings of incoming webhooks. Webhooks are an admin tool: the
-- Owner and Administrators create and configure them, and a webhook posts with
-- exactly these settings, whoever made it (created_by is audit information).
ALTER TABLE webhooks
    ADD COLUMN allow_everyone BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN allow_attachments BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN allow_embeds BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN allow_user_mentions BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN allow_identity_override BOOLEAN NOT NULL DEFAULT TRUE;
