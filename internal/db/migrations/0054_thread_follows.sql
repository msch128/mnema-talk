-- Thread follows: who is told about the replies of a thread. A reply notifies
-- only the thread's followers. The root's author, everyone who replied and
-- everyone mentioned in a reply follow automatically; an explicit unfollow is
-- kept as following = FALSE so that a later reply does not undo it.
--
-- 0055 is reserved for notification preference columns and stays unused: the
-- in-page notification follows the existing push_preview setting.
CREATE TABLE IF NOT EXISTS thread_follows (
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    thread_id  UUID        NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    following  BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, thread_id)
);

-- Fan-out reads the followers of one thread.
CREATE INDEX IF NOT EXISTS idx_thread_follows_thread ON thread_follows (thread_id) WHERE following;

-- Existing threads keep notifying the people who took part, as before.
INSERT INTO thread_follows (user_id, thread_id)
SELECT DISTINCT r.user_id, r.parent_id FROM messages r
WHERE r.parent_id IS NOT NULL
ON CONFLICT DO NOTHING;

INSERT INTO thread_follows (user_id, thread_id)
SELECT DISTINCT root.user_id, root.id FROM messages root
WHERE root.parent_id IS NULL AND EXISTS (SELECT 1 FROM messages c WHERE c.parent_id = root.id)
ON CONFLICT DO NOTHING;
