// This file is a test fixture for imageverify and its users: an
// in-memory OCI registry plus a virtual Sigstore (own CA, Rekor log and
// timestamp authority), so a whole signed, tampered or unsigned release can be
// built without a network.

package imageverify_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/msch128/mnema-talk/internal/imageverify"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

const (
	// Identity is the certificate subject of the real release workflow.
	Identity = "https://github.com/msch128/mnema-talk/.github/workflows/release.yml@refs/heads/main"
	// OtherSource is a repository that is not the policy's.
	OtherSource = "https://github.com/someone-else/mnema-talk"

	bundleType = "application/vnd.dev.sigstore.bundle.v0.3+json"
	emptyType  = "application/vnd.oci.empty.v1+json"
	slsaType   = "https://slsa.dev/provenance/v1"
)

// Fixture is the registry and Sigstore of one test.
type Fixture struct {
	t        *testing.T
	sigstore *ca.VirtualSigstore
	// Host is the registry's host:port; Repo the image repository on it.
	Host, Repo string

	mu       sync.Mutex
	entities map[string]verify.SignedEntity
}

// newFixture starts the fixture; the registry stops with the test.
func newFixture(t *testing.T) *Fixture {
	t.Helper()
	vs, err := ca.NewVirtualSigstore()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	return &Fixture{t: t, sigstore: vs, Host: host, Repo: host + "/msch128/mnema-talk", entities: map[string]verify.SignedEntity{}}
}

// Policy is the default policy pointed at the fixture's repository.
func (f *Fixture) Policy() imageverify.Policy {
	p := imageverify.DefaultPolicy()
	p.Repository = f.Repo
	return p
}

// Trust returns the fixture's Sigstore as the trusted root.
func (f *Fixture) Trust() imageverify.TrustFunc {
	return func(context.Context) (root.TrustedMaterial, error) { return f.sigstore, nil }
}

// Verifier builds a Verifier for the fixture with its trust root and bundle
// decoder.
func (f *Fixture) Verifier() *imageverify.Verifier {
	f.t.Helper()
	return f.VerifierFor(f.Policy(), f.Trust())
}

// VerifierFor is Verifier with an explicit policy and trust source.
func (f *Fixture) VerifierFor(p imageverify.Policy, trust imageverify.TrustFunc, opts ...imageverify.Option) *imageverify.Verifier {
	f.t.Helper()
	v, err := imageverify.New(p, trust, append([]imageverify.Option{imageverify.WithBundleParser(f.parse)}, opts...)...)
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

// parse stands in for the Sigstore bundle decoder: the "bundle" layer holds
// the key of an entity the virtual Sigstore produced (it cannot serialise).
func (f *Fixture) parse(raw []byte) (verify.SignedEntity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entities[string(raw)]
	if !ok {
		return nil, errors.New("not a bundle")
	}
	return e, nil
}

// Release is one pushed image index.
type Release struct {
	Index  v1.ImageIndex
	Digest v1.Hash
}

// Provenance lets a test damage the provenance of one platform image.
type Provenance struct {
	Source  string // vcs:source, default the policy's
	Builder string // builder id, default an Actions run of the policy's source
	Subject string // subject digest hex, default the platform image's
	Drop    bool   // no provenance layer at all
	// NoAttestation leaves the platform image without an attestation manifest.
	NoAttestation bool
	// Raw replaces the statement with these bytes (a malformed one).
	Raw []byte
	// NoSource leaves the build source out of the statement.
	NoSource bool
	// Predicate overrides the statement's predicate type.
	Predicate string
}

func (f *Fixture) statement(img v1.Hash, o Provenance) []byte {
	if o.Source == "" {
		o.Source = imageverify.DefaultSource
	}
	if o.Builder == "" {
		o.Builder = imageverify.DefaultSource + "/actions/runs/1/attempts/1"
	}
	if o.Subject == "" {
		o.Subject = img.Hex
	}
	if o.Raw != nil {
		return o.Raw
	}
	predicate := slsaType
	if o.Predicate != "" {
		predicate = o.Predicate
	}
	args := map[string]string{"vcs:source": o.Source}
	vcs := map[string]string{"source": o.Source}
	if o.NoSource {
		args, vcs = map[string]string{}, map[string]string{}
	}
	st := map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"predicateType": predicate,
		"subject":       []any{map[string]any{"digest": map[string]string{"sha256": o.Subject}}},
		"predicate": map[string]any{
			"buildDefinition": map[string]any{"externalParameters": map[string]any{"request": map[string]any{"args": args}}},
			"runDetails": map[string]any{
				"builder":  map[string]any{"id": o.Builder},
				"metadata": map[string]any{"buildkit_metadata": map[string]any{"vcs": vcs}},
			},
		},
	}
	raw, err := json.Marshal(st)
	if err != nil {
		f.t.Fatal(err)
	}
	return raw
}

