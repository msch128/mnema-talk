//go:build integration

package ws

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/emojitext"
)

// Live reactions as the database has them: a custom emoji of the community
// reaches the Talk, a deleted or unknown one does not, and so does every
// preset of the react menu.
func TestLiveReactionsFromTheDatabaseCarryCustomEmoji(t *testing.T) {
	s := newDBStage(t)
	var id uuid.UUID
	if err := s.p.QueryRow(context.Background(), `INSERT INTO custom_emoji (name, content_type, s3_key, size_bytes, content_hash)
		VALUES ('party', 'image/png', 'emoji/party.png', 1, 'h') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	custom := emojitext.Token("party", id)
	reaction(t, s.mod, custom)
	if got := reactions(t, s.bob); len(got) != 1 || got[0] != custom {
		t.Fatalf("custom emoji reached %v", got)
	}
	reaction(t, s.mod, emojitext.Token("gone", uuid.New()))
	if got := reactions(t, s.bob); len(got) != 0 {
		t.Fatalf("an unknown custom emoji was relayed: %v", got)
	}
	for i, e := range []string{"👍", "❤️", "😂", "🔥", "🎉", "🚀"} {
		sender := s.amy
		if i >= reactionsPer {
			sender = s.mod
		}
		reaction(t, sender, e)
		if got := reactions(t, s.bob); len(got) != 1 || got[0] != e {
			t.Fatalf("preset %q reached %v", e, got)
		}
	}
}
