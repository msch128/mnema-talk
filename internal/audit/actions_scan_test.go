package audit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The scan below reads the server's source and lists every action an audit
// entry can be written with: the Action of each audit.Entry literal, followed
// back through variables, parameters, struct fields and function results to
// the string literals it comes from, plus the actions of raw INSERTs into
// audit_log. An action that cannot be followed back is an error, so a new
// writer cannot slip past the class table unseen. A value built as a literal
// plus something dynamic is listed as its prefix followed by "*".

// scanDepth bounds how far one value is followed.
const scanDepth = 10

type scanFunc struct {
	node ast.Node // *ast.FuncDecl or *ast.FuncLit
	typ  *ast.FuncType
	body *ast.BlockStmt
}

type scanFile struct {
	dir   string
	file  *ast.File
	funcs []scanFunc
}

type actionScan struct {
	fset  *token.FileSet
	files []*scanFile
	found map[string]string // action -> where it is written
	errs  []string
}

// scope is where an expression stands: its package directory and function.
type scope struct {
	dir string
	fn  *scanFunc
}

func newActionScan(t *testing.T, roots ...string) *actionScan {
	t.Helper()
	s := &actionScan{fset: token.NewFileSet(), found: map[string]string{}}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(s.fset, path, nil, 0)
			if err != nil {
				return err
			}
			s.files = append(s.files, indexFile(filepath.Dir(path), f))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func indexFile(dir string, f *ast.File) *scanFile {
	sf := &scanFile{dir: dir, file: f}
	ast.Inspect(f, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			if fn.Body != nil {
				sf.funcs = append(sf.funcs, scanFunc{fn, fn.Type, fn.Body})
			}
		case *ast.FuncLit:
			sf.funcs = append(sf.funcs, scanFunc{fn, fn.Type, fn.Body})
		}
		return true
	})
	return sf
}

// enclosing is the innermost function of f around pos.
func (f *scanFile) enclosing(pos token.Pos) *scanFunc {
	var best *scanFunc
	for i := range f.funcs {
		fn := &f.funcs[i]
		if fn.node.Pos() <= pos && pos < fn.node.End() && (best == nil || fn.node.Pos() > best.node.Pos()) {
			best = fn
		}
	}
	return best
}

// run collects the actions of every writer.
func (s *actionScan) run() {
	for _, f := range s.files {
		ast.Inspect(f.file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				s.entryLiteral(f, n)
			case *ast.BasicLit:
				s.rawInsert(f, n)
			case *ast.AssignStmt:
				s.fieldAssign(f, n)
			}
			return true
		})
	}
}

func (s *actionScan) entryLiteral(f *scanFile, lit *ast.CompositeLit) {
	if !isEntryType(f, lit.Type) {
		return
	}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok && identName(kv.Key) == "Action" {
			s.add(kv.Value, scope{f.dir, f.enclosing(kv.Pos())})
		}
	}
}

func isEntryType(f *scanFile, typ ast.Expr) bool {
	switch t := typ.(type) {
	case *ast.SelectorExpr:
		return identName(t.X) == "audit" && t.Sel.Name == "Entry"
	case *ast.Ident:
		return t.Name == "Entry" && filepath.Base(f.dir) == "audit"
	}
	return false
}

// fieldAssign handles e.Action = v in the files that use audit entries.
func (s *actionScan) fieldAssign(f *scanFile, as *ast.AssignStmt) {
	if !usesAudit(f) || len(as.Lhs) != len(as.Rhs) {
		return
	}
	for i, lhs := range as.Lhs {
		if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == "Action" {
			s.add(as.Rhs[i], scope{f.dir, f.enclosing(as.Pos())})
		}
	}
}

func usesAudit(f *scanFile) bool {
	if filepath.Base(f.dir) == "audit" {
		return true
	}
	for _, imp := range f.file.Imports {
		if strings.HasSuffix(imp.Path.Value, `/internal/audit"`) {
			return true
		}
	}
	return false
}

var insertAction = regexp.MustCompile(`(?s)INSERT INTO audit_log\b.*?VALUES\s*\(\s*'([^']+)'`)

