// Package repo holds repository-wide checks that are not about one package:
// the size limits for Go source files and functions (see AGENTS.md).
package repo

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const (
	maxSourceLines = 400
	maxTestLines   = 800
	maxFuncLines   = 80
)

// funcLengthAllowlist lists functions that still exceed maxFuncLines. It only
// shrinks (entries are "directory function"): the test fails when an entry no longer needs to be listed.
// TODO: refactor these and delete the entries.
var funcLengthAllowlist = map[string]bool{}

var generatedHeader = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func goFiles(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "node_modules", "web", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !isGenerated(path) {
			rel, _ := filepath.Rel(root, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func isGenerated(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for i := 0; i < 20 && sc.Scan(); i++ {
		if generatedHeader.MatchString(sc.Text()) {
			return true
		}
	}
	return false
}

func lineCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

func TestGoFilesStayWithinSizeLimits(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range goFiles(t, root) {
		limit := maxSourceLines
		if strings.HasSuffix(rel, "_test.go") {
			limit = maxTestLines
		}
		if n := lineCount(t, filepath.Join(root, rel)); n > limit {
			t.Errorf("%s has %d lines, the limit is %d: split it by responsibility", rel, n, limit)
		}
	}
}

func TestGoFunctionsStayWithinLengthLimit(t *testing.T) {
	root := repoRoot(t)
	seen := map[string]bool{}
	for _, rel := range goFiles(t, root) {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			n := fset.Position(fn.End()).Line - fset.Position(fn.Pos()).Line + 1
			if n <= maxFuncLines {
				continue
			}
			key := filepath.ToSlash(filepath.Dir(rel)) + " " + fn.Name.Name
			seen[key] = true
			if !funcLengthAllowlist[key] {
				t.Errorf("%s is %d lines long, the limit is %d: extract helpers", key, n, maxFuncLines)
			}
		}
	}
	for key := range funcLengthAllowlist {
		if !seen[key] {
			t.Errorf("%s is within the limit now: remove it from funcLengthAllowlist", key)
		}
	}
}
