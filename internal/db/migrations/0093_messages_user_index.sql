-- Nothing led with messages.user_id, so every lookup by author scanned the
-- whole table: the inbox's replies branch, the from: search filter, the account
-- export pages, erasure, and the foreign key check of every user delete.
-- Plain (user_id) on purpose: a (user_id, created_at) index makes the generic
-- plan of the direct message list a merge anti join that takes seconds.
CREATE INDEX messages_user_idx ON messages (user_id);