func (f *Fixture) attestation(img v1.Hash, o Provenance) v1.Image {
	base := mutate.MediaType(mutate.ConfigMediaType(empty.Image, types.MediaType(emptyType)), types.OCIManifestSchema1)
	a, err := mutate.Append(base, mutate.Addendum{
		Layer:       static.NewLayer([]byte(`{}`), "application/vnd.in-toto+json"),
		Annotations: map[string]string{"in-toto.io/predicate-type": "https://spdx.dev/Document"},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	if o.Drop {
		return a
	}
	a, err = mutate.Append(a, mutate.Addendum{
		Layer:       static.NewLayer(f.statement(img, o), "application/vnd.in-toto+json"),
		Annotations: map[string]string{"in-toto.io/predicate-type": slsaType},
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return a
}

// Push writes an index tagged tag with two platform images and an attestation
// for each, like BuildKit does. opts (one per platform, missing = clean) damage
// the provenance.
func (f *Fixture) Push(tag string, opts ...Provenance) Release {
	f.t.Helper()
	idx := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
	for i, arch := range []string{"amd64", "arm64"} {
		img, err := random.Image(512, 1)
		if err != nil {
			f.t.Fatal(err)
		}
		img = mutate.MediaType(img, types.OCIManifestSchema1)
		d, err := img.Digest()
		if err != nil {
			f.t.Fatal(err)
		}
		var o Provenance
		if i < len(opts) {
			o = opts[i]
		}
		idx = mutate.AppendManifests(idx, mutate.IndexAddendum{Add: img, Descriptor: v1.Descriptor{Platform: &v1.Platform{OS: "linux", Architecture: arch}}})
		if o.NoAttestation {
			continue
		}
		idx = mutate.AppendManifests(idx,
			mutate.IndexAddendum{Add: f.attestation(d, o), Descriptor: v1.Descriptor{
				Platform: &v1.Platform{OS: "unknown", Architecture: "unknown"},
				Annotations: map[string]string{
					"vnd.docker.reference.type":   "attestation-manifest",
					"vnd.docker.reference.digest": d.String(),
				},
			}},
		)
	}
	ref, err := name.ParseReference(f.Repo + ":" + tag)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := remote.WriteIndex(ref, idx); err != nil {
		f.t.Fatal(err)
	}
	d, err := idx.Digest()
	if err != nil {
		f.t.Fatal(err)
	}
	return Release{Index: idx, Digest: d}
}

// PushEmpty writes an index tagged tag that lists no image at all.
func (f *Fixture) PushEmpty(tag string) Release {
	f.t.Helper()
	idx := mutate.IndexMediaType(empty.Index, types.OCIImageIndex)
	ref, err := name.ParseReference(f.Repo + ":" + tag)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := remote.WriteIndex(ref, idx); err != nil {
		f.t.Fatal(err)
	}
	d, err := idx.Digest()
	if err != nil {
		f.t.Fatal(err)
	}
	return Release{Index: idx, Digest: d}
}

// Sign attaches a signature referrer to rel, made by identity (the OIDC issuer
// is GitHub Actions') over signedDigest, which is rel's own for an honest
// signature.
func (f *Fixture) Sign(rel Release, identity string, signedDigest v1.Hash) {
	f.t.Helper()
	body := fmt.Sprintf(`{"_type":"https://in-toto.io/Statement/v1","subject":[{"digest":{"sha256":%q}}],"predicateType":"https://sigstore.dev/cosign/sign/v1","predicate":{}}`, signedDigest.Hex)
	ent, err := f.sigstore.Attest(identity, imageverify.DefaultIssuer, []byte(body))
	if err != nil {
		f.t.Fatal(err)
	}
	f.mu.Lock()
	key := fmt.Sprintf("entity-%d-%s", len(f.entities), identity)
	f.entities[key] = ent
	f.mu.Unlock()

	f.attach(rel, []byte(key), bundleType)
}

// Attach adds a referrer to rel whose single layer has the given media type
// and content: garbage, or not a bundle at all.
func (f *Fixture) Attach(rel Release, content []byte, layerType string) {
	f.t.Helper()
	f.attach(rel, content, layerType)
}

// attach writes a referrer image of rel with one layer of the given type.
func (f *Fixture) attach(rel Release, content []byte, layerType string) {
	f.t.Helper()
	base := mutate.MediaType(mutate.ConfigMediaType(empty.Image, types.MediaType(bundleType)), types.OCIManifestSchema1)
	img, err := mutate.Append(base, mutate.Addendum{Layer: static.NewLayer(content, types.MediaType(layerType))})
	if err != nil {
		f.t.Fatal(err)
	}
	size, err := rel.Index.Size()
	if err != nil {
		f.t.Fatal(err)
	}
	signed, ok := mutate.Subject(img, v1.Descriptor{MediaType: types.OCIImageIndex, Digest: rel.Digest, Size: size}).(v1.Image)
	if !ok {
		f.t.Fatal("subject image")
	}
	d, err := signed.Digest()
	if err != nil {
		f.t.Fatal(err)
	}
	ref, err := name.NewDigest(f.Repo + "@" + d.String())
	if err != nil {
		f.t.Fatal(err)
	}
	if err := remote.Write(ref, signed); err != nil {
		f.t.Fatal(err)
	}
}
