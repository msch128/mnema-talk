package imageverify_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/msch128/mnema-talk/internal/imageverify"
	"github.com/sigstore/sigstore-go/pkg/root"
)

func verifyTag(f *Fixture, tag string) (*imageverify.Result, error) {
	return f.Verifier().Verify(context.Background(), f.Repo+":"+tag)
}

func expectUntrusted(t *testing.T, f *Fixture, tag, want string) {
	t.Helper()
	res, err := verifyTag(f, tag)
	if err == nil {
		t.Fatalf("verified %+v, want a refusal containing %q", res, want)
	}
	if !errors.Is(err, imageverify.ErrUntrusted) || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v, want ErrUntrusted containing %q", err, want)
	}
}

func TestVerifyAcceptsASignedImageWithProvenance(t *testing.T) {
	f := newFixture(t)
	rel := f.Push("1.0")
	f.Sign(rel, Identity, rel.Digest)

	res, err := verifyTag(f, "1.0")
	if err != nil {
		t.Fatalf("a correctly signed image must verify: %v", err)
	}
	if res.Digest != rel.Digest.String() || res.Ref() != f.Repo+"@"+rel.Digest.String() {
		t.Fatalf("result = %+v, want the signed digest %s", res, rel.Digest)
	}
	if len(res.Platforms) != 2 {
		t.Fatalf("platforms = %v, want both checked", res.Platforms)
	}
}

func TestVerifyRefusesWhatIsNotTrustworthy(t *testing.T) {
	t.Run("unsigned image", func(t *testing.T) {
		f := newFixture(t)
		f.Push("1.0")
		expectUntrusted(t, f, "1.0", "no signature")
	})
	t.Run("signature by another workflow", func(t *testing.T) {
		f := newFixture(t)
		rel := f.Push("1.0")
		f.Sign(rel, "https://github.com/msch128/mnema-talk/.github/workflows/ci.yml@refs/heads/main", rel.Digest)
		expectUntrusted(t, f, "1.0", "no valid signature")
	})
	t.Run("signature by a lookalike repository", func(t *testing.T) {
		f := newFixture(t)
		rel := f.Push("1.0")
		f.Sign(rel, "https://github.com/evil/mnema-talk/.github/workflows/release.yml@refs/heads/main", rel.Digest)
		expectUntrusted(t, f, "1.0", "no valid signature")
	})
	t.Run("signature on a pull request ref", func(t *testing.T) {
		f := newFixture(t)
		rel := f.Push("1.0")
		f.Sign(rel, "https://github.com/msch128/mnema-talk/.github/workflows/release.yml@refs/pull/9/merge", rel.Digest)
		expectUntrusted(t, f, "1.0", "no valid signature")
	})
	t.Run("signature copied from another image", func(t *testing.T) {
		f := newFixture(t)
		good := f.Push("1.0")
		other := f.Push("evil")
		// A valid signature of the good image, attached to the tampered one.
		f.Sign(other, Identity, good.Digest)
		expectUntrusted(t, f, "evil", "no valid signature")
	})
	t.Run("tag moved to an unsigned image", func(t *testing.T) {
		f := newFixture(t)
		rel := f.Push("1.0")
		f.Sign(rel, Identity, rel.Digest)
		if _, err := verifyTag(f, "1.0"); err != nil {
			t.Fatal(err)
		}
		f.Push("1.0") // the registry now serves a different, unsigned digest
		expectUntrusted(t, f, "1.0", "no signature")
	})
}

func TestVerifyChecksTheProvenance(t *testing.T) {
	cases := map[string]struct {
		opts Provenance
		want string
	}{
		"foreign source":       {Provenance{Source: OtherSource}, "built from"},
		"foreign builder":      {Provenance{Builder: "https://example.com/runner/1"}, "GitHub Actions run"},
		"builder of a fork":    {Provenance{Builder: OtherSource + "/actions/runs/1"}, "GitHub Actions run"},
		"another subject":      {Provenance{Subject: strings.Repeat("a", 64)}, "does not describe"},
		"no provenance at all": {Provenance{Drop: true}, "no SLSA provenance"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			f := newFixture(t)
			rel := f.Push("1.0", Provenance{}, tc.opts) // the second platform is damaged
			f.Sign(rel, Identity, rel.Digest)           // signed: only the provenance is wrong
			expectUntrusted(t, f, "1.0", tc.want)
		})
	}
}

func TestVerifyRefusesImagesOutsideThePolicy(t *testing.T) {
	f := newFixture(t)
	rel := f.Push("1.0")
	f.Sign(rel, Identity, rel.Digest)
	v := f.Verifier()

	for label, image := range map[string]string{
		"another repository": f.Host + "/other/app:1.0",
		"pinned by digest":   f.Repo + "@" + rel.Digest.String(),
		"garbage":            "not a reference!",
	} {
		if _, err := v.Verify(context.Background(), image); !errors.Is(err, imageverify.ErrUntrusted) {
			t.Errorf("%s: err = %v, want ErrUntrusted", label, err)
		}
	}
}

