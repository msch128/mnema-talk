package webbundle

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Verify checks a bundle against the trusted public keys and returns its
// manifest. It fails on a bad or unknown signature, a manifest that does not
// describe exactly the files in the zip, or any limit violation.
func Verify(data []byte, keys []ed25519.PublicKey) (*Manifest, error) {
	entries, err := zipEntries(data)
	if err != nil {
		return nil, err
	}
	manifest, signer, err := verifySignature(entries, keys)
	if err != nil {
		return nil, err
	}
	// Only now is the manifest parsed.
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(manifest))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if err := m.check(signer); err != nil {
		return nil, err
	}
	if len(entries) != len(m.Files)+2 {
		return nil, errors.New("zip entries do not match the manifest")
	}
	for _, f := range m.Files {
		if err := verifyFile(entries, f); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

// zipEntries indexes the archive by name and rejects duplicates.
func zipEntries(data []byte) (map[string]*zip.File, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("read zip: %w", err)
	}
	if len(zr.File) > MaxEntries+2 {
		return nil, errors.New("too many entries")
	}
	entries := make(map[string]*zip.File, len(zr.File))
	for _, f := range zr.File {
		if _, dup := entries[f.Name]; dup {
			return nil, fmt.Errorf("duplicate entry %q", f.Name)
		}
		entries[f.Name] = f
	}
	return entries, nil
}

// readEntry returns an entry's bytes. The declared size is checked before
// anything is decompressed and the reader never goes past it.
func readEntry(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%s: too large", f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(io.LimitReader(rc, limit))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.Name, err)
	}
	return b, nil
}

// verifySignature returns the raw manifest and the trusted key that signed it.
func verifySignature(entries map[string]*zip.File, keys []ed25519.PublicKey) ([]byte, ed25519.PublicKey, error) {
	mf, sf := entries[manifestName], entries[sigName]
	if mf == nil || sf == nil {
		return nil, nil, errors.New("manifest or signature is missing")
	}
	manifest, err := readEntry(mf, maxManifestSize)
	if err != nil {
		return nil, nil, err
	}
	sig, err := readSignature(sf)
	if err != nil {
		return nil, nil, err
	}
	key, err := matchSigningKey(keys, manifest, sig)
	if err != nil {
		return nil, nil, err
	}
	return manifest, key, nil
}

func readSignature(sf *zip.File) ([]byte, error) {
	sigText, err := readEntry(sf, 1024)
	if err != nil {
		return nil, err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("malformed signature")
	}
	return sig, nil
}

func matchSigningKey(keys []ed25519.PublicKey, manifest, sig []byte) (ed25519.PublicKey, error) {
	for _, k := range keys {
		if len(k) == ed25519.PublicKeySize && ed25519.Verify(k, manifest, sig) {
			return k, nil
		}
	}
	return nil, errors.New("signature does not match a trusted key")
}

// verifyFile compares one zip entry with its manifest record.
func verifyFile(entries map[string]*zip.File, f File) error {
	e := entries[appPrefix+f.Path]
	if e == nil {
		return fmt.Errorf("%s: missing from the zip", f.Path)
	}
	if e.UncompressedSize64 != uint64(f.Size) {
		return fmt.Errorf("%s: size does not match the manifest", f.Path)
	}
	body, err := readEntry(e, f.Size)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != f.SHA256 {
		return fmt.Errorf("%s: content does not match the manifest", f.Path)
	}
	return nil
}

func (m *Manifest) check(signer ed25519.PublicKey) error {
	if m.Schema != Schema || m.Name != Name {
		return errors.New("unsupported manifest")
	}
	if m.KeyID != KeyID(signer) {
		return errors.New("manifest key_id does not match the signing key")
	}
	if _, err := ParseVersion(m.Version); err != nil {
		return fmt.Errorf("version: %w", err)
	}
	compat := Compat{WebClientAPI: m.WebClientAPI, MinServerVersion: m.MinServerVersion, SecurityFloor: m.SecurityFloor}
	if err := compat.Validate(); err != nil {
		return err
	}
	if err := compat.within(m.Version); err != nil {
		return err
	}
	return checkFileList(m.Files)
}

// checkFileList validates the manifest's file records: paths, hashes, sizes
// and the total budget.
func checkFileList(files []File) error {
	if len(files) == 0 || len(files) > MaxEntries {
		return errors.New("invalid file count")
	}
	seen := make(map[string]bool, len(files))
	hasIndex := false
	var total int64
	for _, f := range files {
		if err := checkFileRecord(f, seen); err != nil {
			return err
		}
		seen[f.Path] = true
		if f.Path == "index.html" {
			hasIndex = true
		}
		total += f.Size
	}
	if !hasIndex {
		return errors.New("index.html is missing")
	}
	if total > MaxTotalSize {
		return errors.New("bundle exceeds the size limit")
	}
	return nil
}

func checkFileRecord(f File, seen map[string]bool) error {
	if !ValidPath(f.Path) || seen[f.Path] {
		return fmt.Errorf("invalid or duplicate path %q", f.Path)
	}
	if !isValidSHA256(f.SHA256) {
		return fmt.Errorf("%s: invalid sha256", f.Path)
	}
	if f.Size < 0 || f.Size > MaxFileSize {
		return fmt.Errorf("%s: invalid size", f.Path)
	}
	return nil
}

func isValidSHA256(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == sha256.Size
}
