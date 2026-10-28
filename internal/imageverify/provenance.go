package imageverify

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

const (
	slsaPredicate     = "https://slsa.dev/provenance/v1"
	predicateAnnot    = "in-toto.io/predicate-type"
	versionArg        = "label:org.opencontainers.image.version"
	refTypeAnnot      = "vnd.docker.reference.type"
	refDigestAnnot    = "vnd.docker.reference.digest"
	attestationRefTyp = "attestation-manifest"
	// maxProvenanceBytes bounds one provenance statement (~50 KB in practice).
	maxProvenanceBytes = 4 << 20
	// maxPlatforms bounds the work for an index someone stuffed with entries.
	maxPlatforms = 16
)

// statement is the part of an in-toto SLSA v1 statement that BuildKit fills
// and that is checked.
type statement struct {
	PredicateType string `json:"predicateType"`
	Subject       []struct {
		Digest map[string]string `json:"digest"`
	} `json:"subject"`
	Predicate struct {
		BuildDefinition struct {
			ExternalParameters struct {
				Request struct {
					Args map[string]string `json:"args"`
				} `json:"request"`
			} `json:"externalParameters"`
		} `json:"buildDefinition"`
		RunDetails struct {
			Builder struct {
				ID string `json:"id"`
			} `json:"builder"`
			Metadata struct {
				Buildkit struct {
					VCS map[string]string `json:"vcs"`
				} `json:"buildkit_metadata"`
			} `json:"metadata"`
		} `json:"runDetails"`
	} `json:"predicate"`
}

// verifyProvenance requires, for every platform image of the (signed) index,
// an attestation manifest whose SLSA v1 provenance names that image as its
// subject, was produced by a GitHub Actions run of the policy's repository and
// was built from it. It returns the checked platforms.
func (v *Verifier) verifyProvenance(pinned name.Digest, opts []remote.Option) ([]string, string, error) {
	idx, err := remote.Index(pinned, opts...)
	if err != nil {
		return nil, "", fmt.Errorf("read image index: %w", err)
	}
	m, err := idx.IndexManifest()
	if err != nil {
		return nil, "", fmt.Errorf("read image index: %w", err)
	}
	attest := map[string]v1.Descriptor{}
	var images []v1.Descriptor
	for _, d := range m.Manifests {
		switch {
		case d.Annotations[refTypeAnnot] == attestationRefTyp:
			attest[d.Annotations[refDigestAnnot]] = d
		case d.Platform != nil && (d.MediaType == types.OCIManifestSchema1 || d.MediaType == types.DockerManifestSchema2):
			images = append(images, d)
		}
	}
	if len(images) == 0 {
		return nil, "", untrusted("the index has no platform image")
	}
	if len(images) > maxPlatforms {
		return nil, "", untrusted("the index lists %d platform images", len(images))
	}
	return v.checkPlatforms(pinned, images, attest, opts)
}

// checkPlatforms checks the provenance of each platform image and requires
// them all to record the same release version.
func (v *Verifier) checkPlatforms(pinned name.Digest, images []v1.Descriptor, attest map[string]v1.Descriptor, opts []remote.Option) ([]string, string, error) {
	platforms := make([]string, 0, len(images))
	version := ""
	for _, img := range images {
		a, ok := attest[img.Digest.String()]
		if !ok {
			return nil, "", untrusted("platform image %s has no provenance attestation", img.Digest)
		}
		got, err := v.checkAttestation(pinned.Repository.Digest(a.Digest.String()), img.Digest, opts)
		if err != nil {
			return nil, "", err
		}
		if version != "" && got != version {
			return nil, "", untrusted("platform images disagree on the version (%q, %q)", version, got)
		}
		version = got
		platforms = append(platforms, img.Platform.String())
	}
	return platforms, version, nil
}

func (v *Verifier) checkAttestation(ref name.Digest, image v1.Hash, opts []remote.Option) (string, error) {
	att, err := remote.Image(ref, opts...)
	if err != nil {
		return "", fmt.Errorf("fetch attestation %s: %w", ref.DigestStr(), err)
	}
	m, err := att.Manifest()
	if err != nil {
		return "", fmt.Errorf("read attestation %s: %w", ref.DigestStr(), err)
	}
	for _, l := range m.Layers {
		if l.Annotations[predicateAnnot] != slsaPredicate {
			continue
		}
		layer, err := att.LayerByDigest(l.Digest)
		if err != nil {
			return "", err
		}
		rc, err := layer.Compressed()
		if err != nil {
			return "", err
		}
		raw, err := io.ReadAll(io.LimitReader(rc, maxProvenanceBytes+1))
		rc.Close()
		if err != nil {
			return "", err
		}
		if len(raw) > maxProvenanceBytes {
			return "", untrusted("provenance of %s is too large", image)
		}
		return v.checkStatement(raw, image)
	}
	return "", untrusted("platform image %s has no SLSA provenance", image)
}

// checkStatement validates one provenance statement and returns the release
// version the build recorded (the version label BuildKit was given).
func (v *Verifier) checkStatement(raw []byte, image v1.Hash) (string, error) {
	var s statement
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", untrusted("provenance of %s is malformed", image)
	}
	if s.PredicateType != slsaPredicate {
		return "", untrusted("provenance of %s has predicate %q", image, s.PredicateType)
	}
	named := false
	for _, sub := range s.Subject {
		named = named || sub.Digest[image.Algorithm] == image.Hex
	}
	if !named {
		return "", untrusted("provenance does not describe image %s", image)
	}
	if builder := s.Predicate.RunDetails.Builder.ID; !strings.HasPrefix(builder, v.policy.Source+"/actions/runs/") {
		return "", untrusted("image %s was built by %q, not a GitHub Actions run of %s", image, builder, v.policy.Source)
	}
	if err := v.checkSource(&s, image); err != nil {
		return "", err
	}
	version := s.Predicate.BuildDefinition.ExternalParameters.Request.Args[versionArg]
	if version == "" {
		return "", untrusted("provenance of %s does not record the release version", image)
	}
	return version, nil
}

// checkSource compares the repository BuildKit recorded with the policy's.
// BuildKit writes it twice (request args and buildkit metadata); every
// recorded value must match and at least one must exist.
func (v *Verifier) checkSource(s *statement, image v1.Hash) error {
	got := []string{
		s.Predicate.BuildDefinition.ExternalParameters.Request.Args["vcs:source"],
		s.Predicate.RunDetails.Metadata.Buildkit.VCS["source"],
	}
	seen := false
	for _, src := range got {
		if src == "" {
			continue
		}
		seen = true
		if src != v.policy.Source {
			return untrusted("image %s was built from %q, not %s", image, src, v.policy.Source)
		}
	}
	if !seen {
		return untrusted("provenance of %s does not record its source", image)
	}
	return nil
}