func TestVerifyFailsClosedWithoutTheTrustRootOrRegistry(t *testing.T) {
	f := newFixture(t)
	rel := f.Push("1.0")
	f.Sign(rel, Identity, rel.Digest)

	broken := func(context.Context) (root.TrustedMaterial, error) { return nil, errors.New("tuf mirror unreachable") }
	_, err := f.VerifierFor(f.Policy(), broken).Verify(context.Background(), f.Repo+":1.0")
	if err == nil || errors.Is(err, imageverify.ErrUntrusted) || !strings.Contains(err.Error(), "trusted root") {
		t.Fatalf("err = %v, want a trust root failure that is not a verdict on the image", err)
	}

	if _, err := verifyTag(f, "missing-tag"); err == nil {
		t.Fatal("an unknown tag must not verify")
	}
}

func TestPolicyValidation(t *testing.T) {
	good := imageverify.DefaultPolicy()
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	local := good
	local.Repository = "localhost:5000/msch128/mnema-talk"
	if err := local.Validate(); err != nil {
		t.Fatalf("a registry port is fine: %v", err)
	}
	noTrust := func(context.Context) (root.TrustedMaterial, error) { return nil, nil }
	for label, change := range map[string]func(*imageverify.Policy){
		"tagged repository":  func(p *imageverify.Policy) { p.Repository += ":1.0" },
		"digest repository":  func(p *imageverify.Policy) { p.Repository += "@sha256:abc" },
		"no namespace":       func(p *imageverify.Policy) { p.Repository = "mnema-talk" },
		"empty issuer":       func(p *imageverify.Policy) { p.Issuer = "" },
		"unanchored pattern": func(p *imageverify.Policy) { p.IdentityRegexp = "release.yml" },
		"top-level alternation": func(p *imageverify.Policy) {
			p.IdentityRegexp = `^https://github\.com/msch128/.*@refs/heads/main|.*evil$`
		},
		"multiline anchors": func(p *imageverify.Policy) { p.IdentityRegexp = `(?m)^release\.yml$` },
		"broken pattern":    func(p *imageverify.Policy) { p.IdentityRegexp = "^(+$" },
		"plain http source": func(p *imageverify.Policy) { p.Source = "http://example.com" },
	} {
		p := good
		change(&p)
		if err := p.Validate(); err == nil {
			t.Errorf("%s: policy accepted", label)
		}
		if _, err := imageverify.New(p, noTrust); err == nil {
			t.Errorf("%s: New accepted the policy", label)
		}
	}
	if _, err := imageverify.New(good, nil); err == nil {
		t.Error("New without a trust source must fail")
	}
}

func TestPolicyAcceptsAnchoredAlternationInAGroup(t *testing.T) {
	p := imageverify.DefaultPolicy()
	p.IdentityRegexp = `^(?:https://github\.com/msch128/a|https://github\.com/msch128/b)$`
	if err := p.Validate(); err != nil {
		t.Fatalf("a grouped alternation is anchored as a whole: %v", err)
	}
}

// failBlobs makes every blob download fail, like a registry outage while the
// signature bundle is read.
type failBlobs struct{ next http.RoundTripper }

func (f failBlobs) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.Contains(r.URL.Path, "/blobs/") {
		return nil, errors.New("connection reset")
	}
	return f.next.RoundTrip(r)
}

func TestVerifyReportsRegistryFailuresAsUnavailableNotUntrusted(t *testing.T) {
	f := newFixture(t)
	rel := f.Push("1.0")
	f.Sign(rel, Identity, rel.Digest)
	v := f.VerifierFor(f.Policy(), f.Trust(), imageverify.WithRemoteOptions(remote.WithTransport(failBlobs{http.DefaultTransport})))
	_, err := v.Verify(context.Background(), f.Repo+":1.0")
	if err == nil || errors.Is(err, imageverify.ErrUntrusted) {
		t.Fatalf("err = %v, want a failure that is not a verdict on the image", err)
	}
}

func TestVerifyKeepsTheRealSignatureFailureBehindUnrelatedReferrers(t *testing.T) {
	f := newFixture(t)
	rel := f.Push("1.0")
	f.Sign(rel, "https://github.com/msch128/mnema-talk/.github/workflows/ci.yml@refs/heads/main", rel.Digest)
	f.Attach(rel, []byte("{}"), "application/json")
	expectUntrusted(t, f, "1.0", "expected SAN value to match regex")
	_, err := verifyTag(f, "1.0")
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("the refusal must stay on one log line: %q", err)
	}
}
