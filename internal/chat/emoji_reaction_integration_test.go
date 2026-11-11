//go:build integration

package chat

import (
	"context"
	"net/http"
	"testing"

	"sync"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/emojitext"
)

// Every kind of emoji reacts in the chat of a Talk exactly as in a text
// channel: Unicode sequences and the community's custom emoji.
func TestReactionsInATalkChatTakeEveryEmoji(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	talk, err := channels.CreateChannelWithStage(ctx, f.p, nil, "talk-chat", core.ChannelTypeVoice, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := messages.CreateMessage(ctx, f.p, messages.NewMessage{ChannelID: talk.ID, UserID: f.u.ID, Content: "hello talk"})
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
		if _, err := messages.ToggleReactionAs(ctx, f.p, msg, f.u.ID, e, true); err != nil {
			t.Fatalf("react with %q in the Talk chat: %v", e, err)
		}
	}
	got, err := messages.ReactionSummary(ctx, f.p, msg)
	if err != nil || len(got) != len(emojis) {
		t.Fatalf("reactions %+v err=%v, want %d", got, err, len(emojis))
	}
	if _, err := messages.ToggleReactionAs(ctx, f.p, msg, f.u.ID, emojitext.Token("gone", uuid.New()), true); apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("a custom emoji that does not exist: %v", err)
	}
}

func TestBotReactionsAreSetNotToggled(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	const emoji = "👍"
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, _, err := messages.SetReactionAs(ctx, f.p, f.m, f.u.ID, emoji, true, true); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	reactions, changed, err := messages.SetReactionAs(ctx, f.p, f.m, f.u.ID, emoji, true, true)
	if err != nil || changed || len(reactions) != 1 || reactions[0].Count != 1 {
		t.Fatalf("after concurrent adds: %+v changed=%v err=%v", reactions, changed, err)
	}
	if _, changed, err = messages.SetReactionAs(ctx, f.p, f.m, f.u.ID, emoji, false, false); err != nil || !changed {
		t.Fatalf("taking back without Add reactions: changed=%v err=%v", changed, err)
	}
	reactions, changed, err = messages.SetReactionAs(ctx, f.p, f.m, f.u.ID, emoji, false, true)
	if err != nil || changed || len(reactions) != 0 {
		t.Fatalf("removing twice: %+v changed=%v err=%v", reactions, changed, err)
	}
	if _, _, err = messages.SetReactionAs(ctx, f.p, f.m, f.u.ID, emoji, true, false); apiStatus(err) != http.StatusForbidden {
		t.Fatalf("add without permission: %v", err)
	}
}

// A reaction stored before the formatting-character rule stays removable by
// its owner, while adding such an emoji is refused.
func TestStoredReactionWithFormattingCharacterCanBeTakenBack(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	const old = "👍\u200e"
	if _, err := f.p.Exec(ctx, `INSERT INTO message_reactions (message_id, user_id, emoji) VALUES ($1, $2, $3)`, f.m, f.u.ID, old); err != nil {
		t.Fatal(err)
	}
	if _, err := core.ValidateReactionRef(old); err != nil {
		t.Fatalf("lookup rule rejects the stored emoji: %v", err)
	}
	reactions, changed, err := messages.SetReactionAs(ctx, f.p, f.m, f.u.ID, old, false, true)
	if err != nil || !changed || len(reactions) != 0 {
		t.Fatalf("taking back: %+v changed=%v err=%v", reactions, changed, err)
	}
	if _, _, err := messages.SetReactionAs(ctx, f.p, f.m, f.u.ID, old, true, true); apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("adding it again: %v", err)
	}
	if _, err := messages.ToggleReactionAs(ctx, f.p, f.m, f.u.ID, old, true); apiStatus(err) != http.StatusBadRequest {
		t.Fatalf("toggling it on: %v", err)
	}
}
