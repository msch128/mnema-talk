package registry

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	repoRoot   = "../../.."
	schemaPath = repoRoot + "/api/events.json"
)

// openAPISchemas reads which schemas api/openapi.json defines.
func openAPISchemas(t *testing.T) func(string) bool {
	t.Helper()
	raw, err := os.ReadFile(repoRoot + "/api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return func(name string) bool { _, ok := doc.Components.Schemas[name]; return ok }
}

func TestEventSchemaFileIsCurrent(t *testing.T) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(Schema(openAPISchemas(t))); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("UPDATE_EVENT_SCHEMA") != "" {
		if err := os.WriteFile(schemaPath, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatal("api/events.json is stale: an event type changed. Event payloads are public API (doc/api-stability.md); " +
			"for a compatible change run UPDATE_EVENT_SCHEMA=1 go test ./internal/events/registry and node web/scripts/generate-contracts.ts")
	}
}

// eventTypesInSource finds every type with an EventName method in the
// production code, as "pkg.Type".
func eventTypesInSource(t *testing.T) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	err := filepath.WalkDir(repoRoot+"/internal", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "EventName" || fn.Recv == nil {
				continue
			}
			recv := fn.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			found[file.Name.Name+"."+recv.(*ast.Ident).Name] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestEveryEventTypeIsRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, e := range All() {
		name := goName(reflect.TypeOf(e.Event))
		if registered[name] {
			t.Errorf("%s is registered twice", name)
		}
		registered[name] = true
	}
	inSource := eventTypesInSource(t)
	delete(inSource, "eventstest.named") // the stand-in event of tests
	for name := range inSource {
		if !registered[name] {
			t.Errorf("%s has an EventName method but is missing in registry.All", name)
		}
	}
	for name := range registered {
		if !inSource[name] {
			t.Errorf("registry.All lists %s, which no production file declares", name)
		}
	}
}

func TestEveryServerEventHasAType(t *testing.T) {
	raw, err := os.ReadFile(repoRoot + "/internal/events/testdata/events.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Server map[string]string `json:"server"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	typed := map[string]bool{}
	for _, e := range All() {
		typed[e.Event.EventName()] = true
	}
	for _, name := range golden.Server {
		if !typed[name] {
			t.Errorf("server event %q has no event type", name)
		}
		delete(typed, name)
	}
	for name := range typed {
		t.Errorf("an event type names %q, which is not in the event registry (internal/events/types.go)", name)
	}
}

// A payload that is a map of any, or an interface, has no shape a consumer
// could rely on: every field of every event type is a concrete type.
func TestNoEventTypeCarriesAnUntypedValue(t *testing.T) {
	for name, shapes := range Schema(func(string) bool { return false }).Events {
		for _, s := range shapes {
			if path := untyped(s.Type, s.GoType); path != "" {
				t.Errorf("%s: %s is untyped (a map of any, or an interface)", name, path)
			}
		}
	}
}

func untyped(n Node, path string) string {
	if n.Kind == "any" {
		return path
	}
	if n.Items != nil {
		if p := untyped(*n.Items, path+"[]"); p != "" {
			return p
		}
	}
	for _, f := range n.Fields {
		if p := untyped(f.Type, path+"."+f.Name); p != "" {
			return p
		}
	}
	return ""
}

// treeNode contains itself, like a component row that holds components: its
// description must end in a reference instead of recursing for ever.
type treeNode struct {
	Name     string     `json:"name"`
	Children []treeNode `json:"children"`
	Parent   *treeNode  `json:"parent"`
}

func TestATypeThatContainsItselfIsDescribedOnceAndThenReferred(t *testing.T) {
	d := describer{known: func(string) bool { return false }, open: map[reflect.Type]bool{}}
	n := d.node(reflect.TypeFor[treeNode](), "")
	if n.Kind != "object" || len(n.Fields) != 3 {
		t.Fatalf("node: %+v", n)
	}
	children, parent := n.Fields[1].Type, n.Fields[2].Type
	if children.Kind != "array" || children.Items.Kind != "ref" || children.Items.Ref != "registry.treeNode" {
		t.Fatalf("children: %+v", children)
	}
	if parent.Kind != "ref" || parent.Ref != "registry.treeNode" || !parent.Nullable {
		t.Fatalf("parent: %+v", parent)
	}
	if len(d.open) != 0 {
		t.Fatalf("open after describing: %v", d.open)
	}
}
