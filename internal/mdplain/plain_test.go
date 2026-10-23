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

// The same fixtures are checked by the web client (web/src/lib/markdown/plainFixtures.test.ts),
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
