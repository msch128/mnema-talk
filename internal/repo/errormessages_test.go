package repo

import (
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var updateErrorMessages = flag.Bool("update", false, "rewrite testdata/error_messages.golden.json")

// httpx constructors whose message argument reaches the user.
var errorConstructors = map[string]int{
	"ErrInvalidInput": 0, "ErrUnauthorized": 0, "ErrForbidden": 0, "ErrNotFound": 0, "ErrConflict": 0,
	"ErrPayloadTooLarge": 0, "ErrUnsupportedMediaType": 0, "ErrUnavailable": 0, "ErrInternal": 0, "NewAPIError": 2,
}

// messageTemplate renders a message expression as text where every part that
// is not known at compile time becomes "%s" (or the Sprintf verb). ok is false
// for a message that is entirely dynamic.
func messageTemplate(e ast.Expr, consts map[string]string) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		return s, err == nil
	case *ast.Ident:
		s, ok := consts[v.Name]
		return s, ok
	case *ast.ParenExpr:
		return messageTemplate(v.X, consts)
	case *ast.BinaryExpr:
		if v.Op != token.ADD {
			return "", false
		}
		l, lok := messageTemplate(v.X, consts)
		r, rok := messageTemplate(v.Y, consts)
		if !lok {
			l = "%s"
		}
		if !rok {
			r = "%s"
		}
		return l + r, lok || rok
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(v.Args) > 0 {
			return messageTemplate(v.Args[0], consts)
		}
	}
	return "", false
}

func packageConsts(files []*ast.File) map[string]string {
	consts := map[string]string{}
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if s, ok := messageTemplate(vs.Values[i], consts); ok {
							consts[name.Name] = s
						}
					}
				}
			}
		}
	}
	return consts
}

// collectErrorMessages returns every message template the server hands to an
// httpx error constructor, sorted and unique.
func collectErrorMessages(t *testing.T, root string) []string {
	t.Helper()
	found := map[string]bool{}
	dirs := map[string][]string{}
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			dirs[filepath.Dir(path)] = append(dirs[filepath.Dir(path)], path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, paths := range dirs {
		fset := token.NewFileSet()
		var files []*ast.File
		for _, p := range paths {
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
		consts := packageConsts(files)
		for _, f := range files {
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name := ""
				switch fn := call.Fun.(type) {
				case *ast.SelectorExpr:
					if id, ok := fn.X.(*ast.Ident); ok && id.Name == "httpx" {
						name = fn.Sel.Name
					}
				case *ast.Ident:
					name = fn.Name
				}
				arg, ok := errorConstructors[name]
				if !ok || len(call.Args) <= arg {
					return true
				}
				if s, ok := messageTemplate(call.Args[arg], consts); ok && strings.TrimSpace(s) != "" && strings.TrimSpace(s) != "%s" {
					found[s] = true
				}
				return true
			})
		}
	}
	out := make([]string, 0, len(found))
	for s := range found {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

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
