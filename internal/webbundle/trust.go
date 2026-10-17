package webbundle

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TrustSchema is the trust statement schema number.
const TrustSchema = 1

const maxTrustSize = 64 << 10

const (
	// DefaultTrustValidity is how long a statement signed by the CLI is valid.
	DefaultTrustValidity = 180 * 24 * time.Hour
	// MaxTrustValidity bounds ExpiresAt-IssuedAt.
	MaxTrustValidity = 400 * 24 * time.Hour
)

func parseUTC(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil || t.UTC().Format(time.RFC3339) != s {
		return time.Time{}, errors.New("not a UTC time like 2026-10-17T10:00:00Z")
	}
	return t, nil
}

// Trust is the statement signed by the offline root key (see
// doc/desktop-trusted-client.md, "Key revocation"). The desktop client pins
// only the root public key; which web bundle and updater keys it accepts is
// decided by the newest statement it has seen, so a leaked CI key is revoked
// by publishing a new statement instead of shipping a new client.
//
// The statement is exhaustive: once a client has one, only the listed keys
// that are not revoked are accepted. Revocations are cumulative; a key that
// was revoked once is never listed again.
type Trust struct {
	Schema int `json:"schema"`
	// Serial grows with every statement. Clients refuse a lower one than the
	// highest they have seen.
	Serial uint64 `json:"serial"`
	// IssuedAt and ExpiresAt are UTC times in the form 2006-01-02T15:04:05Z.
	// Clients refuse an expired statement and install nothing new while the one
	// in force has expired, so a blocked fetch cannot freeze them forever.
	IssuedAt  string `json:"issued_at"`
	ExpiresAt string `json:"expires_at"`
	// BundleKeys are the allowed web bundle public keys, raw Ed25519, hex.
	BundleKeys []string `json:"bundle_keys"`
	// RevokedBundleKeys are bundle key ids (see KeyID).
	RevokedBundleKeys []string `json:"revoked_bundle_keys"`
	// UpdaterKeys are allowed Tauri updater public keys in the text form of
	// tauri.conf.json (base64 of the minisign .pub file).
	UpdaterKeys []string `json:"updater_keys"`
	// RevokedUpdaterKeys are minisign key ids (see UpdaterKeyID).
	RevokedUpdaterKeys []string `json:"revoked_updater_keys"`
}

