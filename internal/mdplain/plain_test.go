package mdplain

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

type fixture struct {
	Input string `json:"input"`
	Plain string `json:"plain"`
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// The same fixtures are checked by the web client (web/src/shared/lib/markdown/plainFixtures.test.ts),
// so a push, a notification and a reply preview read a message the same way.
func TestPlainAgreesWithTheSharedFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/plain.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []fixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		if got := collapse(Plain(f.Input, "Spoiler")); got != f.Plain {
			t.Errorf("Plain(%q) = %q, want %q", f.Input, got, f.Plain)
		}
	}
}

func TestPlainUsesTheGivenSpoilerWord(t *testing.T) {
	if got := Plain("a ||geheim|| b", "Spoiler"); got != "a Spoiler b" {
		t.Errorf("got %q", got)
	}
	if got := Plain("a ||geheim|| b", "Spoiler (de)"); got != "a Spoiler (de) b" {
		t.Errorf("got %q", got)
	}
}

func TestPlainNeverRevealsASpoiler(t *testing.T) {
	for _, in := range []string{"||x||", "**||x||**", "> ||x||", "# ||x||", "- ||x||", "[||x||](https://example.com)", "_||x||_"} {
		if got := Plain(in, "Spoiler"); strings.Contains(got, "x") {
			t.Errorf("Plain(%q) = %q leaks the hidden text", in, got)
		}
	}
}

// A link whose host only the browser can judge, with markup in it that the
// answer would read differently: everything between the first and the last
// "||" counts as hidden, the rest reads as usual.
func TestPlainFailsClosedWhereOnlyTheBrowserKnows(t *testing.T) {
	in := "before https://café.example/a_b ||one|| middle ||two|| after"
	got := Plain(in, "Spoiler")
	if strings.Contains(got, "one") || strings.Contains(got, "middle") || strings.Contains(got, "two") {
		t.Errorf("Plain(%q) = %q shows what may be hidden", in, got)
	}
	if !strings.HasPrefix(got, "before ") || !strings.HasSuffix(got, " after") || !strings.Contains(got, "Spoiler") {
		t.Errorf("Plain(%q) = %q, want the text around the spoilers", in, got)
	}
	// Without markup in the address both readings agree: no failing closed.
	if got := Plain("see https://café.example/x ||one|| middle ||two||", "Spoiler"); got != "see https://café.example/x Spoiler middle Spoiler" {
		t.Errorf("got %q", got)
	}
}

// Hostile input must not take quadratic time: thousands of unclosed delimiters.
func TestPlainStaysFastOnUnclosedDelimiters(t *testing.T) {
	start := time.Now()
	for _, unit := range []string{"*a ", "_a ", "~~a ", "||a ", "[a ", "`a "} {
		Plain(strings.Repeat(unit, 4000), "Spoiler")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("took %v", elapsed)
	}
}

func TestPlainDeepNestingStaysLiteralBeyondTheLimit(t *testing.T) {
	in := strings.Repeat("*", 10) + "x" + strings.Repeat("*", 10)
	if got := Plain(in, "Spoiler"); !strings.Contains(got, "x") {
		t.Errorf("got %q", got)
	}
}

func TestBareURLDoesNotAllocatePerLetterH(t *testing.T) {
	src := strings.Repeat("h ", 2000)
	if allocs := testing.AllocsPerRun(5, func() { _ = Plain(src, "spoiler") }); allocs > 200 {
		t.Fatalf("allocs per Plain = %v, want a small constant per message", allocs)
	}
}

func TestHasAtFoldIsCaseInsensitive(t *testing.T) {
	if !hasAtFold(encode("HtTpS://x"), 0, "https://") || hasAtFold(encode("http:/"), 0, "http://") || hasAtFold(encode("ftp://x"), 0, "http://") {
		t.Fatal("hasAtFold misjudged a prefix")
	}
}
