package webbundle

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const trustUsage = `usage: webbundle trust <command> [flags]

  keygen  -out DIR            new offline root key: DIR/offline-root.key (mode 600), DIR/offline-root.pub
  sign    -key-file PATH -out DIR [-prev trust.json] [-serial N]
          [-bundle-key PEM|HEX]... [-revoke-bundle-key ID]...
          [-updater-key TEXT]... [-revoke-updater-key ID]... [-valid-for 4320h] [-renew]
          signs a trust statement; writes DIR/trust.json and DIR/trust.json.sig
          with -prev the serial is prev+1 and earlier revocations are kept;
          -renew also keeps prev's allowed keys (re-sign before it expires)
  verify  -trust FILE -sig FILE -pub PEM [-min-valid-days N]
          -min-valid-days fails when fewer than N days remain (the scheduled expiry check uses 60)
`

type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }

func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func runTrust(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printString(stderr, trustUsage)
		return flag.ErrHelp
	}
	switch args[0] {
	case "keygen":
		return runTrustKeygen(args[1:], stdout, stderr)
	case "sign":
		return runTrustSign(args[1:], stdin, stdout, stderr)
	case "verify":
		return runTrustVerify(args[1:], stdout, stderr)
	}
	printString(stderr, trustUsage)
	return flag.ErrHelp
}

func runTrustKeygen(args []string, stdout, stderr io.Writer) error {
	fs := newFlags("trust keygen", stderr)
	out := fs.String("out", "", "directory for the new key pair")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required")
	}
	keyPath := filepath.Join(*out, "offline-root.key")
	if _, err := os.Stat(keyPath); err == nil {
		return fmt.Errorf("%s exists; refusing to overwrite a root key", keyPath)
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	pub, priv, err := GenerateRootKey()
	if err != nil {
		return err
	}
	if err := errors.Join(
		os.WriteFile(keyPath, MarshalPrivateKeyPEM(priv), 0o600),
		os.WriteFile(filepath.Join(*out, "offline-root.pub"), MarshalPublicKeyPEM(pub), 0o644),
	); err != nil {
		return err
	}
	printFormat(stdout, "wrote %s (keep it offline) and offline-root.pub, key id %s\n", keyPath, KeyID(pub))
	return nil
}

// bundleKeyArg accepts a PEM file path or the raw key as hex.
func bundleKeyArg(v string) (ed25519.PublicKey, error) {
	if b, err := hex.DecodeString(v); err == nil && len(b) == ed25519.PublicKeySize {
		return ed25519.PublicKey(b), nil
	}
	return readPub(v)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func runTrustVerify(args []string, stdout, stderr io.Writer) error {
	fs := newFlags("trust verify", stderr)
	doc := fs.String("trust", "desktop-trust/trust.json", "trust statement")
	sig := fs.String("sig", "desktop-trust/trust.json.sig", "its signature")
	pubPath := fs.String("pub", "desktop-trust/root.pub", "root public key (PEM)")
	minDays := fs.Int("min-valid-days", 0, "fail when the statement expires in fewer than this many days")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pub, err := readPub(*pubPath)
	if err != nil {
		return err
	}
	docB, err := os.ReadFile(*doc)
	if err != nil {
		return err
	}
	sigB, err := os.ReadFile(*sig)
	if err != nil {
		return err
	}
	t, err := VerifyTrust(docB, sigB, pub)
	if err != nil {
		return err
	}
	if t.Expired(time.Now()) {
		return fmt.Errorf("trust statement serial %d expired at %s; re-sign it with trust sign -renew", t.Serial, t.ExpiresAt)
	}
	if err := checkRemaining(t, time.Now(), *minDays); err != nil {
		return err
	}
	printFormat(stdout, "ok: serial %d, %d bundle key(s), valid until %s, root key %s\n", t.Serial, len(t.BundleKeys), t.ExpiresAt, KeyID(pub))
	return nil
}

// checkRemaining fails when t expires in fewer than minDays days at now.
func checkRemaining(t *Trust, now time.Time, minDays int) error {
	if minDays <= 0 {
		return nil
	}
	expires, err := parseUTC(t.ExpiresAt)
	if err != nil {
		return err
	}
	left := expires.Sub(now)
	if left < time.Duration(minDays)*24*time.Hour {
		return fmt.Errorf("trust statement serial %d expires at %s, in %d days (limit %d); renew it (doc/desktop-trusted-client.md, \"Renewing the statement\")",
			t.Serial, t.ExpiresAt, int(left.Hours()/24), minDays)
	}
	return nil
}

// describeTrust prints the statement about to be signed with short ids, and
// what changed against the verified previous one (+ added, - removed).
func describeTrust(w io.Writer, t, prev *Trust) {
	if prev == nil {
		prev = &Trust{}
	}
	printFormat(w, "Trust statement serial %d, valid %s to %s\n", t.Serial, t.IssuedAt, t.ExpiresAt)
	bundleID := func(k string) string {
		raw, err := hex.DecodeString(k)
		if err != nil {
			return k
		}
		return KeyID(raw) + " (key " + k[:8] + "...)"
	}
	updaterID := func(k string) string {
		id, err := UpdaterKeyID(k)
		if err != nil {
			return k
		}
		return id
	}
	same := func(k string) string { return k }
	listChange(w, "allowed bundle keys", t.BundleKeys, prev.BundleKeys, bundleID)
	listChange(w, "revoked bundle key ids", t.RevokedBundleKeys, prev.RevokedBundleKeys, same)
	listChange(w, "allowed updater keys", t.UpdaterKeys, prev.UpdaterKeys, updaterID)
	listChange(w, "revoked updater key ids", t.RevokedUpdaterKeys, prev.RevokedUpdaterKeys, same)
	for api, v := range t.MaxVersion {
		printFormat(w, "  rollback cap: web client API %s at most %s\n", api, v)
	}
}

func listChange(w io.Writer, title string, now, before []string, label func(string) string) {
	printFormat(w, "%s:\n", title)
	had := map[string]bool{}
	for _, k := range before {
		had[k] = true
	}
	is := map[string]bool{}
	for _, k := range now {
		is[k] = true
		mark := "+"
		if had[k] {
			mark = " "
		}
		printFormat(w, "  %s %s\n", mark, label(k))
	}
	for _, k := range before {
		if !is[k] {
			printFormat(w, "  - %s\n", label(k))
		}
	}
}
