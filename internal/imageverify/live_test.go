package imageverify

import (
	"context"
	"errors"
	"os"
	"testing"
)

// TestLiveReleaseImage verifies a real release image from GHCR against the
// public Sigstore instance. It needs network access, so it only runs when
// MNEMA_VERIFY_LIVE_IMAGE names an image, e.g.
//
//	MNEMA_VERIFY_LIVE_IMAGE=ghcr.io/msch128/mnema-talk:1.0 go test ./internal/imageverify -run Live
//
// It guards the assumption the whole check rests on: that what release.yml
// really publishes (cosign bundle referrer, BuildKit SLSA provenance) is what
// Verify reads.
func TestLiveReleaseImage(t *testing.T) {
	image := os.Getenv("MNEMA_VERIFY_LIVE_IMAGE")
	if image == "" {
		t.Skip("MNEMA_VERIFY_LIVE_IMAGE not set")
	}
	v, err := New(DefaultPolicy(), LiveTrust(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	res, err := v.Verify(context.Background(), image)
	if err != nil {
		t.Fatalf("the published release image must verify: %v", err)
	}
	t.Logf("verified %s (platforms %v)", res.Ref(), res.Platforms)

	// The same image under a stricter, wrong identity must be refused.
	p := DefaultPolicy()
	p.IdentityRegexp = `^https://github\.com/msch128/mnema-talk/\.github/workflows/ci\.yml@refs/heads/main$`
	strict, err := New(p, LiveTrust(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strict.Verify(context.Background(), image); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("another workflow identity must be refused, got %v", err)
	}
}
