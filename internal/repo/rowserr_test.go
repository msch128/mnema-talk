package repo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestRowLoopsCheckTheirError holds every `for rows.Next()` loop to its
// contract: Next also returns false when the read broke off, so a loop that
// never asks rows.Err() takes a half-read result for the whole one. A loop
// is fine when its function calls rows.Err() on the same variable or hands the
// variable to a pgx collector (CollectRows, ForEachRow, ...), which checks.
func TestRowLoopsCheckTheirError(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
					for _, name := range uncheckedRowLoops(fn) {
						rel, _ := filepath.Rel(root, path)
						t.Errorf("%s: %s loops over %s.Next() but never checks %s.Err()", rel, fn.Name.Name, name, name)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// uncheckedRowLoops returns the variables of fn that are iterated with
// `for v.Next()` and neither checked with v.Err() nor given to a collector.
func uncheckedRowLoops(fn *ast.FuncDecl) []string {
	looped := map[string]bool{}
	checked := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			// A reflect.MapIter also has Next but no error to ask for.
			if len(n.Rhs) == 1 && len(n.Lhs) == 1 {
				if _, ok := receiverCall(n.Rhs[0], "MapRange"); ok {
					if id, ok := n.Lhs[0].(*ast.Ident); ok {
						checked[id.Name] = true
					}
				}
			}
		case *ast.ForStmt:
			if name, ok := receiverOf(n.Cond, "Next"); ok {
				looped[name] = true
			}
		case *ast.CallExpr:
			if name, ok := receiverOf(n, "Err"); ok {
				checked[name] = true
			}
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok && (strings.HasPrefix(sel.Sel.Name, "Collect") || sel.Sel.Name == "ForEachRow") {
				for _, arg := range n.Args {
					if id, ok := arg.(*ast.Ident); ok {
						checked[id.Name] = true
					}
				}
			}
		}
		return true
	})
	var out []string
	for name := range looped {
		if !checked[name] {
			out = append(out, name)
		}
	}
	return out
}

// receiverCall reports whether e is a call of the method, whatever its receiver.
func receiverCall(e ast.Expr, method string) (*ast.CallExpr, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return call, ok && sel.Sel.Name == method
}

// receiverOf reports v when e is the call v.method().
func receiverOf(e ast.Node, method string) (string, bool) {
	call, ok := e.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != method {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}
