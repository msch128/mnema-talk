package imageverify

import (
	"context"
	"fmt"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
)

// LiveTrust fetches the Sigstore trusted root through TUF (the public-good
// instance's root of trust is embedded in the library; updates are verified
// against it). cacheDir holds the TUF metadata cache; an empty value uses no
// cache at all (the container's file system is read-only and /tmp is RAM).
// If Sigstore cannot be reached the verification fails; nothing is skipped.
func LiveTrust(cacheDir string) TrustFunc { return LiveTrustAt(cacheDir, "") }

// LiveTrustAt is LiveTrust with another TUF mirror (an empty URL is the
// public-good instance's); for tests and private Sigstore deployments.
func LiveTrustAt(cacheDir, mirror string) TrustFunc {
	return func(ctx context.Context) (root.TrustedMaterial, error) {
		opts := tuf.DefaultOptions().WithContext(ctx)
		if mirror != "" {
			opts = opts.WithRepositoryBaseURL(mirror)
		}
		if cacheDir == "" {
			opts = opts.WithDisableLocalCache()
		} else {
			opts = opts.WithCachePath(cacheDir)
		}
		tr, err := root.FetchTrustedRootWithOptions(opts)
		if err != nil {
			return nil, fmt.Errorf("fetch the trusted root: %w", err)
		}
		return tr, nil
	}
}

// FileTrust reads a trusted_root.json (the format cosign and sigstore-go use)
// from disk, for installations that cannot reach the TUF mirror. The operator
// is then responsible for keeping it current.
func FileTrust(path string) TrustFunc {
	return func(context.Context) (root.TrustedMaterial, error) {
		tr, err := root.NewTrustedRootFromPath(path)
		if err != nil {
			return nil, fmt.Errorf("read trusted root %s: %w", path, err)
		}
		return tr, nil
	}
}
