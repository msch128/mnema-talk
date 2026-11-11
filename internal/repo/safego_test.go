package repo

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestGoroutinesAndTimersStartThroughSafego keeps the answer to a panic in
// one place. The server is one process: a panic on a goroutine or in a timer
// callback that nobody recovers ends all of it. Production code therefore
// starts both through internal/safego, where every site says what a panic
// does (close one connection or peer, or let the process exit) and where the
// panic is logged and counted. Anything else that starts a goroutine fails
// here: a bare `go` statement, time.AfterFunc, context.AfterFunc, a Go method
// (sync.WaitGroup.Go, errgroup.Group.Go) and a dot import of time or context,
// which would hide the two AfterFuncs. There is no allowlist.
func TestGoroutinesAndTimersStartThroughSafego(t *testing.T) {
	root := repoRoot(t)
	fset := token.NewFileSet()
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			if strings.HasPrefix(filepath.ToSlash(rel), "internal/safego/") {
				return nil
			}
			file, perr := parser.ParseFile(fset, path, nil, 0)
			if perr != nil {
				return perr
			}
			for _, found := range bareStarts(fset, file) {
				t.Errorf("%s:%s: use internal/safego instead", filepath.ToSlash(rel), found)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestBareStartsAreFound(t *testing.T) {
	const src = `package p

import (
	"context"
	"sync"
	"time"
	clock "time"

	"example.com/internal/safego"
)

func f() {
	go f()
	time.AfterFunc(time.Second, f)
	clock.AfterFunc(time.Second, f)
	start := time.AfterFunc
	_ = start
	time.Sleep(time.Second)
	context.AfterFunc(context.Background(), f)
	var wg sync.WaitGroup
	wg.Go(f)
	safego.Go("x", safego.Crash, f)
	safego.AfterFunc("x", time.Second, safego.Crash, f)
}
`
	want := []string{"13: go statement", "14: time.AfterFunc", "15: time.AfterFunc", "16: time.AfterFunc", "19: context.AfterFunc", "21: Go method"}
	if got := bareStartsIn(t, src); strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Fatalf("found %v, want %v", got, want)
	}

	const dotted = `package p

import (
	. "context"
	. "time"
)

func f() { AfterFunc(Second, f) }
`
	want = []string{"4: dot import of context", "5: dot import of time"}
	if got := bareStartsIn(t, dotted); strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Fatalf("found %v, want %v", got, want)
	}
}

func bareStartsIn(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	return bareStarts(fset, file)
}

// bareStarts lists what starts a goroutine or a timer in file without
// internal/safego, each as "line: what".
func bareStarts(fset *token.FileSet, file *ast.File) []string {
	var out []string
	found := func(n ast.Node, what string) {
		out = append(out, strconv.Itoa(fset.Position(n.Pos()).Line)+": "+what)
	}
	// The names under which the file knows time, context and safego.
	names := map[string]string{}
	for _, imp := range file.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		pkg := path[strings.LastIndex(path, "/")+1:]
		if pkg != "time" && pkg != "context" && pkg != "safego" {
			continue
		}
		switch {
		case imp.Name == nil:
			names[pkg] = pkg
		case imp.Name.Name == ".":
			found(imp, "dot import of "+pkg)
		default:
			names[imp.Name.Name] = pkg
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.GoStmt:
			found(n, "go statement")
		case *ast.SelectorExpr:
			pkg := ""
			if x, ok := n.X.(*ast.Ident); ok {
				pkg = names[x.Name]
			}
			switch {
			case n.Sel.Name == "AfterFunc" && (pkg == "time" || pkg == "context"):
				found(n, pkg+".AfterFunc")
			case n.Sel.Name == "Go" && pkg != "safego":
				found(n, "Go method")
			}
		}
		return true
	})
	return out
}
