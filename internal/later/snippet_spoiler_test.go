package later

import (
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/mdplain"
)

// A reminder's push and the saved-items list show the message cut to
// snippetRunes first (loadRefs, cut) and stripped afterwards (push plainLine,
// web previewText). A spoiler that closes after the cut is open in the
// snippet, and its text shows. Finding H-L3-06 of the 2026-11 hunt; the web
// side is web/src/shared/lib/markdown/plainSpoiler.test.ts.
//
// knownSpoilerLeak asserts the WRONG behaviour (an inverse assertion): it
// fails the day the snippet hides the spoiler; then turn it into a plain check.
func knownSpoilerLeak(t *testing.T, content, secret string) {
	t.Helper()
	if !strings.Contains(mdplain.Plain(content, "Spoiler"), "Spoiler") || strings.Contains(mdplain.Plain(content, "Spoiler"), secret) {
		t.Fatalf("control: the whole message must hide %q", secret)
	}
	if got := mdplain.Plain(cut(content, snippetRunes), "Spoiler"); !strings.Contains(got, secret) {
		t.Errorf("the cut snippet hides the spoiler now (%q): H-L3-06 is fixed, make this a plain check", got)
	}
}

func TestACutSnippetShowsASpoilerThatClosesAfterTheCut(t *testing.T) {
	knownSpoilerLeak(t, strings.Repeat("x", 180)+" ||the code is 4711, do not tell||", "4711")
	knownSpoilerLeak(t, "**heads up** "+strings.Repeat("y ", 75)+"||ending: the butler did it, and the cook saw it||", "butler")
}
