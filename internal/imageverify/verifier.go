package imageverify

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// bundleType is the artifact and layer media type of a cosign signature
// stored as a Sigstore bundle in an OCI referrer (what cosign 3 writes).
const bundleType = "application/vnd.dev.sigstore.bundle.v0.3+json"

// emptyType is the artifact type GHCR's referrers index shows for it.
const emptyType = "application/vnd.oci.empty.v1+json"

const (
	// maxBundleBytes bounds one downloaded signature bundle (they are ~10 KB).
	maxBundleBytes = 1 << 20
	// maxBundles bounds how many referrer signatures are tried; anyone can
	// attach referrers to a digest, so the loop must not be unbounded.
	maxBundles = 32
)

// ErrUntrusted marks an image that was reached and examined but does not meet
// the policy: no signature, a signature by another identity or over another
// digest, or missing or foreign provenance. Any other error means the check
// could not be carried out (registry or trust root unreachable); the caller
// treats both as "do not swap".
var ErrUntrusted = errors.New("image is not trusted")

func untrusted(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUntrusted, fmt.Sprintf(format, args...))
}

// TrustFunc supplies the Sigstore trusted root (Fulcio and Rekor keys and
// the timestamp authority).
type TrustFunc func(ctx context.Context) (root.TrustedMaterial, error)

// Verifier checks images against a Policy.
type Verifier struct {
	policy      Policy
	trust       TrustFunc
	remoteOpts  []remote.Option
	parseBundle func([]byte) (verify.SignedEntity, error)
}

// Option customises a Verifier.
type Option func(*Verifier)

// WithRemoteOptions sets the registry client options (transport, auth).
func WithRemoteOptions(opts ...remote.Option) Option {
	return func(v *Verifier) { v.remoteOpts = append(v.remoteOpts, opts...) }
}

// WithBundleParser replaces the bundle decoder. For tests only (verifytest: it cannot
// produce serialised Sigstore bundles from a virtual Sigstore).
func WithBundleParser(f func([]byte) (verify.SignedEntity, error)) Option {
	return func(v *Verifier) { v.parseBundle = f }
}

// New returns a Verifier. trust is required: there is no way to verify
// without a trusted root, and no fallback that skips verification.
func New(policy Policy, trust TrustFunc, opts ...Option) (*Verifier, error) {
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("image policy: %w", err)
	}
	if trust == nil {
		return nil, errors.New("a trusted root source is required")
	}
	v := &Verifier{policy: policy, trust: trust, parseBundle: parseSigstoreBundle}
	for _, o := range opts {
		o(v)
	}
	return v, nil
}

func parseSigstoreBundle(raw []byte) (verify.SignedEntity, error) {
	var b bundle.Bundle
	if err := b.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	return &b, nil
}

// Result describes a verified image.
type Result struct {
	// Repository and Digest identify exactly what was verified; pull this
	// digest, never the tag again.
	Repository string
	Digest     string
	// Platforms lists the platform images whose provenance was checked.
	Platforms []string
	// Version is the release version the signed index's provenance records
	// (the same for every platform image). A valid signature says who built
	// the digest, not which release it is: the caller compares this with the
	// release it meant to install, so an older signed image is not accepted.
	Version string
}

// Ref is the pinned reference "repository@digest".
func (r *Result) Ref() string { return r.Repository + "@" + r.Digest }

// Verify resolves image (repository:tag) once, then checks signature and
// provenance of that exact digest. The repository must be the policy's.
func (v *Verifier) Verify(ctx context.Context, image string) (*Result, error) {
	ref, err := name.ParseReference(image)
	if err != nil {
		return nil, untrusted("image reference: %v", err)
	}
	repo := ref.Context()
	if repo.Name() != v.policy.Repository {
		return nil, untrusted("repository %s is not %s", repo.Name(), v.policy.Repository)
	}
	if _, pinned := ref.(name.Digest); pinned {
		return nil, untrusted("%s is pinned by digest; there is nothing to update", image)
	}
	opts := append([]remote.Option{remote.WithContext(ctx)}, v.remoteOpts...)
	desc, err := remote.Head(ref, opts...)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", image, err)
	}
	trust, err := v.trust(ctx)
	if err != nil {
		return nil, fmt.Errorf("sigstore trusted root: %w", err)
	}
	pinned := repo.Digest(desc.Digest.String())
	if err := v.verifySignature(pinned, desc.Digest, trust, opts); err != nil {
		return nil, err
	}
	platforms, version, err := v.verifyProvenance(pinned, opts)
	if err != nil {
		return nil, err
	}
	return &Result{Repository: repo.Name(), Digest: desc.Digest.String(), Platforms: platforms, Version: version}, nil
}

