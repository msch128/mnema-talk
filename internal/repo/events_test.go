package repo

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// A realtime event is a named type (events.Event), so a publisher cannot send
// a map[string]any: the compiler refuses it. The one way around the types is
// eventstest.Named, the stand-in event of tests. Production code must never
// import that package. (That every event type is registered and has no
// untyped field is checked in internal/events/registry.)
func TestProductionCodeNeverPublishesAStandInEvent(t *testing.T) {
	const standIn = `"` + modulePath + `internal/events/eventstest"`
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(repoRoot(t), dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range file.Imports {
				if imp.Path.Value == standIn {
					t.Errorf("%s imports internal/events/eventstest: publish a named event type instead of an untyped payload", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
