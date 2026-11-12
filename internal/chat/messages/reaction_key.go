package messages

import (
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// reactionKeySQL is core.ReactionKey in SQL: the stored emoji without U+FE0F.
// Rows of one member may hold one emoji in both forms (stored before the rule);
// every reaction query matches, counts and groups by this key, so they are one
// reaction without a migration that rewrites them.
const reactionKeySQL = `replace(emoji, chr(65039), '')`

// oneReaction finishes a reaction grouped by key: shown in its one form, each
// member once (in the order they first gave it) and counted once.
func oneReaction(r core.ReactionSummary) core.ReactionSummary {
	r.Emoji = core.ReactionForm(r.Emoji)
	users := make([]uuid.UUID, 0, len(r.Users))
	seen := make(map[uuid.UUID]bool, len(r.Users))
	for _, u := range r.Users {
		if !seen[u] {
			seen[u] = true
			users = append(users, u)
		}
	}
	r.Users, r.Count = users, len(users)
	return r
}
