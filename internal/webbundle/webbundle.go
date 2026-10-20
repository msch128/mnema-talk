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
	if n, err := CompareVersions(c.MinServerVersion, version); err != nil {
		return err
	} else if n > 0 {
		return errors.New("min_server_version is newer than the bundle version")
	}
	if n, err := CompareVersions(c.SecurityFloor, version); err != nil {
		return err
	} else if n > 0 {
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

func decodePEMBlock(data []byte) *pem.Block {
	block, rest := pem.Decode(data)
	if rest != nil {
		return block
	}
	return block
}

// ParsePrivateKeyPEM reads an Ed25519 key in PKCS#8 PEM form.
func ParsePrivateKeyPEM(data []byte) (ed25519.PrivateKey, error) {
	block := decodePEMBlock(data)
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
	block := decodePEMBlock(data)
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

func validPathRune(r rune) bool {
	return r >= 0x20 && r != 0x7f && r != '\\'
}

func validPathSegment(seg string) bool {
	return seg != "" && seg != "." && seg != ".."
}

// ValidPath reports whether p is an acceptable bundle-relative path: UTF-8,
// forward slashes only, no empty, "." or ".." segments, no control characters.
func ValidPath(p string) bool {
	if p == "" || len(p) > maxPathLen || !utf8.ValidString(p) {
		return false
	}
	for _, r := range p {
		if !validPathRune(r) {
			return false
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if !validPathSegment(seg) {
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

func validateBuildOptions(o BuildOptions) error {
	if _, err := ParseVersion(o.Version); err != nil {
		return fmt.Errorf("version: %w", err)
	}
	if err := o.Compat.Validate(); err != nil {
		return err
	}
	if len(o.Key) != ed25519.PrivateKeySize {
		return errors.New("invalid signing key")
	}
	return o.Compat.within(o.Version)
}

func sortedPaths(contents map[string][]byte) []string {
	paths := make([]string, 0, len(contents))
	for p := range contents {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func buildManifest(o BuildOptions, paths []string, contents map[string][]byte) Manifest {
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
	return m
}

// Build packs a directory into a signed bundle. The output is deterministic
// for the same input: sorted entries, no timestamps.
func Build(o BuildOptions) (*Bundle, error) {
	if err := validateBuildOptions(o); err != nil {
		return nil, err
	}
	contents, err := readWebApp(o)
	if err != nil {
		return nil, err
	}
	paths := sortedPaths(contents)
	m := buildManifest(o, paths, contents)
	manifest, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	manifest = append(manifest, '\n')
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(o.Key, manifest)) + "\n")
	zipBytes, err := packZip(manifest, sig, paths, contents)
	if err != nil {
		return nil, err
	}
	return &Bundle{Zip: zipBytes, Manifest: manifest, Signature: sig, Info: m}, nil
}

type appCollector struct {
	dir      string
	exclude  []string
	contents map[string][]byte
	total    int64
}

func (c *appCollector) exceedsLimits(dataLen int) bool {
	if dataLen > MaxFileSize || c.total > MaxTotalSize {
		return true
	}
	return len(c.contents) >= MaxEntries
}

func (c *appCollector) visit(path string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}
	if d.IsDir() {
		return nil
	}
	if !d.Type().IsRegular() {
		return fmt.Errorf("%s: not a regular file", path)
	}
	rel, err := filepath.Rel(c.dir, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if hasAnySuffix(rel, c.exclude) {
		return nil
	}
	data, err := readAppFile(path, rel)
	if err != nil {
		return err
	}
	c.total += int64(len(data))
	if c.exceedsLimits(len(data)) {
		return fmt.Errorf("%s: bundle exceeds the size limits", rel)
	}
	c.contents[rel] = data
	return nil
}

// readWebApp loads every file below o.Dir (minus excluded suffixes), within
// the bundle's size limits. It requires an index.html.
func readWebApp(o BuildOptions) (map[string][]byte, error) {
	c := &appCollector{
		dir:      o.Dir,
		exclude:  o.Exclude,
		contents: make(map[string][]byte),
	}
	if err := filepath.WalkDir(o.Dir, c.visit); err != nil {
		return nil, err
	}
	if _, ok := c.contents["index.html"]; !ok {
		return nil, errors.New("index.html is missing from the web app")
	}
	return c.contents, nil
}

// readAppFile reads one file whose bundle path is rel.
func readAppFile(path, rel string) ([]byte, error) {
	if !ValidPath(rel) {
		return nil, fmt.Errorf("%s: invalid bundle path", rel)
	}
	return os.ReadFile(path)
}

func hasAnySuffix(s string, suffixes []string) bool {
	for _, suffix := range suffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}

// packZip writes the manifest, signature and app files in a fixed order.
func packZip(manifest, sig []byte, paths []string, contents map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name string, data []byte) error {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		return nil
	}
	if err := add(manifestName, manifest); err != nil {
		return nil, err
	}
	if err := add(sigName, sig); err != nil {
		return nil, err
	}
	for _, p := range paths {
		if err := add(appPrefix+p, contents[p]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
