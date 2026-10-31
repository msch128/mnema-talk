package repo

import (
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

// httpx constructors whose message argument reaches the user.
var errorConstructors = map[string]int{
	"ErrInvalidInput": 0, "ErrUnauthorized": 0, "ErrForbidden": 0, "ErrNotFound": 0, "ErrConflict": 0,
	"ErrPayloadTooLarge": 0, "ErrUnsupportedMediaType": 0, "ErrUnavailable": 0, "ErrInternal": 0, "NewAPIError": 2,
}

// maxResolveDepth bounds how far a message is followed through helper calls.
const maxResolveDepth = 4

// funcInfo is one function of a package with the calls made to it.
type funcInfo struct {
	decl    *ast.FuncDecl
	params  map[string]int
	assigns map[string][]assignment // local variables and the values they are given
	calls   []callSite
}

type callSite struct {
	args []ast.Expr
	in   *funcInfo
	pkg  *pkgInfo
}

// pkgInfo collects what resolving a message needs from one package.
type pkgInfo struct {
	reg    registry
	name   string
	consts map[string]string
	funcs  map[string]*funcInfo
}

// registry maps a package's directory name to its info, so a call such as
// httpx.CleanText(...) finds the function it reaches.
type registry map[string]*pkgInfo

func newPkgInfo(reg registry, name string, files []*ast.File) *pkgInfo {
	p := &pkgInfo{reg: reg, name: name, consts: map[string]string{}, funcs: map[string]*funcInfo{}}
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				p.funcs[fd.Name.Name] = &funcInfo{decl: fd, params: paramIndex(fd), assigns: localAssigns(fd)}
			}
		}
	}
	for _, f := range files {
		for _, d := range f.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && (gd.Tok == token.CONST || gd.Tok == token.VAR) {
				p.collectConsts(gd)
			}
		}
	}
	return p
}

// linkCalls records, for every function of every package, the calls made to it.
func (reg registry) linkCalls() {
	for _, p := range reg {
		for _, fi := range p.funcs {
			fi := fi
			if fi.decl.Body == nil {
				continue
			}
			ast.Inspect(fi.decl.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if target := reg.target(p, call); target != nil {
						target.calls = append(target.calls, callSite{args: call.Args, in: fi, pkg: p})
					}
				}
				return true
			})
		}
	}
}

// target is the function a call reaches: a function of the same package, a
// method of any type in it, or a function of another package named by its qualifier.
func (reg registry) target(from *pkgInfo, call *ast.CallExpr) *funcInfo {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return from.funcs[fn.Name]
	case *ast.SelectorExpr:
		if id, ok := fn.X.(*ast.Ident); ok {
			if other := reg[id.Name]; other != nil && other != from {
				return other.funcs[fn.Sel.Name]
			}
		}
		if local := from.funcs[fn.Sel.Name]; local != nil {
			return local
		}
		return reg.uniqueMethod(fn.Sel.Name)
	}
	return nil
}

// uniqueMethod finds x.Name(...) when exactly one package declares a function
// of that name (a method reached through a field such as h.Perms).
func (reg registry) uniqueMethod(name string) *funcInfo {
	var found *funcInfo
	for _, p := range reg {
		if fi := p.funcs[name]; fi != nil {
			if found != nil {
				return nil
			}
			found = fi
		}
	}
	return found
}

func (p *pkgInfo) collectConsts(gd *ast.GenDecl) {
	for _, spec := range gd.Specs {
		vs := spec.(*ast.ValueSpec)
		for i, name := range vs.Names {
			if i < len(vs.Values) {
				if alts := p.resolve(vs.Values[i], nil, 0); len(alts) == 1 && !strings.Contains(alts[0], "%") {
					p.consts[name.Name] = alts[0]
				}
			}
		}
	}
}

// localAssigns lists the values assigned to each simple local variable.
type assignment struct {
	value ast.Expr
	add   bool // x += value
}