// rawInsert lists the action of an INSERT into audit_log written as SQL; the
// writer's own INSERT (all values are parameters) is the one exception.
func (s *actionScan) rawInsert(f *scanFile, lit *ast.BasicLit) {
	if lit.Kind != token.STRING || !strings.Contains(lit.Value, "INSERT INTO audit_log") {
		return
	}
	if m := insertAction.FindStringSubmatch(lit.Value); m != nil {
		s.found[m[1]] = s.fset.Position(lit.Pos()).String()
	} else if !strings.Contains(lit.Value, "VALUES ($1") {
		s.errs = append(s.errs, s.fset.Position(lit.Pos()).String()+": an INSERT into audit_log whose action is not a literal")
	}
}

func (s *actionScan) add(e ast.Expr, sc scope) {
	vals, ok := s.values(e, sc, scanDepth)
	if !ok {
		s.errs = append(s.errs, s.fset.Position(e.Pos()).String()+": cannot tell which action this audit entry writes; build it from string literals")
		return
	}
	for _, v := range vals {
		if v != "" && v != "*" {
			s.found[v] = s.fset.Position(e.Pos()).String()
		}
	}
}

// values follows e back to the string literals it can hold.
func (s *actionScan) values(e ast.Expr, sc scope, depth int) ([]string, bool) {
	if depth == 0 {
		return nil, false
	}
	switch e := e.(type) {
	case *ast.BasicLit:
		v, err := strconv.Unquote(e.Value)
		return []string{v}, err == nil && e.Kind == token.STRING
	case *ast.ParenExpr:
		return s.values(e.X, sc, depth-1)
	case *ast.BinaryExpr:
		return s.concat(e, sc, depth-1)
	case *ast.Ident:
		return s.identValues(e, sc, depth-1)
	case *ast.SelectorExpr:
		return s.fieldValues(e.Sel.Name, sc.dir, depth-1)
	}
	return nil, false
}

// concat handles a + b; a dynamic right side leaves the prefix and "*".
func (s *actionScan) concat(e *ast.BinaryExpr, sc scope, depth int) ([]string, bool) {
	if e.Op != token.ADD {
		return nil, false
	}
	left, ok := s.values(e.X, sc, depth)
	if !ok {
		return nil, false
	}
	right, ok := s.values(e.Y, sc, depth)
	if !ok {
		right = []string{"*"}
	}
	var out []string
	for _, l := range left {
		for _, r := range right {
			out = append(out, l+r)
		}
	}
	return out, true
}

// identValues: the assignments to a local variable, the arguments of a
// parameter, or a package constant.
func (s *actionScan) identValues(id *ast.Ident, sc scope, depth int) ([]string, bool) {
	if sc.fn == nil {
		return s.constValues(id.Name, sc.dir, depth)
	}
	out, found, ok := s.assigned(id.Name, sc, depth)
	if !ok {
		return nil, false
	}
	if idx := paramIndex(sc.fn.typ, id.Name); idx >= 0 {
		args, ok := s.arguments(sc, idx, depth)
		return append(out, args...), ok
	}
	if found {
		return out, true
	}
	return s.constValues(id.Name, sc.dir, depth)
}

// assigned lists the values assigned to name in the function's own body.
func (s *actionScan) assigned(name string, sc scope, depth int) (out []string, found, ok bool) {
	ok = true
	ast.Inspect(sc.fn.body, func(n ast.Node) bool {
		as, isAssign := n.(*ast.AssignStmt)
		if !isAssign || !ok {
			return ok
		}
		for i, lhs := range as.Lhs {
			if identName(lhs) != name {
				continue
			}
			found = true
			vals, good := s.rhsValue(as, i, sc, depth)
			out, ok = append(out, vals...), good
		}
		return ok
	})
	return out, found, ok
}

func (s *actionScan) rhsValue(as *ast.AssignStmt, i int, sc scope, depth int) ([]string, bool) {
	if len(as.Lhs) == len(as.Rhs) {
		return s.values(as.Rhs[i], sc, depth)
	}
	if call, ok := as.Rhs[0].(*ast.CallExpr); ok && len(as.Rhs) == 1 {
		return s.results(call, i, sc.dir, depth)
	}
	return nil, false
}

func paramIndex(ft *ast.FuncType, name string) int {
	idx := 0
	for _, field := range ft.Params.List {
		for _, n := range field.Names {
			if n.Name == name {
				return idx
			}
			idx++
		}
		if len(field.Names) == 0 {
			idx++
		}
	}
	return -1
}

