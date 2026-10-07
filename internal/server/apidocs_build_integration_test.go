//go:build integration

package server

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A minimal frontend build may omit the optional API docs page. Compile that
// real embed variant without changing the workspace, then exercise the session
// protected route against its own isolated Postgres fixture.
func TestAPIDocsUnavailableInMinimalBuild(t *testing.T) {
	asset, err := filepath.Abs(filepath.Join("..", "..", "web", "dist", "api-docs.html"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(asset); os.IsNotExist(err) {
		t.Skip("current frontend build already omits API docs")
	} else if err != nil {
		t.Fatal(err)
	}
	fixtureDir := t.TempDir()
	overlay := filepath.Join(fixtureDir, "overlay.json")
	contents, err := json.Marshal(struct{ Replace map[string]string }{Replace: map[string]string{asset: ""}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(overlay, contents, 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(fixtureDir, "server-minimal.test")
	args := []string{"test", "-c", "-race", "-tags=integration", "-overlay=" + overlay, "-o", binary}
	if mode := testing.CoverMode(); mode != "" {
		args = append(args, "-cover", "-covermode="+mode, "-coverpkg=github.com/msch128/mnema-talk/internal/server")
	}
	args = append(args, ".")
	build := exec.Command("go", args...)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compile minimal embed fixture: %v\n%s", err, output)
	}
	// cmd/go sets GOCOVERDIR for subprocesses. The child contains exactly the
	// same Go source and counters. The native collector combines matching
	// package metadata; the overlay removes only a static HTML asset.
	childArgs := []string{"-test.run=^TestAPIDocsNeedASession$"}
	if dir := os.Getenv("GOCOVERDIR"); testing.CoverMode() != "" && dir != "" {
		childArgs = append(childArgs, "-test.gocoverdir="+dir)
	}
	child := exec.Command(binary, childArgs...)
	child.Env = os.Environ()
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("minimal embed fixture: %v\n%s", err, output)
	}
}
