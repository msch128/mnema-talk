package push

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDirectReminderShowsTheMessageOnlyWhenAllowed(t *testing.T) {
	ch, msg := uuid.New(), uuid.New()
	d := Direct{Kind: DirectReminder, ID: uuid.New(), ChannelID: &ch, MessageID: &msg, Sender: "Alice", Content: "**bring** the cake"}

	shown := buildDirect(d, "en", true, 1)
	if shown.Title != "Reminder" || shown.Body != "Alice: bring the cake" || shown.MessageID == nil {
		t.Fatalf("with preview: %+v", shown)
	}
	hidden := buildDirect(d, "en", false, 1)
	if strings.Contains(hidden.Body, "cake") || strings.Contains(hidden.Body, "Alice") {
		t.Fatalf("a preview-less push carries the text: %+v", hidden)
	}
}

func TestDirectReminderOfAMessageTheMemberCannotSeeSaysNothingAboutIt(t *testing.T) {
	d := Direct{Kind: DirectReminder, ID: uuid.New(), Gone: true, Sender: "Alice", Content: "secret"}
	for _, locale := range []string{"en", "de"} {
		p := buildDirect(d, locale, true, 1)
		if p.ChannelID != nil || p.MessageID != nil || strings.Contains(p.Body, "secret") || strings.Contains(p.Body, "Alice") || p.Body == "" {
			t.Fatalf("%s: %+v", locale, p)
		}
	}
	if got := buildDirect(d, "en", true, 1).Body; got != "A message you can no longer see" {
		t.Fatalf("body %q", got)
	}
}

func TestDirectScheduledFailureNamesNoMessage(t *testing.T) {
	d := Direct{Kind: DirectScheduledFailed, ID: uuid.New(), Content: "private draft"}
	p := buildDirect(d, "de", true, 1)
	if p.Title != texts["de"]["scheduled_failed"] || p.Title == texts["en"]["scheduled_failed"] || strings.Contains(p.Body, "private") {
		t.Fatalf("%+v", p)
	}
}
