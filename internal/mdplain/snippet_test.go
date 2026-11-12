package mdplain

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type snippetFixture struct {
	Input   string `json:"input"`
	Limit   int    `json:"limit"`
	Snippet string `json:"snippet"`
	Plain   string `json:"plain"`
}

// The same fixtures are checked by the web client (web/src/shared/lib/markdown/snippet.test.ts).
func TestSnippetAgreesWithTheSharedFixtures(t *testing.T) {
	raw, err := os.ReadFile("testdata/snippet.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []snippetFixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		got := Snippet(f.Input, f.Limit)
		if got != f.Snippet {
			t.Errorf("Snippet(%q, %d) = %q, want %q", f.Input, f.Limit, got, f.Snippet)
		}
		if plain := collapse(Plain(got, "Spoiler")); plain != f.Plain {
			t.Errorf("Plain(Snippet(%q)) = %q, want %q", f.Input, plain, f.Plain)
		}
	}
}

// Whatever plain text is escaped reads back as itself, each spoiler as the reader's word.
func TestEscapedPlainTextReadsBackAsItself(t *testing.T) {
	pieces := []string{"a", "b1", "9", ".", " ", "#", ">", "-", "*", "_", "~", "|", "<", ">", "[", "]", "(", ")", "`", "\\", ":", "h", "https://x.example/a_b", "http://y.example", hiddenMark, "…", "é"}
	r := rand.New(rand.NewPCG(3, 3))
	for range 20000 {
		var b strings.Builder
		for n := r.IntN(14); n >= 0; n-- {
			b.WriteString(pieces[r.IntN(len(pieces))])
		}
		plain := collapse(b.String())
		want := strings.ReplaceAll(plain, hiddenMark, "Spoiler")
		if got := collapse(Plain(escapePlain(plain), "Spoiler")); got != want {
			t.Fatalf("plain %q escaped as %q reads back as %q", plain, escapePlain(plain), got)
		}
	}
}

func TestSnippetDropsThePrivateMarkOfTheMessage(t *testing.T) {
	in := strings.Repeat("w ", 120) + hiddenMark + " tail"
	if strings.Contains(Snippet(in, 200), "||") {
		t.Fatalf("a character of the message became a spoiler: %q", Snippet(in, 200))
	}
}

var canary = regexp.MustCompile(`zq\d+_\d+`)

var sweepPieces = []string{"word ", "a longer bit of text ", "**bold** ", "*it* ", "`code` ", "> ", "\n", "- ", "# ", "~~gone~~ ", "[label](https://a.example) ", "\\|| ", "| ", "`", "**", "[", strings.Repeat("x", 40) + " "}

// sweepMessage is the hunt's generator (web/src/shared/lib/markdown/plainSpoiler.test.ts)
// with some unclosed code, bold and link openers added.
func sweepMessage(r *rand.Rand, n int) string {
	var b strings.Builder
	k := 0
	for float64(b.Len()) < 60+r.Float64()*400 {
		if r.Float64() < 0.25 {
			fmt.Fprintf(&b, "||secret zq%d_%d more||", n, k)
			k++
			if r.Float64() < 0.5 {
				b.WriteByte(' ')
			}
			continue
		}
		b.WriteString(sweepPieces[r.IntN(len(sweepPieces))])
	}
	return b.String()
}

// A snippet never shows text the whole message hides. The deep run is
// PLAIN_SWEEP=1 [PLAIN_SWEEP_N=<n>] (100,000 by default).
func TestSnippetNeverShowsWhatTheWholeMessageHides(t *testing.T) {
	n := 2000
	if os.Getenv("PLAIN_SWEEP") != "" {
		n = 100000
		if v, err := strconv.Atoi(os.Getenv("PLAIN_SWEEP_N")); err == nil {
			n = v
		}
	}
	r := rand.New(rand.NewPCG(7, 7))
	cut := 0
	for i := range n {
		msg := sweepMessage(r, i)
		shown := map[string]bool{}
		for _, c := range canary.FindAllString(Plain(msg, "Spoiler"), -1) {
			shown[c] = true
		}
		snippet := Snippet(msg, 200)
		if snippet != msg {
			cut++
		}
		for _, c := range canary.FindAllString(Plain(snippet, "Spoiler"), -1) {
			if !shown[c] {
				t.Fatalf("message %d: the snippet %q shows %s, hidden in %q", i, snippet, c, msg)
			}
		}
	}
	t.Logf("snippets: %d messages, %d cut, 0 leaks", n, cut)
}

// Reply quotes and search roots now read the whole message (at most
// core.MaxMessageLen characters) to build the snippet.
func BenchmarkSnippetOfALongMessage(b *testing.B) {
	msg := strings.Repeat("**bold** `code` [label](https://a.example) ||hidden|| words and more ", 60)[:4000]
	for b.Loop() {
		_ = Snippet(msg, 200)
	}
}