// GenerateRootKey creates a key pair for signing trust statements.
func GenerateRootKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// MarshalPrivateKeyPEM encodes a key as PKCS#8 PEM.
func MarshalPrivateKeyPEM(k ed25519.PrivateKey) []byte {
	der, _ := x509.MarshalPKCS8PrivateKey(k) // cannot fail for an Ed25519 key
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// MarshalPublicKeyPEM encodes a public key as PKIX PEM.
func MarshalPublicKeyPEM(k ed25519.PublicKey) []byte {
	der, _ := x509.MarshalPKIXPublicKey(k) // cannot fail for an Ed25519 key
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// UpdaterKeyID returns the 8-byte key id inside a minisign public key given in
// tauri.conf.json form, as lowercase hex of the bytes in file order. The same
// id sits in every signature made with that key.
func UpdaterKeyID(pubkey string) (string, error) {
	raw, err := minisignLine(pubkey)
	if err != nil {
		return "", err
	}
	if len(raw) != 42 || raw[0] != 'E' || raw[1] != 'd' {
		return "", errors.New("not a minisign Ed25519 public key")
	}
	return hex.EncodeToString(raw[2:10]), nil
}

// minisignLine decodes the base64 payload line of a base64-wrapped minisign
// file.
func minisignLine(wrapped string) ([]byte, error) {
	text, err := base64.StdEncoding.DecodeString(strings.TrimSpace(wrapped))
	if err != nil {
		return nil, errors.New("updater key is not base64")
	}
	lines := strings.Split(strings.TrimSpace(string(text)), "\n")
	if len(lines) < 2 {
		return nil, errors.New("updater key has no key line")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil {
		return nil, errors.New("updater key line is not base64")
	}
	return raw, nil
}

func validID(s string) bool {
	if len(s) != 16 {
		return false
	}
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 8 && s == strings.ToLower(s)
}

// Validate checks that a statement is well formed and consistent.
func (t *Trust) Validate() error {
	if t.Schema != TrustSchema {
		return fmt.Errorf("unsupported trust schema %d", t.Schema)
	}
	if t.Serial < 1 {
		return errors.New("serial must be at least 1")
	}
	issued, err := parseUTC(t.IssuedAt)
	if err != nil {
		return fmt.Errorf("issued_at: %w", err)
	}
	expires, err := parseUTC(t.ExpiresAt)
	if err != nil {
		return fmt.Errorf("expires_at: %w", err)
	}
	if !expires.After(issued) || expires.Sub(issued) > MaxTrustValidity {
		return errors.New("expires_at must be after issued_at, within 400 days")
	}
	revokedBundle := map[string]bool{}
	for _, id := range t.RevokedBundleKeys {
		if !validID(id) {
			return fmt.Errorf("revoked bundle key id %q is not 16 lowercase hex digits", id)
		}
		revokedBundle[id] = true
	}
	for _, h := range t.BundleKeys {
		raw, err := hex.DecodeString(h)
		if err != nil || len(raw) != ed25519.PublicKeySize || h != strings.ToLower(h) {
			return fmt.Errorf("bundle key %q is not a 32 byte lowercase hex key", h)
		}
		if revokedBundle[KeyID(raw)] {
			return fmt.Errorf("bundle key %s is both allowed and revoked", KeyID(raw))
		}
	}
	revokedUpdater := map[string]bool{}
	for _, id := range t.RevokedUpdaterKeys {
		if !validID(id) {
			return fmt.Errorf("revoked updater key id %q is not 16 lowercase hex digits", id)
		}
		revokedUpdater[id] = true
	}
	for _, k := range t.UpdaterKeys {
		id, err := UpdaterKeyID(k)
		if err != nil {
			return err
		}
		if revokedUpdater[id] {
			return fmt.Errorf("updater key %s is both allowed and revoked", id)
		}
	}
	if len(t.BundleKeys) == 0 {
		return errors.New("bundle_keys must not be empty")
	}
	return nil
}

// SignTrust validates and serializes a statement and signs the exact bytes.
// The result is the pair of files to publish: trust.json and trust.json.sig.
func SignTrust(t Trust, root ed25519.PrivateKey) (doc, sig []byte, err error) {
	if err := t.Validate(); err != nil {
		return nil, nil, err
	}
	doc, _ = json.MarshalIndent(t, "", "  ") // plain strings and numbers
	doc = append(doc, '\n')
	sig = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(root, doc)) + "\n")
	return doc, sig, nil
}

// VerifyTrust checks the signature over the exact bytes of doc with the root
// key, then parses and validates the statement. Nothing is parsed before the
// signature holds.
func VerifyTrust(doc, sig []byte, root ed25519.PublicKey) (*Trust, error) {
	if len(doc) > maxTrustSize {
		return nil, errors.New("trust statement too large")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return nil, errors.New("malformed trust signature")
	}
	if !ed25519.Verify(root, doc, raw) {
		return nil, errors.New("trust signature does not match the root key")
	}
	var t Trust
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("trust statement: %w", err)
	}
	if err := t.Validate(); err != nil {
		return nil, err
	}
	return &t, nil
}

// AllowsBundleKey reports whether a statement accepts a web bundle key.
func (t *Trust) AllowsBundleKey(pub ed25519.PublicKey) bool {
	id := KeyID(pub)
	for _, id2 := range t.RevokedBundleKeys {
		if id2 == id {
			return false
		}
	}
	h := hex.EncodeToString(pub)
	for _, k := range t.BundleKeys {
		if k == h {
			return true
		}
	}
	return false
}

// Expired reports whether the statement is no longer valid at now.
func (t *Trust) Expired(now time.Time) bool {
	expires, err := parseUTC(t.ExpiresAt)
	return err != nil || !now.Before(expires)
}
