package imageverify_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/msch128/mnema-talk/internal/imageverify"
)

func TestVerifyRefusesBrokenProvenance(t *testing.T) {
	cases := map[string]struct {
		opts Provenance
		want string
	}{
		"no attestation manifest":  {Provenance{NoAttestation: true}, "no provenance attestation"},
		"malformed statement":      {Provenance{Raw: []byte("{not json")}, "malformed"},
		"another predicate type":   {Provenance{Predicate: "https://example.com/other"}, "has predicate"},
		"no recorded source":       {Provenance{NoSource: true}, "does not record its source"},
		"oversized statement":      {Provenance{Raw: []byte(strings.Repeat(" ", 5<<20))}, "too large"},
		"statement without digest": {Provenance{Subject: "0"}, "does not describe"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			f := newFixture(t)
			rel := f.Push("1.0", tc.opts)
			f.Sign(rel, Identity, rel.Digest)
			expectUntrusted(t, f, "1.0", tc.want)
		})
	}
}

func TestVerifyRefusesAnIndexWithoutPlatformImages(t *testing.T) {
	f := newFixture(t)
	rel := f.PushEmpty("1.0")
	f.Sign(rel, Identity, rel.Digest)
	expectUntrusted(t, f, "1.0", "no platform image")
}

func TestVerifyDecodesRealBundlesAndRefusesGarbage(t *testing.T) {
	real, err := os.ReadFile("testdata/release-1.0.1-bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		raw   []byte
		layer string
		want  string
	}{
		// A genuine bundle, but of another digest and unknown to the test's
		// Sigstore: it decodes and is then refused, which is what it must be.
		"genuine bundle of another image": {real, "application/vnd.dev.sigstore.bundle.v0.3+json", "no valid signature"},
		"garbage as a bundle":             {[]byte("not a bundle"), "application/vnd.dev.sigstore.bundle.v0.3+json", "malformed signature bundle"},
		"referrer that is no bundle":      {[]byte("{}"), "application/json", "no bundle layer"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			f := newFixture(t)
			rel := f.Push("1.0")
			f.Attach(rel, tc.raw, tc.layer)
			// The default decoder, not the fixture's.
			v, err := imageverify.New(f.Policy(), f.Trust())
			if err != nil {
				t.Fatal(err)
			}
			_, err = v.Verify(context.Background(), f.Repo+":1.0")
			if !errors.Is(err, imageverify.ErrUntrusted) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want ErrUntrusted containing %q", err, tc.want)
			}
		})
	}
}

func TestVerifyGivesUpOnAnImageBuriedInReferrers(t *testing.T) {
	f := newFixture(t)
	rel := f.Push("1.0")
	for i := range 40 {
		f.Attach(rel, []byte{byte(i)}, "application/json")
	}
	if _, err := f.VerifierFor(f.Policy(), f.Trust()).Verify(context.Background(), f.Repo+":1.0"); !errors.Is(err, imageverify.ErrUntrusted) {
		t.Fatalf("err = %v, want a refusal rather than an unbounded search", err)
	}
}

func TestVerifierAcceptsRegistryOptions(t *testing.T) {
	f := newFixture(t)
	rel := f.Push("1.0")
	f.Sign(rel, Identity, rel.Digest)
	v := f.VerifierFor(f.Policy(), f.Trust(), imageverify.WithRemoteOptions(remote.WithUserAgent("mnema-test")))
	if _, err := v.Verify(context.Background(), f.Repo+":1.0"); err != nil {
		t.Fatal(err)
	}
}

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
