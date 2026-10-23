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
	"strings"
)

// Defaults pin the identity of this repository's release workflow. They are
// what the published images carry: the certificate's subject is
// ".../release.yml@refs/heads/main" (the workflow_run trigger runs on main).
const (
	DefaultRepository = "ghcr.io/msch128/mnema-talk"
	DefaultIssuer     = "https://token.actions.githubusercontent.com"
	DefaultSource     = "https://github.com/msch128/mnema-talk"
	DefaultIdentity   = `^https://github\.com/msch128/mnema-talk/\.github/workflows/release\.yml@refs/heads/main$`
)

// Policy says which images are trusted.
type Policy struct {
	// Repository is the only image repository the updater may pull from,
	// e.g. "ghcr.io/msch128/mnema-talk".
	Repository string
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
	return Policy{Repository: DefaultRepository, Issuer: DefaultIssuer, IdentityRegexp: DefaultIdentity, Source: DefaultSource}
}

// Validate refuses a policy that would trust too much (an empty or unanchored
// identity) instead of silently verifying nothing.
func (p Policy) Validate() error {
	// A port in the registry host is fine ("localhost:5000/x"); a tag or a
	// digest is not.
	lastSlash := strings.LastIndex(p.Repository, "/")
	if lastSlash < 0 || strings.ContainsAny(p.Repository, "@ ") || strings.Contains(p.Repository[lastSlash:], ":") {
		return fmt.Errorf("repository %q is not an image repository without tag or digest", p.Repository)
	}
	if p.Issuer == "" {
		return errors.New("the certificate issuer must be set")
	}
	if !strings.HasPrefix(p.IdentityRegexp, "^") || !strings.HasSuffix(p.IdentityRegexp, "$") {
		return errors.New("the identity pattern must be anchored with ^ and $")
	}
	if _, err := regexp.Compile(p.IdentityRegexp); err != nil {
		return fmt.Errorf("identity pattern: %w", err)
	}
	if !strings.HasPrefix(p.Source, "https://") {
		return errors.New("the source must be an https URL")
	}
	return nil
}