// arguments lists what the callers of the function pass at position idx.
func (s *actionScan) arguments(sc scope, idx, depth int) ([]string, bool) {
	decl, isDecl := sc.fn.node.(*ast.FuncDecl)
	if !isDecl {
		return nil, false
	}
	name, calls := decl.Name.Name, 0
	var out []string
	for _, f := range s.files {
		if f.dir != sc.dir && !ast.IsExported(name) {
			continue
		}
		ok := true
		ast.Inspect(f.file, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall || !ok || calledName(call.Fun) != name || idx >= len(call.Args) {
				return ok
			}
			calls++
			var vals []string
			vals, ok = s.values(call.Args[idx], scope{f.dir, f.enclosing(call.Pos())}, depth)
			out = append(out, vals...)
			return ok
		})
		if !ok {
			return nil, false
		}
	}
	return out, calls > 0
}

// results lists the idx-th result of the functions a call can reach: the
// functions of that name and the function literals stored in a field of it.
func (s *actionScan) results(call *ast.CallExpr, idx int, dir string, depth int) ([]string, bool) {
	name := calledName(call.Fun)
	var out []string
	targets := 0
	for _, f := range s.files {
		if f.dir != dir {
			continue
		}
		for i := range f.funcs {
			fn := &f.funcs[i]
			if !s.isTarget(f, fn, name) {
				continue
			}
			targets++
			vals, ok := s.returns(fn, idx, scope{f.dir, fn}, depth)
			if !ok {
				return nil, false
			}
			out = append(out, vals...)
		}
	}
	return out, targets > 0
}

// isTarget: a declared function called name, or a function literal that is
// the value of a field called name.
func (s *actionScan) isTarget(f *scanFile, fn *scanFunc, name string) bool {
	if decl, ok := fn.node.(*ast.FuncDecl); ok {
		return decl.Name.Name == name
	}
	hit := false
	ast.Inspect(f.file, func(n ast.Node) bool {
		if kv, ok := n.(*ast.KeyValueExpr); ok && kv.Value == fn.node && identName(kv.Key) == name {
			hit = true
		}
		return !hit
	})
	return hit
}

// returns lists the idx-th value of the function's own return statements.
func (s *actionScan) returns(fn *scanFunc, idx int, sc scope, depth int) ([]string, bool) {
	var out []string
	ok := true
	ast.Inspect(fn.body, func(n ast.Node) bool {
		if lit, nested := n.(*ast.FuncLit); nested && lit != fn.node {
			return false
		}
		ret, isRet := n.(*ast.ReturnStmt)
		if !isRet || !ok {
			return ok
		}
		if idx >= len(ret.Results) {
			ok = false
			return false
		}
		var vals []string
		vals, ok = s.values(ret.Results[idx], sc, depth)
		out = append(out, vals...)
		return ok
	})
	return out, ok
}

// fieldValues lists every value given to a field of that name in the package.
func (s *actionScan) fieldValues(name, dir string, depth int) ([]string, bool) {
	var out []string
	found, ok := false, true
	for _, f := range s.files {
		if f.dir != dir {
			continue
		}
		ast.Inspect(f.file, func(n ast.Node) bool {
			kv, isKV := n.(*ast.KeyValueExpr)
			if !isKV || !ok || identName(kv.Key) != name {
				return ok
			}
			found = true
			var vals []string
			vals, ok = s.values(kv.Value, scope{f.dir, f.enclosing(kv.Pos())}, depth)
			out = append(out, vals...)
			return ok
		})
	}
	return out, found && ok
}

// constValues lists a package-level constant or variable of that name.
func (s *actionScan) constValues(name, dir string, depth int) ([]string, bool) {
	for _, f := range s.files {
		if f.dir != dir {
			continue
		}
		for _, decl := range f.file.Decls {
			if vals, ok := s.specValues(decl, name, dir, depth); ok {
				return vals, true
			}
		}
	}
	return nil, false
}

func (s *actionScan) specValues(decl ast.Decl, name, dir string, depth int) ([]string, bool) {
	gen, ok := decl.(*ast.GenDecl)
	if !ok {
		return nil, false
	}
	for _, spec := range gen.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for i, n := range vs.Names {
			if n.Name == name && i < len(vs.Values) {
				return s.values(vs.Values[i], scope{dir: dir}, depth)
			}
		}
	}
	return nil, false
}

func identName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func calledName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}
