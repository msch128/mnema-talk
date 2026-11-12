package push

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"testing"
)

// The text of a message push shows nothing the web renderer hides: the
// corpus of generated messages the renderer judged (internal/mdplain/testdata/cross.jsonl)
// through the line a push carries (plainLine, then clean).
func TestPushTextHidesWhatTheRendererHides(t *testing.T) {
	f, err := os.Open("../mdplain/testdata/cross.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	canary := regexp.MustCompile(`zq\d+k\d+q`)
	lines := bufio.NewScanner(f)
	lines.Buffer(make([]byte, 1<<20), 1<<20)
	n := 0
	for lines.Scan() {
		var rec struct {
			Input  string   `json:"input"`
			Hidden []string `json:"hidden"`
		}
		if err := json.Unmarshal(lines.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		n++
		for _, locale := range []string{"en", "de"} {
			shown := map[string]bool{}
			for _, c := range canary.FindAllString(clean(plainLine(rec.Input, locale), 1<<20), -1) {
				shown[c] = true
			}
			for _, c := range rec.Hidden {
				if shown[c] {
					t.Fatalf("the push (%s) of %q shows %s, which the renderer hides", locale, rec.Input, c)
				}
			}
		}
	}
	if err := lines.Err(); err != nil || n == 0 {
		t.Fatalf("read %d records: %v", n, err)
	}
}
