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
	"net/url"
	"regexp"
	"regexp/syntax"
	"slices"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/msch128/mnema-talk/internal/update"
)

// Defaults pin the identity of this repository's release workflow. They are
// what the published images carry: the certificate's subject is
// ".../release.yml@refs/heads/main" (the workflow_run trigger runs on main).
//
// The repository moves from the GitHub account msch128 to the organization
// mnema-xyz, where it is called "talk" (the image keeps its name
// mnema-talk). Releases built before the move are signed as msch128/mnema-talk
// and record that source; releases after it as mnema-xyz/talk. Both are trusted, each
// as one Origin: a signature of one owner never vouches for provenance of
// the other (see Origin).
const (
	DefaultRepository = "ghcr.io/mnema-xyz/mnema-talk"
	// LegacyRepository is where the images were published before the move.
	// GHCR does not redirect, so installs keep pulling from it until their
	// MNEMA_IMAGE names DefaultRepository.
	LegacyRepository = "ghcr.io/msch128/mnema-talk"
	// DefaultMirror is the Docker Hub copy of the same images, mirrored by
	// digest: same signature identity, same provenance. OrgMirror is the
	// copy under the organization's Docker Hub account (Docker Hub names
	// cannot contain a hyphen, hence mnemaxyz).
	DefaultMirror = "docker.io/msch128/mnema-talk"
	OrgMirror     = "docker.io/mnemaxyz/mnema-talk"
	DefaultIssuer = "https://token.actions.githubusercontent.com"
	DefaultSource = "https://github.com/mnema-xyz/talk"
	// DefaultIdentity is the release workflow of DefaultSource.
	DefaultIdentity = `^https://github\.com/mnema-xyz/talk/\.github/workflows/release\.yml@refs/heads/main$`
	// LegacySource and LegacyIdentity are the same before the move.
	LegacySource   = "https://github.com/msch128/mnema-talk"
	LegacyIdentity = `^https://github\.com/msch128/mnema-talk/\.github/workflows/release\.yml@refs/heads/main$`
)

// Origin is one trusted producer of images: the signing identity of its
// release workflow and the repository its builds record. An image is trusted
// only if one Origin matches both its signature and its provenance, so a
// signature of one owner cannot be combined with provenance of another.
type Origin struct {
	// IdentityRegexp must match the certificate's subject alternative name
	// (the workflow URL and ref). It has to be anchored.
	IdentityRegexp string
	// Source is the repository URL BuildKit records as the build source
	// (vcs:source); the builder id must live under it as well.
	Source string
	// MaxVersion, when set, is the newest release this origin may vouch for
	// (a release version such as "3.1.5"). The old owner gets one once its
	// last release is known, so a compromised or re-registered msch128
	// account cannot sign a newer release under the old name.
	MaxVersion string
}

// Policy says which images are trusted.
type Policy struct {
	// Repository is the primary image repository the updater may pull from,
	// e.g. "ghcr.io/mnema-xyz/mnema-talk".
	Repository string
	// Mirrors are further repositories trusted under exactly the same
	// origins (none = only Repository). The default policy sets the legacy
	// GHCR path and the Docker Hub copy; overriding Repository drops them,
	// so a third-party setup trusts one repository only.
	Mirrors []string
	// Issuer is the exact OIDC issuer in the signing certificate.
	Issuer string
	// Origins are the trusted producers, tried in order (at least one).
	Origins []Origin
}

// DefaultOrigins are the release workflow after and before the move.
func DefaultOrigins() []Origin {
	return []Origin{
		{IdentityRegexp: DefaultIdentity, Source: DefaultSource},
		{IdentityRegexp: LegacyIdentity, Source: LegacySource},
	}
}

// DefaultPolicy is the policy for the official release images.
func DefaultPolicy() Policy {
	return Policy{
		Repository: DefaultRepository,
		Mirrors:    []string{LegacyRepository, DefaultMirror, OrgMirror},
		Issuer:     DefaultIssuer,
		Origins:    DefaultOrigins(),
	}
}

// Validate refuses a policy that would trust too much (an empty or unanchored
// identity) instead of silently verifying nothing.
func (p Policy) Validate() error {
	if err := validateRepository(p.Repository); err != nil {
		return err
	}
	for _, m := range p.Mirrors {
		if err := validateRepository(m); err != nil {
			return err
		}
	}
	if p.Issuer == "" {
		return errors.New("the certificate issuer must be set")
	}
	if len(p.Origins) == 0 {
		return errors.New("at least one trusted origin (identity and source) must be set")
	}
	for _, o := range p.Origins {
		if err := validateIdentity(o.IdentityRegexp); err != nil {
			return err
		}
		if err := validateSource(o.Source); err != nil {
			return err
		}
		if _, ok := update.ParseSemver(o.MaxVersion); o.MaxVersion != "" && !ok {
			return fmt.Errorf("the newest release of origin %s must be a release version, not %q", o.Source, o.MaxVersion)
		}
	}
	return nil
}

// validateSource requires an https repository URL with a host and a path and
// without a trailing slash, query or fragment: it is compared exactly and is
// the prefix of the builder id.
func validateSource(source string) error {
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.Trim(u.Path, "/") == "" ||
		strings.HasSuffix(source, "/") || u.RawQuery != "" || u.Fragment != "" || strings.Contains(source, "?") || strings.Contains(source, "#") {
		return fmt.Errorf("the source must be an https repository URL like https://github.com/owner/repo, not %q", source)
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

// Trusted lists the normalised names of the repositories the policy trusts.
func (p Policy) Trusted() []string {
	var out []string
	for _, r := range append([]string{p.Repository}, p.Mirrors...) {
		if n, ok := NormalizeRepository(r); ok && r != "" {
			out = append(out, n)
		}
	}
	return out
}

// Allows reports whether repo (a normalised name) is one the policy trusts.
func (p Policy) Allows(repo string) bool {
	return slices.Contains(p.Trusted(), repo)
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
