package imageverify_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/msch128/mnema-talk/internal/imageverify"
)

// legacy is the provenance of a release built before the move to mnema-xyz.
var legacy = imageverify.LegacySource

func provenanceOf(source string) Provenance {
	return Provenance{Source: source, Builder: source + "/actions/runs/7/attempts/1"}
}

// pushBuiltBy pushes a release whose two platform images record source.
func pushBuiltBy(f *Fixture, tag, source string) Release {
	return f.Push(tag, provenanceOf(source), provenanceOf(source))
}

func TestVerifyAcceptsReleasesFromBeforeAndAfterTheMove(t *testing.T) {
	for label, tc := range map[string]struct{ identity, source string }{
		"mnema-xyz (after the move)": {Identity, imageverify.DefaultSource},
		"msch128 (before the move)":  {LegacyIdentity, legacy},
	} {
		t.Run(label, func(t *testing.T) {
			f := newFixture(t)
			rel := pushBuiltBy(f, "1.0", tc.source)
			f.Sign(rel, tc.identity, rel.Digest)
			res, err := verifyTag(f, "1.0")
			if err != nil {
				t.Fatalf("a release of %s must verify: %v", tc.source, err)
			}
			if res.Digest != rel.Digest.String() || res.Version != "1.1.0" {
				t.Fatalf("result = %+v", res)
			}
		})
	}
}

// The signature and the provenance must come from the same origin: one
// owner's signature never vouches for an image the other owner's build
// records, in either direction.
func TestVerifyRefusesAnOwnersSignatureOverTheOtherOwnersProvenance(t *testing.T) {
	t.Run("msch128 signature, mnema-xyz provenance", func(t *testing.T) {
		f := newFixture(t)
		rel := pushBuiltBy(f, "1.0", imageverify.DefaultSource)
		f.Sign(rel, LegacyIdentity, rel.Digest)
		expectUntrusted(t, f, "1.0", "as "+legacy+": image")
		expectUntrusted(t, f, "1.0", "as "+imageverify.DefaultSource+": no valid signature")
	})
	t.Run("mnema-xyz signature, msch128 provenance", func(t *testing.T) {
		f := newFixture(t)
		rel := pushBuiltBy(f, "1.0", legacy)
		f.Sign(rel, Identity, rel.Digest)
		expectUntrusted(t, f, "1.0", "as "+imageverify.DefaultSource+": image")
		expectUntrusted(t, f, "1.0", "as "+legacy+": no valid signature")
	})
	t.Run("msch128 builder, mnema-xyz source", func(t *testing.T) {
		f := newFixture(t)
		mixed := Provenance{Source: imageverify.DefaultSource, Builder: legacy + "/actions/runs/7/attempts/1"}
		rel := f.Push("1.0", mixed, mixed)
		f.Sign(rel, Identity, rel.Digest)
		f.Sign(rel, LegacyIdentity, rel.Digest)
		expectUntrusted(t, f, "1.0", "GitHub Actions run")
	})
	t.Run("mnema-xyz builder, msch128 source", func(t *testing.T) {
		f := newFixture(t)
		mixed := Provenance{Source: legacy, Builder: imageverify.DefaultSource + "/actions/runs/7/attempts/1"}
		rel := f.Push("1.0", mixed, mixed)
		f.Sign(rel, Identity, rel.Digest)
		f.Sign(rel, LegacyIdentity, rel.Digest)
		expectUntrusted(t, f, "1.0", "GitHub Actions run")
	})
}

func TestVerifyRefusesAThirdOwner(t *testing.T) {
	for _, owner := range []string{"mnemaxyz", "someone-else", "msch128-fork", "mnema-xyz-fork"} {
		t.Run(owner, func(t *testing.T) {
			f := newFixture(t)
			source := "https://github.com/" + owner + "/mnema-talk"
			rel := pushBuiltBy(f, "1.0", source)
			f.Sign(rel, "https://github.com/"+owner+"/mnema-talk/.github/workflows/release.yml@refs/heads/main", rel.Digest)
			expectUntrusted(t, f, "1.0", "no valid signature")
		})
	}
}

func TestRefusalNamesEveryOriginOnOneLine(t *testing.T) {
	f := newFixture(t)
	f.Push("1.0")
	_, err := verifyTag(f, "1.0")
	if err == nil {
		t.Fatal("an unsigned image verified")
	}
	msg := err.Error()
	if strings.Count(msg, "carries no signature") != 2 || strings.Contains(msg, "\n") ||
		strings.Count(msg, imageverify.ErrUntrusted.Error()) != 1 {
		t.Fatalf("refusal = %q, want both origins once each, one prefix, one line", msg)
	}
}

func TestMaxVersionCapsWhatAnOriginMayVouchFor(t *testing.T) {
	for label, tc := range map[string]struct {
		max  string
		want bool
	}{
		"no ceiling":                {"", true},
		"release at the ceiling":    {"1.1.0", true},
		"release above the ceiling": {"1.0.9", false},
	} {
		t.Run(label, func(t *testing.T) {
			f := newFixture(t)
			rel := pushBuiltBy(f, "1.0", legacy) // records version 1.1.0
			f.Sign(rel, LegacyIdentity, rel.Digest)
			p := f.Policy()
			p.Origins[1].MaxVersion = tc.max
			_, err := f.VerifierFor(p, f.Trust()).Verify(context.Background(), f.Repo+":1.0")
			if tc.want && err != nil {
				t.Fatalf("refused: %v", err)
			}
			if !tc.want && (!errors.Is(err, imageverify.ErrUntrusted) || !strings.Contains(err.Error(), "newer than the last release 1.0.9")) {
				t.Fatalf("err = %v, want a refusal above the ceiling", err)
			}
		})
	}
}

// failDigests fails every registry request that names one of the digests,
// like an outage while one particular manifest is read.
type failDigests struct {
	next    http.RoundTripper
	digests []string
}

func (f failDigests) RoundTrip(r *http.Request) (*http.Response, error) {
	for _, d := range f.digests {
		if strings.Contains(r.URL.Path, d) {
			return nil, errors.New("connection reset")
		}
	}
	return f.next.RoundTrip(r)
}

// The first origin refuses (another owner signed); the second could verify
// but its provenance cannot be read: that is no verdict, not "untrusted".
func TestARegistryFailureOnTheSecondOriginIsNoVerdict(t *testing.T) {
	f := newFixture(t)
	rel := pushBuiltBy(f, "1.0", legacy)
	f.Sign(rel, LegacyIdentity, rel.Digest)
	m, err := rel.Index.IndexManifest()
	if err != nil {
		t.Fatal(err)
	}
	var attestations []string
	for _, d := range m.Manifests {
		if d.Annotations["vnd.docker.reference.type"] == "attestation-manifest" {
			attestations = append(attestations, d.Digest.String())
		}
	}
	v := f.VerifierFor(f.Policy(), f.Trust(), imageverify.WithRemoteOptions(remote.WithTransport(failDigests{http.DefaultTransport, attestations})))
	_, err = v.Verify(context.Background(), f.Repo+":1.0")
	if err == nil || errors.Is(err, imageverify.ErrUntrusted) {
		t.Fatalf("err = %v, want a failure that is not a verdict on the image", err)
	}
}
