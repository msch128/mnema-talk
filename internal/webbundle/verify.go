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
	// read returns an entry's bytes. The declared size is checked before
	// anything is decompressed and the reader never goes past it.
	read := func(f *zip.File, limit int64) ([]byte, error) {
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
	mf, sf := entries[manifestName], entries[sigName]
	if mf == nil || sf == nil {
		return nil, errors.New("manifest or signature is missing")
	}
	manifest, err := read(mf, maxManifestSize)
	if err != nil {
		return nil, err
	}
	sigText, err := read(sf, 1024)
	if err != nil {
		return nil, err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("malformed signature")
	}
	var signer ed25519.PublicKey
	for _, k := range keys {
		if len(k) == ed25519.PublicKeySize && ed25519.Verify(k, manifest, sig) {
			signer = k
			break
		}
	}
	if signer == nil {
		return nil, errors.New("signature does not match a trusted key")
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
		e := entries[appPrefix+f.Path]
		if e == nil {
			return nil, fmt.Errorf("%s: missing from the zip", f.Path)
		}
		if e.UncompressedSize64 != uint64(f.Size) {
			return nil, fmt.Errorf("%s: size does not match the manifest", f.Path)
		}
		body, err := read(e, f.Size)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != f.SHA256 {
			return nil, fmt.Errorf("%s: content does not match the manifest", f.Path)
		}
	}
	return &m, nil
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
	if len(m.Files) == 0 || len(m.Files) > MaxEntries {
		return errors.New("invalid file count")
	}
	seen := make(map[string]bool, len(m.Files))
	hasIndex := false
	var total int64
	for _, f := range m.Files {
		if !ValidPath(f.Path) || seen[f.Path] {
			return fmt.Errorf("invalid or duplicate path %q", f.Path)
		}
		seen[f.Path] = true
		hasIndex = hasIndex || f.Path == "index.html"
		raw, err := hex.DecodeString(f.SHA256)
		if err != nil || len(raw) != sha256.Size {
			return fmt.Errorf("%s: invalid sha256", f.Path)
		}
		if f.Size < 0 || f.Size > MaxFileSize {
			return fmt.Errorf("%s: invalid size", f.Path)
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
