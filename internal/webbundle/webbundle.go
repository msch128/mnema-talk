// Package webbundle builds, signs and verifies the web app bundle that the
// desktop client ships instead of loading JavaScript from the server (see
// doc/desktop-trusted-client.md). A bundle is one zip: manifest.json (file
// hashes, version and compatibility numbers), manifest.sig (Ed25519 over the
// exact manifest bytes) and app/<path> for every file of web/dist.
//
// The desktop host implements the same verification in Rust; the limits and
// path rules below are part of the format and must stay in step with it.
package webbundle

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	// Schema is the manifest schema number.
	Schema = 1
	// Name identifies the kind of bundle in the manifest.
	Name = "mnema-web"

	manifestName = "manifest.json"
	sigName      = "manifest.sig"
	appPrefix    = "app/"

	// Limits shared with the desktop verifier.
	MaxEntries      = 4000
	MaxFileSize     = 64 << 20
	MaxTotalSize    = 256 << 20
	maxManifestSize = 4 << 20
	maxPathLen      = 512
)

// File is one manifest entry.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Manifest is the signed description of a bundle.
type Manifest struct {
	Schema           int    `json:"schema"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	WebClientAPI     int    `json:"web_client_api"`
	MinServerVersion string `json:"min_server_version"`
	SecurityFloor    string `json:"security_floor"`
	Commit           string `json:"commit"`
	KeyID            string `json:"key_id"`
	Files            []File `json:"files"`
}

// Compat is web/desktop-compat.json: the compatibility numbers a release
// declares for its bundle.
type Compat struct {
	WebClientAPI     int    `json:"web_client_api"`
	MinServerVersion string `json:"min_server_version"`
	SecurityFloor    string `json:"security_floor"`
}

// Validate checks the numbers a release may declare.
func (c Compat) Validate() error {
	if c.WebClientAPI < 1 {
		return errors.New("web_client_api must be at least 1")
	}
	if _, err := ParseVersion(c.MinServerVersion); err != nil {
		return fmt.Errorf("min_server_version: %w", err)
	}
	if _, err := ParseVersion(c.SecurityFloor); err != nil {
		return fmt.Errorf("security_floor: %w", err)
	}
	return nil
}

// within checks that the declared numbers do not point past the bundle's own
// version (both are already valid versions).
func (c Compat) within(version string) error {
	if n, _ := CompareVersions(c.MinServerVersion, version); n > 0 {
		return errors.New("min_server_version is newer than the bundle version")
	}
	if n, _ := CompareVersions(c.SecurityFloor, version); n > 0 {
		return errors.New("security_floor is newer than the bundle version")
	}
	return nil
}

// LoadCompat reads and validates a compat file.
func LoadCompat(path string) (Compat, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Compat{}, err
	}
	var c Compat
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Compat{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return Compat{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// KeyID names a public key: the first 8 bytes of SHA-256 over the raw key, hex.
func KeyID(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

// ParsePrivateKeyPEM reads an Ed25519 key in PKCS#8 PEM form.
func ParsePrivateKeyPEM(data []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block in private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not Ed25519")
	}
	return priv, nil
}

// ParsePublicKeyPEM reads an Ed25519 public key in PKIX PEM form.
func ParsePublicKeyPEM(data []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block in public key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key is not Ed25519")
	}
	return pub, nil
}

// ValidPath reports whether p is an acceptable bundle-relative path: UTF-8,
// forward slashes only, no empty, "." or ".." segments, no control characters.
func ValidPath(p string) bool {
	if p == "" || len(p) > maxPathLen || !utf8.ValidString(p) {
		return false
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return false
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// BuildOptions configures Build.
type BuildOptions struct {
	// Dir is the built web app (web/dist).
	Dir string
	// Compat carries the compatibility numbers declared by the release.
	Compat Compat
	// Version is the release version, X.Y.Z.
	Version string
	// Commit is the source revision, recorded for provenance.
	Commit string
	// Key signs the manifest.
	Key ed25519.PrivateKey
	// Exclude lists path suffixes left out of the bundle (precompressed
	// variants and source maps that the desktop host never serves).
	Exclude []string
}

// Bundle is the result of Build.
type Bundle struct {
	Zip       []byte
	Manifest  []byte
	Signature []byte
	Info      Manifest
}

// Build packs a directory into a signed bundle. The output is deterministic
// for the same input: sorted entries, no timestamps.
func Build(o BuildOptions) (*Bundle, error) {
	if _, err := ParseVersion(o.Version); err != nil {
		return nil, fmt.Errorf("version: %w", err)
	}
	if err := o.Compat.Validate(); err != nil {
		return nil, err
	}
	if len(o.Key) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid signing key")
	}
	if err := o.Compat.within(o.Version); err != nil {
		return nil, err
	}
	contents := map[string][]byte{}
	var total int64
	err := filepath.WalkDir(o.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s: not a regular file", path)
		}
		rel, _ := filepath.Rel(o.Dir, path) // path is below o.Dir by construction
		rel = filepath.ToSlash(rel)
		for _, suffix := range o.Exclude {
			if strings.HasSuffix(rel, suffix) {
				return nil
			}
		}
		if !ValidPath(rel) {
			return fmt.Errorf("%s: invalid bundle path", rel)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		total += int64(len(data))
		if len(data) > MaxFileSize || total > MaxTotalSize || len(contents) >= MaxEntries {
			return fmt.Errorf("%s: bundle exceeds the size limits", rel)
		}
		contents[rel] = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	if _, ok := contents["index.html"]; !ok {
		return nil, errors.New("index.html is missing from the web app")
	}
	paths := make([]string, 0, len(contents))
	for p := range contents {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	m := Manifest{
		Schema:           Schema,
		Name:             Name,
		Version:          o.Version,
		WebClientAPI:     o.Compat.WebClientAPI,
		MinServerVersion: o.Compat.MinServerVersion,
		SecurityFloor:    o.Compat.SecurityFloor,
		Commit:           o.Commit,
		KeyID:            KeyID(o.Key.Public().(ed25519.PublicKey)),
	}
	for _, p := range paths {
		sum := sha256.Sum256(contents[p])
		m.Files = append(m.Files, File{Path: p, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(contents[p]))})
	}
	manifest, _ := json.MarshalIndent(m, "", "  ") // plain data cannot fail to marshal
	manifest = append(manifest, '\n')
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(o.Key, manifest)) + "\n")

	// Writing to memory cannot fail, so the zip writer's errors are dropped.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte) {
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		_, _ = w.Write(data)
	}
	add(manifestName, manifest)
	add(sigName, sig)
	for _, p := range paths {
		add(appPrefix+p, contents[p])
	}
	_ = zw.Close()
	return &Bundle{Zip: buf.Bytes(), Manifest: manifest, Signature: sig, Info: m}, nil
}
