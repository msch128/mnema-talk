package memlimit

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(root, env string) (Result, int64, string) {
	var out bytes.Buffer
	var set int64
	res := apply(root, env, func(n int64) int64 { set = n; return 0 }, slog.New(slog.NewTextHandler(&out, nil)))
	return res, set, out.String()
}

func TestSetsEightyFivePercentOfTheCgroupV2Limit(t *testing.T) {
	root := t.TempDir()
	write(t, root, "memory.max", "1073741824\n")
	res, set, logged := run(root, "")
	want := int64(1073741824) / 100 * 85
	if res.Source != "cgroup v2" || res.Limit != want || set != want || res.Container != 1073741824 {
		t.Fatalf("result %+v, set %d, want %d", res, set, want)
	}
	if !strings.Contains(logged, "cgroup v2") || !strings.Contains(logged, "gomemlimit_bytes") {
		t.Fatalf("the choice was not logged: %s", logged)
	}
}

func TestFallsBackToCgroupV1(t *testing.T) {
	root := t.TempDir()
	write(t, root, "memory/memory.limit_in_bytes", "2147483648")
	res, set, _ := run(root, "")
	if res.Source != "cgroup v1" || set != int64(2147483648)/100*85 {
		t.Fatalf("result %+v, set %d", res, set)
	}
}

func TestNoLimitLeavesTheRuntimeAlone(t *testing.T) {
	cases := map[string]func(root string){
		"no files":      func(string) {},
		"v2 max":        func(root string) { write(t, root, "memory.max", "max\n") },
		"v2 garbage":    func(root string) { write(t, root, "memory.max", "lots") },
		"v2 zero":       func(root string) { write(t, root, "memory.max", "0") },
		"v1 unlimited":  func(root string) { write(t, root, "memory/memory.limit_in_bytes", "9223372036854771712") },
		"v1 unreadable": func(root string) { write(t, root, "memory/memory.limit_in_bytes", "") },
	}
	for name, prepare := range cases {
		root := t.TempDir()
		prepare(root)
		res, set, logged := run(root, "")
		if res != (Result{}) || set != 0 {
			t.Errorf("%s: result %+v, set %d", name, res, set)
		}
		if !strings.Contains(logged, "no cgroup limit") {
			t.Errorf("%s: not logged: %s", name, logged)
		}
	}
}

func TestAV2MaxWithAV1LimitFallsThrough(t *testing.T) {
	root := t.TempDir()
	write(t, root, "memory.max", "max")
	write(t, root, "memory/memory.limit_in_bytes", "536870912")
	if res, _, _ := run(root, ""); res.Source != "cgroup v1" {
		t.Fatalf("result %+v", res)
	}
}

func TestAnExplicitGOMEMLIMITWins(t *testing.T) {
	root := t.TempDir()
	write(t, root, "memory.max", "1073741824")
	res, set, logged := run(root, "600MiB")
	if res.Source != "env" || res.Limit != 0 || set != 0 {
		t.Fatalf("result %+v, set %d: the env limit was overridden", res, set)
	}
	if !strings.Contains(logged, "600MiB") {
		t.Fatalf("not logged: %s", logged)
	}
}

func TestApplyUsesTheProcessEnvironment(t *testing.T) {
	t.Setenv("GOMEMLIMIT", "1GiB")
	if res := Apply(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))); res.Source != "env" {
		t.Fatalf("result %+v", res)
	}
}
