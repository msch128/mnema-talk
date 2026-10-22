package events

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type registry struct {
	Server map[string]string `json:"server"`
	Client map[string]string `json:"client"`
}

// declared reads the constants of types.go, so a constant added, renamed or
// re-valued without touching the golden file fails the test.
func declared(t *testing.T) registry {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "types.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	reg := registry{Server: map[string]string{}, Client: map[string]string{}}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			value, err := strconv.Unquote(vs.Values[0].(*ast.BasicLit).Value)
			if err != nil {
				t.Fatal(err)
			}
			name := vs.Names[0].Name
			if strings.HasPrefix(name, "Client") {
				reg.Client[name] = value
			} else {
				reg.Server[name] = value
			}
		}
	}
	return reg
}

func TestEventRegistryMatchesGoldenFile(t *testing.T) {
	raw, err := os.ReadFile("testdata/events.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden registry
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	if got := declared(t); !reflect.DeepEqual(got, golden) {
		t.Errorf("the event registry changed. Event names are public API (doc/api-stability.md): "+
			"adding one is a minor change, renaming or removing one needs a major release.\nregistry: %v\ngolden:   %v", got, golden)
	}
}

func TestEventNamesAreUniquePerDirection(t *testing.T) {
	reg := declared(t)
	for direction, names := range map[string]map[string]string{"server": reg.Server, "client": reg.Client} {
		seen := map[string]string{}
		for constant, value := range names {
			if other, dup := seen[value]; dup {
				t.Errorf("%s event %q is declared twice (%s, %s)", direction, value, other, constant)
			}
			seen[value] = constant
		}
	}
}
