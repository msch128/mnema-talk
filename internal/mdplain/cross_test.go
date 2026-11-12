package mdplain

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// crossRecord is one message of the corpus the web renderer judged
// (web/src/shared/lib/markdown/plainCross.test.ts writes it).
type crossRecord struct {
	Input   string   `json:"input"`
	Hidden  []string `json:"hidden"`
	Plain   string   `json:"plain"`
	Snippet string   `json:"snippet"`
}

var crossCanary = regexp.MustCompile(`zq\d+k\d+q`)

// crossLeaks are the canaries the renderer hides that text shows.
func crossLeaks(hidden []string, text string) []string {
	seen := map[string]bool{}
	for _, c := range crossCanary.FindAllString(text, -1) {
		seen[c] = true
	}
	var out []string
	for _, c := range hidden {
		if seen[c] {
			out = append(out, c)
		}
	}
	return out
}

type crossTally struct {
	messages, hidden, leaks, failClosed, plainDiffers, snippetDiffers int
}

func (c *crossTally) check(t *testing.T, rec crossRecord) {
	t.Helper()
	c.messages++
	c.hidden += len(rec.Hidden)
	p, _ := parse(strings.ReplaceAll(rec.Input, "\x00", ""), "Spoiler")
	doubt := p.doubt || p.unsure
	if p.doubt && strings.Contains(rec.Input, "||") {
		c.failClosed++
	}
	plain := collapseJS(Plain(rec.Input, "Spoiler"))
	snippet := Snippet(rec.Input, SnippetLen)
	if leaks := append(crossLeaks(rec.Hidden, plain), crossLeaks(rec.Hidden, Plain(snippet, "Spoiler"))...); len(leaks) > 0 {
		c.leaks++
		if c.leaks <= 3 {
			t.Errorf("shows %v, which the renderer hides, in %q", leaks, rec.Input)
		}
	}
	if !doubt && plain != rec.Plain {
		c.plainDiffers++
		if c.plainDiffers <= 3 {
			t.Errorf("plain form of %q:\n go  %q\n web %q", rec.Input, plain, rec.Plain)
		}
	}
	if !doubt && snippet != rec.Snippet {
		c.snippetDiffers++
		if c.snippetDiffers <= 3 {
			t.Errorf("snippet of %q:\n go  %q\n web %q", rec.Input, snippet, rec.Snippet)
		}
	}
}

func collapseJS(s string) string { return strings.Join(strings.FieldsFunc(s, jsSpaceRune), " ") }

// The plain form and the snippet show nothing the web renderer hides, and
// read every message the client reads with certainty exactly like it. The
// deep run reads files the web side wrote: CROSS_FILE=<file>[,<file>...].
func TestPlainHidesWhatTheRendererHides(t *testing.T) {
	files := []string{"testdata/cross.jsonl"}
	if env := os.Getenv("CROSS_FILE"); env != "" {
		files = strings.Split(env, ",")
	}
	var tally crossTally
	for _, name := range files {
		readCross(t, name, func(rec crossRecord) { tally.check(t, rec) })
	}
	t.Logf("cross (go): %+v", tally)
	if tally.messages == 0 {
		t.Fatal("no records")
	}
}

func readCross(t *testing.T, name string, each func(crossRecord)) {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	lines := bufio.NewScanner(f)
	lines.Buffer(make([]byte, 1<<20), 1<<20)
	for lines.Scan() {
		var rec crossRecord
		if err := json.Unmarshal(lines.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		each(rec)
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
}
