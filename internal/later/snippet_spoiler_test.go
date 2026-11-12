package later

import (
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/mdplain"
)

// A reminder's push and the reminders list show the message's snippet
// (loadRefs) stripped by the reader (push plainLine, web previewText). The
// snippet used to be the Markdown cut to 200 characters, so a spoiler that
// closed after the cut was open in it and its text showed (finding H-L3-06 of
// the 2026-11 hunt; the web side is web/src/shared/lib/markdown/plainSpoiler.test.ts).
func snippetHides(t *testing.T, content, secret string) {
	t.Helper()
	if !strings.Contains(mdplain.Plain(content, "Spoiler"), "Spoiler") || strings.Contains(mdplain.Plain(content, "Spoiler"), secret) {
		t.Fatalf("control: the whole message must hide %q", secret)
	}
	if got := mdplain.Plain(mdplain.Snippet(content, mdplain.SnippetLen), "Spoiler"); strings.Contains(got, secret) || !strings.Contains(got, "Spoiler") {
		t.Errorf("the snippet shows the spoiler's text: %q", got)
	}
}

func TestASnippetHidesASpoilerThatClosesAfterTheCut(t *testing.T) {
	snippetHides(t, strings.Repeat("x", 180)+" ||the code is 4711, do not tell||", "4711")
	snippetHides(t, "**heads up** "+strings.Repeat("y ", 75)+"||ending: the butler did it, and the cook saw it||", "butler")
}
