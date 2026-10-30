//go:build integration

package chat

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/emojitext"
)

// Every kind of emoji reacts in the chat of a Talk exactly as in a text
// channel: Unicode sequences and the community's custom emoji.
func TestReactionsInATalkChatTakeEveryEmoji(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	talk, err := CreateChannel(ctx, f.p, nil, "talk-chat", ChannelTypeVoice, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := CreateMessage(ctx, f.p, NewMessage{ChannelID: talk.ID, UserID: f.u.ID, Content: "hello talk"})
	if err != nil {
		t.Fatal(err)
	}
	var emojiID uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO custom_emoji (name, content_type, s3_key, size_bytes, content_hash)
		VALUES ('party', 'image/png', 'emoji/party.png', 1, 'h') RETURNING id`).Scan(&emojiID); err != nil {
		t.Fatal(err)
	}
	custom := emojitext.Token("party", emojiID)
	emojis := []string{"🎉", "🥳", "👍🏽", "👨‍👩‍👧", "🏳️‍🌈", "🇩🇪", "1️⃣", "🧑🏽‍❤️‍💋‍🧑🏿", custom}
	for _, e := range emojis {
		if _, err := ToggleReactionAs(ctx, f.p, msg, f.u.ID, e, true); err != nil {
			t.Fatalf("react with %q in the Talk chat: %v", e, err)
		}
	}
	got, err := reactionSummary(ctx, f.p, msg)
	if err != nil || len(got) != len(emojis) {
		t.Fatalf("reactions %+v err=%v, want %d", got, err, len(emojis))
	}
	if _, err := ToggleReactionAs(ctx, f.p, msg, f.u.ID, emojitext.Token("gone", uuid.New()), true); apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("a custom emoji that does not exist: %v", err)
	}
}