func (v *Verifier) verifySignature(pinned name.Digest, digest v1.Hash, trust root.TrustedMaterial, opts []remote.Option) error {
	referrers, err := remote.Referrers(pinned, opts...)
	if err != nil {
		return fmt.Errorf("list signatures of %s: %w", digest, err)
	}
	manifest, err := referrers.IndexManifest()
	if err != nil {
		return fmt.Errorf("read signatures of %s: %w", digest, err)
	}
	verifier, policy, err := v.sigstorePolicy(trust, digest)
	if err != nil {
		return err
	}
	last := v.tryBundles(pinned, manifest.Manifests, verifier, policy, opts)
	if last == nil {
		return nil
	}
	if errors.Is(last, errNoBundle) {
		return untrusted("%s carries no signature", digest)
	}
	return untrusted("no valid signature for %s by the release workflow: %v", digest, last)
}

var errNoBundle = errors.New("no signature bundle")

// tryBundles returns nil as soon as one referrer carries a signature that
// verifies, errNoBundle if there was nothing to try, else the last failure.
func (v *Verifier) tryBundles(pinned name.Digest, referrers []v1.Descriptor, verifier *verify.Verifier, policy verify.PolicyBuilder, opts []remote.Option) error {
	tried := 0
	last := errNoBundle
	for _, d := range referrers {
		// The referrers index of a registry without the referrers API (GHCR)
		// carries the config media type as artifact type, not the bundle
		// type, so the filter is the manifest's own: fetchBundle looks for
		// a bundle layer.
		if d.ArtifactType != "" && d.ArtifactType != bundleType && d.ArtifactType != emptyType {
			continue
		}
		if tried++; tried > maxBundles {
			break
		}
		if last = v.tryBundle(pinned.Repository.Digest(d.Digest.String()), verifier, policy, opts); last == nil {
			return nil
		}
	}
	return last
}

func (v *Verifier) tryBundle(ref name.Digest, verifier *verify.Verifier, policy verify.PolicyBuilder, opts []remote.Option) error {
	raw, err := v.fetchBundle(ref, opts)
	if err != nil {
		return err
	}
	entity, err := v.parseBundle(raw)
	if err != nil {
		return fmt.Errorf("malformed signature bundle: %w", err)
	}
	_, err = verifier.Verify(entity, policy)
	return err
}

func (v *Verifier) sigstorePolicy(trust root.TrustedMaterial, digest v1.Hash) (*verify.Verifier, verify.PolicyBuilder, error) {
	// A transparency log entry and a timestamp (log or RFC 3161) are both
	// required, as cosign does: the short-lived certificate is only valid
	// for the minutes around the signing.
	verifier, err := verify.NewVerifier(trust, verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1))
	if err != nil {
		return nil, verify.PolicyBuilder{}, fmt.Errorf("sigstore verifier: %w", err)
	}
	id, err := verify.NewShortCertificateIdentity(v.policy.Issuer, "", "", strings.TrimSpace(v.policy.IdentityRegexp))
	if err != nil {
		return nil, verify.PolicyBuilder{}, fmt.Errorf("certificate identity: %w", err)
	}
	sum, err := hex.DecodeString(digest.Hex)
	if err != nil || digest.Algorithm != "sha256" {
		return nil, verify.PolicyBuilder{}, untrusted("unsupported digest %s", digest)
	}
	return verifier, verify.NewPolicy(verify.WithArtifactDigest("sha256", sum), verify.WithCertificateIdentity(id)), nil
}

func (v *Verifier) fetchBundle(ref name.Digest, opts []remote.Option) ([]byte, error) {
	img, err := remote.Image(ref, opts...)
	if err != nil {
		return nil, fmt.Errorf("fetch signature %s: %w", ref.DigestStr(), err)
	}
	m, err := img.Manifest()
	if err != nil {
		return nil, err
	}
	for _, l := range m.Layers {
		if l.MediaType != bundleType {
			continue
		}
		layer, err := img.LayerByDigest(l.Digest)
		if err != nil {
			return nil, err
		}
		rc, err := layer.Compressed()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		raw, err := io.ReadAll(io.LimitReader(rc, maxBundleBytes+1))
		if err != nil {
			return nil, err
		}
		if len(raw) > maxBundleBytes {
			return nil, errors.New("signature bundle is too large")
		}
		return raw, nil
	}
	return nil, errors.New("signature artifact has no bundle layer")
}
