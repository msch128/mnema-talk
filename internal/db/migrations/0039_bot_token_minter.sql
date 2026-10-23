-- Who issued a bot token. A token is only as trustworthy as its minter: the
-- server re-checks on use that the minter still holds Manage bots and could
-- still act as the bot (perm CanActAs). Tokens that exist already have no
-- recorded minter (NULL) and are not re-checked; regenerate them to bind them.
-- Deleting the minter's account deletes the tokens they issued.
ALTER TABLE bot_tokens ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users(id) ON DELETE CASCADE;
