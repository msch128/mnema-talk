// Package imageverify checks, before a self-update swaps the app container,
// that the image to be pulled was built and signed by this repository's
// release workflow (SUP-05).
//
// What release.yml produces, and so what is verified (SECURITY.md,
// "Self-update sidecar"):
//
//   - A keyless cosign signature over the digest of the image index, stored as
//     a Sigstore bundle (v0.3, DSSE envelope, Fulcio certificate, Rekor entry
//     and RFC 3161 timestamp) in an OCI referrer of that digest. It is checked
//     offline against the Sigstore trusted root and pinned to the release
//     workflow's identity (see Policy).
//   - BuildKit SLSA v1 provenance, attached to the index as one attestation
//     manifest per platform image. BuildKit does not sign it itself; it is
//     covered by the signature because the signed index lists the attestation
//     manifests by digest. It is checked for the platform image it describes,
//     the repository it was built from and the GitHub Actions builder.
//
// Everything fails closed: any missing, malformed or mismatching piece is an
// error, and the caller must not swap.
package imageverify

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
)

// Defaults pin the identity of this repository's release workflow. They are
// what the published images carry: the certificate's subject is
// ".../release.yml@refs/heads/main" (the workflow_run trigger runs on main).
const (
	DefaultRepository = "ghcr.io/msch128/mnema-talk"
	// DefaultMirror is the Docker Hub copy of the same images, mirrored by
	// digest: same signature identity, same provenance.
	DefaultMirror   = "docker.io/msch128/mnema-talk"
	DefaultIssuer   = "https://token.actions.githubusercontent.com"
	DefaultSource   = "https://github.com/msch128/mnema-talk"
	DefaultIdentity = `^https://github\.com/msch128/mnema-talk/\.github/workflows/release\.yml@refs/heads/main$`
)

// Policy says which images are trusted.
type Policy struct {
	// Repository is the only image repository the updater may pull from,
	// e.g. "ghcr.io/msch128/mnema-talk".
	Repository string
	// Mirror is a second repository trusted under exactly the same identity
	// and source (empty = none). The default policy sets the Docker Hub copy;
	// overriding Repository drops it, so a third-party setup trusts one
	// repository only.
	Mirror string
	// Issuer is the exact OIDC issuer in the signing certificate.
	Issuer string
	// IdentityRegexp must match the certificate's subject alternative name
	// (the workflow URL and ref). It has to be anchored.
	IdentityRegexp string
	// Source is the repository URL BuildKit records as the build source
	// (vcs:source); the builder id must live under it as well.
	Source string
}

// DefaultPolicy is the policy for the official release images.
func DefaultPolicy() Policy {
	return Policy{Repository: DefaultRepository, Mirror: DefaultMirror, Issuer: DefaultIssuer, IdentityRegexp: DefaultIdentity, Source: DefaultSource}
}

// Validate refuses a policy that would trust too much (an empty or unanchored
// identity) instead of silently verifying nothing.
func (p Policy) Validate() error {
	if err := validateRepository(p.Repository); err != nil {
		return err
	}
	if p.Mirror != "" {
		if err := validateRepository(p.Mirror); err != nil {
			return err
		}
	}
	if p.Issuer == "" {
		return errors.New("the certificate issuer must be set")
	}
	if err := validateIdentity(p.IdentityRegexp); err != nil {
		return err
	}
	if !strings.HasPrefix(p.Source, "https://") {
		return errors.New("the source must be an https URL")
	}
	return nil
}

func validateRepository(repo string) error {
	// A port in the registry host is fine ("localhost:5000/x"); a tag or a
	// digest is not.
	lastSlash := strings.LastIndex(repo, "/")
	if lastSlash < 0 || strings.ContainsAny(repo, "@ ") || strings.Contains(repo[lastSlash:], ":") {
		return fmt.Errorf("repository %q is not an image repository without tag or digest", repo)
	}
	if _, err := name.NewRepository(repo); err != nil {
		return fmt.Errorf("repository %q: %w", repo, err)
	}
	return nil
}

// NormalizeRepository returns the canonical name of a repository, so that
// "msch128/mnema-talk", "docker.io/msch128/mnema-talk" and
// "index.docker.io/msch128/mnema-talk" are the same repository. ok is false
// for something that is not a repository name.
func NormalizeRepository(repo string) (string, bool) {
	r, err := name.NewRepository(repo)
	if err != nil {
		return "", false
	}
	return r.Name(), true
}

// DaemonRepository is the name to hand the Docker daemon for a verified
// repository: the canonical "docker.io/..." the daemon itself uses for Docker
// Hub, instead of the "index.docker.io/..." the registry client reports. Other
// registries are returned as they are.
func DaemonRepository(repo string) string {
	if rest, ok := strings.CutPrefix(repo, "index.docker.io/"); ok {
		return "docker.io/" + rest
	}
	return repo
}

// Allows reports whether repo (a normalised name) is one the policy trusts.
func (p Policy) Allows(repo string) bool {
	for _, allowed := range []string{p.Repository, p.Mirror} {
		if n, ok := NormalizeRepository(allowed); ok && allowed != "" && n == repo {
			return true
		}
	}
	return false
}

// validateIdentity requires the pattern to be anchored as a whole: a
// sequence that starts with ^ and ends with $. Checking the first and last
// character is not enough, because "^a|b$" is two alternatives, each
// anchored on one side only.
func validateIdentity(pattern string) error {
	re, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return fmt.Errorf("identity pattern: %w", err)
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("identity pattern: %w", err)
	}
	if re.Op != syntax.OpConcat || len(re.Sub) < 2 ||
		re.Sub[0].Op != syntax.OpBeginText || re.Sub[len(re.Sub)-1].Op != syntax.OpEndText {
		return errors.New("the identity pattern must be anchored as a whole with ^ and $ (wrap alternatives in a group)")
	}
	return nil
}
