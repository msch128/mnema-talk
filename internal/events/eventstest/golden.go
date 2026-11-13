// Package eventstest pins the JSON of realtime events as the hub writes it.
// A test drives the real publishers, hands every frame to a Golden and
// compares the result with a file under testdata, byte for byte: key order,
// omitted fields and null included. Only what differs from run to run (IDs,
// timestamps) is replaced by a placeholder.
//
// The frames are public API (doc/api-stability.md). Regenerate a file with
// UPDATE_EVENT_GOLDEN=1 only for a deliberate, compatible addition.
package eventstest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/events"
)

var (
	uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	timePattern = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)`)
	// A message's number comes from a sequence that outlives a test database
	// reset; the account names of an incoming webhook and a bot end in random hex.
	numberPattern  = regexp.MustCompile(`"number":-?\d+`)
	webhookPattern = regexp.MustCompile(`"wh-[0-9a-f]{16}"`)
	botPattern     = regexp.MustCompile(`"bot-[0-9a-f]{12}"`)
)

// Normalize replaces what differs from run to run in a frame: IDs, timestamps,
// message numbers and the generated account names of incoming webhooks and bots. Every distinct ID
// gets its own number in order of appearance, so two fields that carry the
// same ID still show it.
func Normalize(frame []byte) string {
	seen := map[string]int{}
	out := uuidPattern.ReplaceAllStringFunc(string(frame), func(id string) string {
		n, ok := seen[id]
		if !ok {
			n = len(seen) + 1
			seen[id] = n
		}
		return fmt.Sprintf("<id-%d>", n)
	})
	out = timePattern.ReplaceAllString(out, "<time>")
	out = numberPattern.ReplaceAllString(out, `"number":<n>`)
	out = webhookPattern.ReplaceAllString(out, `"wh-<hex>"`)
	return botPattern.ReplaceAllString(out, `"bot-<hex>"`)
}

// Golden collects the frames of one test, grouped by the step that caused them.
type Golden struct {
	t    testing.TB
	path string
	out  strings.Builder
}

// Open starts a golden file testdata/<name>.golden.
func Open(t testing.TB, name string) *Golden {
	t.Helper()
	return &Golden{t: t, path: filepath.Join("testdata", name+".golden")}
}

// Step records what one action published: a heading, then one line per frame
// in the order given. site names the publishing function ("" when the frames
// were read off a connection).
func (g *Golden) Step(step string, frames ...Frame) {
	g.t.Helper()
	if len(frames) == 0 {
		g.t.Errorf("step %q published nothing", step)
	}
	fmt.Fprintf(&g.out, "# %s\n", step)
	for _, f := range frames {
		if f.Site != "" {
			fmt.Fprintf(&g.out, "@ %s\n", f.Site)
		}
		g.out.WriteString(Normalize(f.Wire))
		g.out.WriteByte('\n')
	}
	g.out.WriteByte('\n')
}

// Frame is one event as written to a connection and, when known, the function
// that published it.
type Frame struct {
	Site string
	Wire []byte
}

// Check compares the collected frames with the file, or writes the file when
// UPDATE_EVENT_GOLDEN is set.
func (g *Golden) Check() {
	g.t.Helper()
	got := g.out.String()
	if os.Getenv("UPDATE_EVENT_GOLDEN") != "" {
		if err := os.MkdirAll(filepath.Dir(g.path), 0o755); err != nil {
			g.t.Fatal(err)
		}
		if err := os.WriteFile(g.path, []byte(got), 0o644); err != nil {
			g.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(g.path)
	if err != nil {
		g.t.Fatalf("read golden file (UPDATE_EVENT_GOLDEN=1 writes it): %v", err)
	}
	if got == string(want) {
		return
	}
	g.t.Errorf("the JSON of a realtime event changed (%s). It is public API: key order, "+
		"omitted fields and null must stay as they are.\n%s", g.path, firstDifference(got, string(want)))
}

func firstDifference(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(g) || i < len(w); i++ {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return fmt.Sprintf("line %d\n got:  %s\n want: %s", i+1, gl, wl)
		}
	}
	return ""
}

// Named is an event with any name and payload, for a test that needs a frame
// the server itself never publishes (an unknown name, a payload that cannot be
// encoded) or that only cares that something was published.
func Named(name string, payload any) events.Event { return named{name: name, payload: payload} }

type named struct {
	name    string
	payload any
}

func (n named) EventName() string { return n.name }

func (n named) MarshalJSON() ([]byte, error) { return json.Marshal(n.payload) }
