package push

import (
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/chat/core"
)

// A message with a lifetime under ten minutes is pushed as "New message"
// alone: no text, no sender, no channel; a longer one is pushed as usual.
func TestAShortLivedMessageIsPushedWithoutItsText(t *testing.T) {
	created := time.Now()
	for _, c := range []struct {
		lifetime time.Duration
		short    bool
	}{{time.Minute, true}, {9*time.Minute + 59*time.Second, true}, {10 * time.Minute, false}, {time.Hour, false}, {0, false}} {
		msg := core.Message{CreatedAt: created}
		if c.lifetime > 0 {
			end := created.Add(c.lifetime)
			msg.ExpiresAt = &end
		}
		if got := shortLived(msg); got != c.short {
			t.Errorf("lifetime %v: short %v, want %v", c.lifetime, got, c.short)
		}
	}
	for _, dm := range []bool{false, true} {
		n := notice()
		n.Content, n.Sender, n.DM, n.Short = "meet at the back door", "Alice", dm, true
		p := build(n, "en", true)
		if p.Title != "New message" || p.Body != "" {
			t.Errorf("dm %v: title %q body %q", dm, p.Title, p.Body)
		}
		n.Count = 3
		p = build(n, "en", true)
		if strings.Contains(p.Title+p.Body, "back door") || strings.Contains(p.Body, "Alice") && strings.Contains(p.Body, "door") {
			t.Errorf("dm %v: a burst ending in a short-lived message shows its text: %q %q", dm, p.Title, p.Body)
		}
	}
}