func localAssigns(fd *ast.FuncDecl) map[string][]assignment {
	out := map[string][]assignment{}
	if fd.Body == nil {
		return out
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok && len(as.Lhs) == len(as.Rhs) {
			for i, l := range as.Lhs {
				if id, ok := l.(*ast.Ident); ok {
					out[id.Name] = append(out[id.Name], assignment{value: as.Rhs[i], add: as.Tok == token.ADD_ASSIGN})
				}
			}
		}
		return true
	})
	return out
}

func paramIndex(fd *ast.FuncDecl) map[string]int {
	idx := map[string]int{}
	i := 0
	for _, field := range fd.Type.Params.List {
		for _, name := range field.Names {
			idx[name.Name] = i
			i++
		}
		if len(field.Names) == 0 {
			i++
		}
	}
	return idx
}

func calleeName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		return fn.Sel.Name
	}
	return ""
}

// resolve lists the texts an expression can take, with "%s" for any part that
// is not known at compile time. A parameter of the enclosing function takes the
// literal arguments of its call sites.
func (p *pkgInfo) resolve(e ast.Expr, in *funcInfo, depth int) []string {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			if s, err := strconv.Unquote(v.Value); err == nil {
				return []string{s}
			}
		}
	case *ast.ParenExpr:
		return p.resolve(v.X, in, depth)
	case *ast.SelectorExpr: // perm.TargetChannel
		if id, ok := v.X.(*ast.Ident); ok {
			if other := p.reg[id.Name]; other != nil {
				if s, ok := other.consts[v.Sel.Name]; ok {
					return []string{s}
				}
			}
		}
	case *ast.Ident:
		if s, ok := p.consts[v.Name]; ok {
			return []string{s}
		}
		if in != nil {
			if idx, ok := in.params[v.Name]; ok && depth < maxResolveDepth {
				return p.fromCallSites(in, idx, depth)
			}
			if values, ok := in.assigns[v.Name]; ok && depth < maxResolveDepth {
				return p.fromAssigns(values, in, depth)
			}
		}
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			return product(p.resolve(v.X, in, depth), p.resolve(v.Y, in, depth), func(a, b string) string { return a + b })
		}
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(v.Args) > 0 {
			return p.sprintf(v, in, depth)
		}
	}
	return []string{"%s"}
}

// fromAssigns is every text the variable can hold: each plain assignment, and
// those with any of the `+=` additions appended.
func (p *pkgInfo) fromAssigns(values []assignment, in *funcInfo, depth int) []string {
	var out []string
	for _, a := range values {
		if !a.add {
			out = append(out, p.resolve(a.value, in, depth+1)...)
		}
	}
	for _, a := range values {
		if a.add {
			out = append(out, product(out, p.resolve(a.value, in, depth+1), func(x, y string) string { return x + y })...)
		}
	}
	return out
}

func (p *pkgInfo) fromCallSites(in *funcInfo, idx, depth int) []string {
	var out []string
	for _, c := range in.calls {
		if idx >= len(c.args) {
			return []string{"%s"}
		}
		out = append(out, c.pkg.resolve(c.args[idx], c.in, depth+1)...)
	}
	if len(out) == 0 {
		return []string{"%s"}
	}
	return out
}

// sprintf fills the %s verbs whose argument is a known text and leaves the rest.
func (p *pkgInfo) sprintf(call *ast.CallExpr, in *funcInfo, depth int) []string {
	formats := p.resolve(call.Args[0], in, depth)
	var out []string
	for _, format := range formats {
		out = append(out, p.fillVerbs(format, call.Args[1:], in, depth)...)
	}
	return out
}

func (p *pkgInfo) fillVerbs(format string, args []ast.Expr, in *funcInfo, depth int) []string {
	parts := splitVerbs(format)
	results := []string{""}
	arg := 0
	for _, part := range parts {
		if !strings.HasPrefix(part, "%") || len(part) != 2 {
			results = product(results, []string{part}, func(a, b string) string { return a + b })
			continue
		}
		alts := []string{part}
		if part == "%s" && arg < len(args) {
			if got := p.resolve(args[arg], in, depth); !(len(got) == 1 && got[0] == "%s") {
				alts = got
			}
		}
		arg++
		results = product(results, alts, func(a, b string) string { return a + b })
	}
	return results
}

