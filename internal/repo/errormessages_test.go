package repo

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateErrorMessages = flag.Bool("update", false, "rewrite testdata/error_messages.golden.json")

// TestErrorMessageCatalogue pins the sentences the server sends in error
// bodies. web/src/lib/errorDetails.ts must translate each of them (its test
// reads this file), so a new or reworded sentence fails there until the
// German text exists. Regenerate with: go test ./internal/repo -run ErrorMessageCatalogue -update
func TestErrorMessageCatalogue(t *testing.T) {
	root := repoRoot(t)
	got := collectErrorMessages(t, root)
	golden := filepath.Join(root, "internal", "repo", "testdata", "error_messages.golden.json")
	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if *updateErrorMessages {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(data) {
		t.Fatal("server error messages changed: run go test ./internal/repo -run ErrorMessageCatalogue -update, then translate the new ones in web/src/lib/errorDetails.ts")
	}
}

// openSubjects are the sentences whose subject is a name the code cannot know
// statically (a bot's option name, a field passed through several helpers).
// The web app answers them with a pattern that reads one token, so prose
// cannot be half translated. A new sentence of this shape needs a decision:
// resolve the subject at the call site, or list it here with the reason.
var openSubjects = map[string]string{
	"%s contains invalid characters":       "httpx.CleanText/CleanName: bot option names and rule fields reach it as data",
	"%s file is required":                  "media upload: the form field name is configuration",
	"%s is required":                       "httpx.CleanText: bot option names reach it as data",
	"%s is too long":                       "media/chat helpers: the field name is a parameter",
	"%s must be a message id":              "chat queryIDs: the query parameter names are map keys",
	"%s must be an RFC 3339 time":          "events filter: the query parameter name is a parameter",
	"%s must be an id":                     "chat queryIDs: the query parameter names are map keys",
	"%s must be at most %d characters":     "httpx.CleanText: bot option names reach it as data",
	"%s not found":                         "perm overwrites: the target kind comes from the route",
	"invalid %s":                           "httpx.PathUUID and media helpers: the parameter name is a parameter",
	"option %s must be %s":                 "bot commands: the option name is the bot author's",
	"option %s must be one of its choices": "bot commands: the option name is the bot author's",
}

func TestErrorMessageOpenSubjects(t *testing.T) {
	for _, message := range collectErrorMessages(t, repoRoot(t)) {
		open := strings.HasPrefix(message, "%s") || strings.HasPrefix(message, "invalid %s") || strings.HasPrefix(message, "option %s")
		if _, listed := openSubjects[message]; open && !listed {
			t.Errorf("%q has a subject the code does not name: resolve it at the call site or list it in openSubjects", message)
		}
		if _, listed := openSubjects[message]; listed && !open {
			t.Errorf("%q is listed in openSubjects but is not an open subject", message)
		}
	}
}
