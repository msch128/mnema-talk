package repo

// Import boundaries of the layered packages internal/chat and internal/auth.
//
// REVIEW NOTE: the tables below are the contract of the package split (see
// "Package size and layers" in AGENTS.md). Adding an edge, or a sub-package,
// is an architecture decision: say why in the pull request. A lower layer
// never imports a higher one and sibling packages of one layer never import
// each other; shared code moves down into core, or becomes an interface that
// the consumer defines and the root wires.

import (
	"encoding/json"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

const (
	modulePath = "github.com/msch128/mnema-talk/"
	chatRoot   = "internal/chat"
	authRoot   = "internal/auth"
)

// layered describes one package tree: which sub-packages may import which
// siblings, which of them the root may import, and who outside may import them.
type layered struct {
	root string
	// allowed maps a sub-package to the sibling sub-packages it may import.
	// A sub-package that exists on disk but is missing here fails the test.
	allowed map[string][]string
	// rootMayImport lists the sub-packages the root package itself may import
	// when rootRestricted; otherwise the root is the composition root and may
	// import all of them.
	rootMayImport  []string
	rootRestricted bool
	// subMayImportRoot lists the sub-packages that may import the root.
	subMayImportRoot []string
	// importers restricts who outside the tree may import a sub-package
	// (prefix match on the import path below the module); absent means free.
	importers map[string][]string
	// mustExist lists sub-packages that exist today, a guard for the walk.
	mustExist []string
}

// forbiddenForCore are the application layers a core package must not reach.
var forbiddenForCore = []string{
	"internal/server", "internal/ws", "internal/sfu", "internal/webhook", "internal/bot",
}

var trees = []layered{
	{
		root: chatRoot,
		allowed: map[string][]string{
			"chattest": {},
			"stage":    {},
			"core":     {},
			"blocks":   {"core", "mentions"},
			"mentions": {"core", "mentions"},
			// readstate resolves notify levels; nothing sideways.
			"readstate": {"core", "mentions"},
			"messages":  {"core", "mentions", "readstate", "blocks", "stage"},
			"channels":  {"core", "messages", "mentions", "readstate", "blocks", "stage"},
			"dm":        {"core", "messages", "mentions", "readstate", "blocks"},
			"search":    {"core", "messages", "mentions", "blocks"},
			"reports":   {"core", "messages", "blocks"},
			"forum":     {"core", "messages", "search", "mentions", "readstate", "blocks"},
			"saved":     {"core", "messages", "blocks"},
			// talks never imports dm: it takes a consumer-defined CallStarter.
			"talks":  {"core", "messages", "channels", "mentions", "stage"},
			"botapi": {"core", "messages", "channels", "mentions"},
			// account renders the userdata sections of every domain.
			"account": {"core", "messages", "channels", "dm", "reports"},
		},
		mustExist: []string{"blocks", "stage", "chattest", "core", "mentions", "readstate", "messages", "channels", "dm", "reports"},
		importers: map[string][]string{
			"botapi":  {"internal/server", "internal/chat"},
			"account": {"internal/server", "internal/chat"},
		},
	},
	{
		root: authRoot,
		allowed: map[string][]string{
			"memberadmin": {},
			"mailtext":    {},
			"account":     {},
		},
		rootRestricted:   true,
		rootMayImport:    []string{"mailtext"},
		subMayImportRoot: []string{"memberadmin", "account"},
		importers: map[string][]string{
			// slashcmd runs the same timeout, ban and kick actions as the routes.
			"memberadmin": {"internal/server", "internal/slashcmd", "internal/auth"},
			"account":     {"internal/server", "internal/auth"},
		},
	},
}

type listedPackage struct {
	ImportPath   string
	Imports      []string
	TestImports  []string
	XTestImports []string
}

// goList runs go list for the trees of the repository (integration tag
// included, so the integration tests are checked as well).
func goList(t *testing.T, root string, patterns ...string) []listedPackage {
	t.Helper()
	args := append([]string{"list", "-e", "-tags", "integration", "-json=ImportPath,Imports,TestImports,XTestImports"}, patterns...)
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var pkgs []listedPackage
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p listedPackage
		if err := dec.Decode(&p); err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

// rel turns an import path into a module-relative path ("" if outside).
func rel(importPath string) string {
	if r, ok := strings.CutPrefix(importPath, modulePath); ok {
		return r
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// subOf returns the first-level sub-package of tree below its root ("" for the
// root itself or for a path outside the tree).
func subOf(tree, path string) string {
	rest, ok := strings.CutPrefix(path, tree+"/")
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(rest, "/")
	return name
}

func inTree(tree, path string) bool { return path == tree || strings.HasPrefix(path, tree+"/") }

func TestPackageTreesKeepTheirLayerBoundaries(t *testing.T) {
	pkgs := goList(t, repoRoot(t), "./internal/...", "./cmd/...")
	for _, tree := range trees {
		t.Run(tree.root, func(t *testing.T) { checkTree(t, tree, pkgs) })
	}
}

func checkTree(t *testing.T, tree layered, pkgs []listedPackage) {
	t.Helper()
	seen := map[string]bool{}
	for _, p := range pkgs {
		from := rel(p.ImportPath)
		fromSub := subOf(tree.root, from)
		if inTree(tree.root, from) && fromSub != "" {
			seen[fromSub] = true
			if _, ok := tree.allowed[fromSub]; !ok {
				t.Errorf("%s: sub-package %q is not in the boundary table: give it a layer in internal/repo/boundaries_test.go", from, fromSub)
			}
		}
		prod := p.Imports
		tests := append(append([]string{}, p.TestImports...), p.XTestImports...)
		for _, imp := range prod {
			checkEdge(t, tree, from, fromSub, rel(imp), false)
		}
		for _, imp := range tests {
			checkEdge(t, tree, from, fromSub, rel(imp), true)
		}
	}
	// The walk must have seen the sub-packages that exist, so a path filter
	// cannot silently exempt a new directory from the table.
	for _, name := range tree.mustExist {
		if !seen[name] {
			t.Errorf("sub-package %s/%s not listed: go list patterns miss the tree", tree.root, name)
		}
	}
}

func checkEdge(t *testing.T, tree layered, from, fromSub, to string, testOnly bool) {
	t.Helper()
	if to == "" {
		return
	}
	if inTree(tree.root, to) {
		toSub := subOf(tree.root, to)
		switch {
		case !inTree(tree.root, from):
			checkImporter(t, tree, from, to, toSub)
		case fromSub == "" && toSub == "":
			// root to root: a test importing itself.
		case fromSub == "":
			if tree.rootRestricted && !contains(tree.rootMayImport, toSub) {
				t.Errorf("%s must not import %s", from, to)
			}
		case toSub == "":
			if !contains(tree.subMayImportRoot, fromSub) {
				t.Errorf("%s must not import the root package %s (shared code belongs in core, or behind an interface the consumer defines)", from, to)
			}
		case toSub == fromSub:
		case testOnly && toSub == "chattest":
		default:
			if !contains(tree.allowed[fromSub], toSub) {
				t.Errorf("%s must not import %s (allowed: %v)", from, to, tree.allowed[fromSub])
			}
		}
	}
}

// checkImporter enforces the importers allowlist for packages outside the tree.
func checkImporter(t *testing.T, tree layered, from, to, toSub string) {
	t.Helper()
	if toSub == "" {
		return
	}
	allowed, restricted := tree.importers[toSub]
	if !restricted {
		return
	}
	for _, prefix := range allowed {
		if inTree(prefix, from) {
			return
		}
	}
	t.Errorf("%s must not import %s: only %v may", from, to, allowed)
}

// TestChatCoreStaysFree makes sure the lowest chat layer pulls in no
// application layer, however indirectly.
func TestChatCoreStaysFree(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "list", "-e", "-deps", "./internal/chat/core")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		// Not created yet (the split adds it); nothing to check.
		t.Skip("internal/chat/core does not exist yet")
	}
	var bad []string
	for _, dep := range strings.Fields(string(out)) {
		r := rel(dep)
		if r == "" || r == chatRoot+"/core" {
			continue
		}
		if inTree(chatRoot, r) {
			bad = append(bad, r)
			continue
		}
		for _, f := range forbiddenForCore {
			if inTree(f, r) {
				bad = append(bad, r)
			}
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Errorf("internal/chat/core must stay below every other chat package and the application layers, but depends on %v", bad)
	}
}
