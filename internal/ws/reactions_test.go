package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/emojitext"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

func reaction(t *testing.T, c *Client, emoji string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"emoji": emoji})
	c.handle(events.ClientTalkReaction, raw)
}

// reactions returns the emoji of the talk_reaction events in c's queue.
func reactions(t *testing.T, c *Client) []string {
	t.Helper()
	var out []string
	for len(c.send) > 0 {
		var ev struct {
			Type    string
			Payload struct {
				Emoji  string    `json:"emoji"`
				UserID uuid.UUID `json:"user_id"`
			}
		}
		if err := json.Unmarshal(<-c.send, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type == events.TalkReaction {
			out = append(out, ev.Payload.Emoji)
		}
	}
	return out
}

func TestLiveReactionReachesTheTalkOnly(t *testing.T) {
	w := newPermWorld(t)
	w.h.joinVoice(w.owner, w.public)
	w.h.joinVoice(w.insider, w.public)
	drainTypes(t, w.owner)
	drainTypes(t, w.insider)
	drainTypes(t, w.outsider)
	reaction(t, w.insider, "🎉")
	if got := reactions(t, w.owner); len(got) != 1 || got[0] != "🎉" {
		t.Fatalf("the Talk heard %v", got)
	}
	if got := reactions(t, w.insider); len(got) != 1 {
		t.Fatalf("the sender sees their own reaction float: %v", got)
	}
	if got := drainTypes(t, w.outsider); len(got) != 0 {
		t.Fatalf("somebody outside the Talk heard %v", got)
	}
	// Not in a Talk: nothing to react to.
	reaction(t, w.outsider, "👍")
	if got := reactions(t, w.owner); len(got) != 0 {
		t.Fatalf("a reaction from outside the Talk reached it: %v", got)
	}
}

func TestLiveReactionsAreRateLimitedPerMember(t *testing.T) {
	w := newPermWorld(t)
	w.h.joinVoice(w.owner, w.public)
	w.h.joinVoice(w.insider, w.public)
	drainTypes(t, w.owner)
	for i := 0; i < reactionsPer+3; i++ {
		reaction(t, w.insider, "👏")
	}
	if got := reactions(t, w.owner); len(got) != reactionsPer {
		t.Fatalf("%d reactions got through, want %d", len(got), reactionsPer)
	}
	// Another member has an allowance of their own.
	reaction(t, w.owner, "👍")
	if got := reactions(t, w.owner); len(got) != 1 {
		t.Fatalf("the Owner's own allowance: %v", got)
	}
}

func TestLiveReactionAllowanceRefills(t *testing.T) {
	var g windowGate
	id := uuid.New()
	start := time.Now()
	for i := 0; i < reactionsPer; i++ {
		if !g.allow(id, start, reactionsPer, reactionWindow) {
			t.Fatalf("use %d refused", i)
		}
	}
	if g.allow(id, start.Add(time.Second), reactionsPer, reactionWindow) {
		t.Fatal("over the allowance")
	}
	if !g.allow(id, start.Add(reactionWindow+time.Second), reactionsPer, reactionWindow) {
		t.Fatal("the window moved on, the member may react again")
	}
}

func TestLiveReactionRefusesWhatIsNoEmoji(t *testing.T) {
	w := newPermWorld(t)
	w.h.joinVoice(w.owner, w.public)
	w.h.joinVoice(w.insider, w.public)
	drainTypes(t, w.owner)
	for _, bad := range []string{"", "hello there", "<script>", string(make([]byte, 400))} {
		reaction(t, w.insider, bad)
	}
	if got := reactions(t, w.owner); len(got) != 0 {
		t.Fatalf("junk was relayed: %v", got)
	}
	// Custom emoji tokens pass the shape check (existence needs the database).
	token := emojitext.Token("party", uuid.New())
	reaction(t, w.insider, token)
	if got := reactions(t, w.owner); len(got) != 1 || got[0] != token {
		t.Fatalf("custom emoji: %v", got)
	}
}

func TestLiveReactionNeedsAddReactions(t *testing.T) {
	w := newPermWorld(t)
	w.h.joinVoice(w.owner, w.public)
	w.h.joinVoice(w.outsider, w.public)
	drainTypes(t, w.owner)
	w.f.Everyone(perm.DefaultEveryone &^ perm.AddReactions)
	reaction(t, w.outsider, "👍")
	if got := reactions(t, w.owner); len(got) != 0 {
		t.Fatalf("a member who may not react did: %v", got)
	}
}
