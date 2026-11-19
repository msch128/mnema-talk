package repo

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// THIRD_PARTY_NOTICES.md lists the Go modules compiled into the binaries with
// their versions. This test keeps the tables equal to what the build uses:
// every row must name a compiled module at its go.mod version, and every
// compiled module must have a row. After a dependency bump, update the table
// (version column) and, for a new module, add a row with its license.
var noticeRow = regexp.MustCompile("^\\| `([^`]+)` \\| (v[^ |]+) \\|")

func compiledModules(t *testing.T, root string) map[string]string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}", "./cmd/...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	mods := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			mods[f[0]] = f[1]
		}
	}
	return mods
}

func noticeRows(t *testing.T, root string) map[string]string {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "THIRD_PARTY_NOTICES.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if m := noticeRow.FindStringSubmatch(sc.Text()); m != nil && strings.Contains(m[1], ".") && strings.Contains(m[1], "/") {
			rows[m[1]] = m[2]
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestThirdPartyNoticesMatchTheCompiledModules(t *testing.T) {
	root := repoRoot(t)
	mods, rows := compiledModules(t, root), noticeRows(t, root)
	var problems []string
	for path, ver := range rows {
		switch got, ok := mods[path]; {
		case !ok:
			problems = append(problems, path+" is listed but not compiled into a binary")
		case got != ver:
			problems = append(problems, path+" is listed at "+ver+", go.mod builds "+got)
		}
	}
	for path, ver := range mods {
		if _, ok := rows[path]; !ok {
			problems = append(problems, path+" "+ver+" is compiled in but not listed")
		}
	}
	sort.Strings(problems)
	if len(problems) > 0 {
		t.Fatalf("THIRD_PARTY_NOTICES.md drifted from go.mod:\n  %s", strings.Join(problems, "\n  "))
	}
}
