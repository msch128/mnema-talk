package imageverify_test

import (
	"context"
	"os"
	"testing"

	"github.com/msch128/mnema-talk/internal/imageverify"
)

func TestTrustSources(t *testing.T) {
	if _, err := imageverify.FileTrust("testdata/does-not-exist.json")(context.Background()); err == nil {
		t.Error("a missing trusted root file must be an error")
	}
	bad := t.TempDir() + "/root.json"
	if err := os.WriteFile(bad, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := imageverify.FileTrust(bad)(context.Background()); err == nil {
		t.Error("an empty trusted root must be an error")
	}

	// An unreachable Sigstore mirror is an error, not a pass.
	if _, err := imageverify.LiveTrustAt(t.TempDir(), "http://127.0.0.1:1")(context.Background()); err == nil {
		t.Error("an unreachable TUF mirror must be an error")
	}
	if _, err := imageverify.LiveTrustAt("", "http://127.0.0.1:1")(context.Background()); err == nil {
		t.Error("an unreachable TUF mirror must be an error without a cache too")
	}
}