func splitVerbs(format string) []string {
	var parts []string
	last := 0
	for i := 0; i+1 < len(format); i++ {
		if format[i] == '%' && strings.ContainsRune("dsq", rune(format[i+1])) {
			if i > last {
				parts = append(parts, format[last:i])
			}
			parts = append(parts, format[i:i+2])
			last = i + 2
			i++
		}
	}
	if last < len(format) {
		parts = append(parts, format[last:])
	}
	return parts
}

func product(xs, ys []string, join func(a, b string) string) []string {
	out := make([]string, 0, len(xs)*len(ys))
	for _, x := range xs {
		for _, y := range ys {
			out = append(out, join(x, y))
		}
	}
	return out
}

// collectErrorMessages returns every message the server hands to an httpx
// error constructor, sorted and unique. A part that varies at run time stays
// as %d, %s or %q.
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
	reg := registry{}
	parsed := map[string][]*ast.File{}
	for dir, paths := range dirs {
		fset := token.NewFileSet()
		var files []*ast.File
		for _, p := range paths {
			f, err := parser.ParseFile(fset, p, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
		parsed[dir] = files
		reg[filepath.Base(dir)] = newPkgInfo(reg, filepath.Base(dir), files)
	}
	reg.linkCalls()
	for dir, files := range parsed {
		collectFromPackage(files, reg[filepath.Base(dir)], found)
	}
	out := make([]string, 0, len(found))
	for s := range found {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func collectFromPackage(files []*ast.File, pkg *pkgInfo, found map[string]bool) {
	for _, f := range files {
		for _, d := range f.Decls {
			var in *funcInfo
			if fd, ok := d.(*ast.FuncDecl); ok {
				in = pkg.funcs[fd.Name.Name]
			}
			ast.Inspect(d, func(n ast.Node) bool {
				for _, expr := range messageExprs(n, pkg) {
					for _, s := range pkg.resolve(expr, in, 0) {
						if strings.TrimSpace(s) != "" && strings.TrimSpace(s) != "%s" {
							found[s] = true
						}
					}
				}
				return true
			})
		}
	}
}

// messageExprs are the expressions in a node that become the text of an error
// body: the message argument of an httpx constructor, the Message of an
// APIError literal, and the format of the emoji package's image rejections
// (the handler passes that error text on).
func messageExprs(n ast.Node, pkg *pkgInfo) []ast.Expr {
	switch v := n.(type) {
	case *ast.CallExpr:
		if arg, ok := errorConstructors[calleeName(v)]; ok && len(v.Args) > arg && isHTTPXCall(v) {
			return []ast.Expr{v.Args[arg]}
		}
		if pkg.name == "emoji" && calleeName(v) == "reject" && len(v.Args) > 0 {
			return []ast.Expr{sprintfOf(v)}
		}
	case *ast.CompositeLit:
		if isAPIErrorType(v.Type) {
			for _, el := range v.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Message" {
						return []ast.Expr{kv.Value}
					}
				}
			}
		}
	}
	return nil
}

// sprintfOf rewrites reject("format", args...) as fmt.Sprintf("format", args...).
func sprintfOf(call *ast.CallExpr) ast.Expr {
	return &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent("fmt"), Sel: ast.NewIdent("Sprintf")}, Args: call.Args}
}

func isAPIErrorType(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == "APIError"
	case *ast.Ident:
		return t.Name == "APIError"
	}
	return false
}

// isHTTPXCall accepts httpx.ErrX(...) anywhere and a bare ErrX(...) inside httpx itself.
func isHTTPXCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		id, ok := fn.X.(*ast.Ident)
		return ok && id.Name == "httpx"
	case *ast.Ident:
		return true
	}
	return false
}
