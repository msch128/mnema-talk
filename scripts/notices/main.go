// Command notices rewrites the Go module tables of THIRD_PARTY_NOTICES.md from
// the build: versions follow go.mod, modules no binary compiles are dropped,
// and a new module gets a row with the license read from its LICENSE file.
// It fails, leaving the file alone, when a license is not recognised: a human
// adds that row. Run it with `make notices` from the repository root.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

const file = "THIRD_PARTY_NOTICES.md"

var row = regexp.MustCompile("^\\| `([^`]+)` \\| (v[^ |]+) \\| ([^|]+) \\|$")

func goOut(args ...string) (string, error) {
	out, err := exec.Command("go", args...).Output()
	return string(out), err
}

// compiled maps module path to version for the packages under pattern.
func compiled(pattern string) (map[string]string, error) {
	out, err := goOut("list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}}{{end}}{{end}}", pattern)
	if err != nil {
		return nil, fmt.Errorf("go list %s: %w", pattern, err)
	}
	mods := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			mods[f[0]] = f[1]
		}
	}
	return mods, nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "notices:", err)
		os.Exit(1)
	}
}

func run() error {
	server, err := compiled("./cmd/server")
	if err != nil {
		return err
	}
	all, err := compiled("./cmd/...")
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	lines := strings.Split(string(raw), "\n")
	listed := map[string]bool{}
	lines = refresh(lines, all, listed)
	var added []string
	for path := range all {
		if !listed[path] {
			added = append(added, path)
		}
	}
	sort.Strings(added)
	for _, path := range added {
		section := "## Updater binary"
		if _, ok := server[path]; ok || !hasSection(lines, section) {
			section = "## Server binary"
		}
		license, err := licenseOf(path)
		if err != nil {
			return err
		}
		lines = insert(lines, section, fmt.Sprintf("| `%s` | %s | %s |", path, all[path], license))
	}
	return os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0o644)
}

// refresh updates the version of every module row and drops rows of modules
// no binary compiles.
func refresh(lines []string, mods map[string]string, listed map[string]bool) []string {
	var out []string
	for _, l := range lines {
		m := row.FindStringSubmatch(l)
		if m == nil || !strings.Contains(m[1], "/") || !strings.Contains(m[1], ".") {
			out = append(out, l)
			continue
		}
		ver, ok := mods[m[1]]
		if !ok {
			continue
		}
		listed[m[1]] = true
		out = append(out, fmt.Sprintf("| `%s` | %s | %s |", m[1], ver, strings.TrimSpace(m[3])))
	}
	return out
}

func hasSection(lines []string, header string) bool {
	for _, l := range lines {
		if l == header {
			return true
		}
	}
	return false
}

// insert puts newRow into the module table under header, keeping the rows
// sorted by module path.
func insert(lines []string, header, newRow string) []string {
	in := false
	last := -1
	pos := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "## ") {
			in = l == header
			continue
		}
		if !in || !row.MatchString(l) {
			continue
		}
		last = i
		if pos < 0 && l > newRow {
			pos = i
		}
	}
	if pos < 0 {
		pos = last + 1
	}
	return append(lines[:pos], append([]string{newRow}, lines[pos:]...)...)
}
