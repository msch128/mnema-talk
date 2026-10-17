package webbundle

import (
	"bufio"
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

const usage = `usage: webbundle <command> [flags]

  build   -dir web/dist -compat web/desktop-compat.json -version X.Y.Z -commit SHA -out DIR [-security-floor X.Y.Z]
          [-key-env NAME | -key-file PATH] [-exclude .br,.gz,.map]
          signs the web app and writes mnema-web-<version>.zip, manifest.json, manifest.sig
  verify  -zip FILE -pub web/desktop-signing.pub
  keyid   -pub web/desktop-signing.pub
  trust   keygen|sign|verify   offline root key and signed trust statements (key revocation)
`

// Run implements the webbundle command line and returns the exit code.
func Run(args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "build":
		err = runBuild(args[1:], getenv, stdout, stderr)
	case "verify":
		err = runVerify(args[1:], stdout, stderr)
	case "keyid":
		err = runKeyID(args[1:], stdout, stderr)
	case "trust":
		err = runTrust(args[1:], stdin, stdout, stderr)
	default:
		_, _ = fmt.Fprint(stderr, usage)
		return 2
	}
	switch {
	case err == nil:
		return 0
	case errors.Is(err, flag.ErrHelp):
		return 2
	}
	_, _ = fmt.Fprintln(stderr, "webbundle:", err)
	return 1
}

func newFlags(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func runBuild(args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	fs := newFlags("build", stderr)
	dir := fs.String("dir", "web/dist", "built web app")
	compat := fs.String("compat", "web/desktop-compat.json", "compatibility file")
	version := fs.String("version", "", "release version X.Y.Z")
	commit := fs.String("commit", "", "source revision")
	out := fs.String("out", "", "output directory")
	keyEnv := fs.String("key-env", "WEB_BUNDLE_SIGNING_KEY", "environment variable holding the PKCS#8 PEM private key")
	keyFile := fs.String("key-file", "", "file holding the PKCS#8 PEM private key (instead of -key-env)")
	floor := fs.String("security-floor", "", "raise the compat file's security_floor to this version (never lowers it)")
	exclude := fs.String("exclude", ".br,.gz,.map", "comma-separated path suffixes to leave out")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required")
	}
	pemKey := []byte(getenv(*keyEnv))
	if *keyFile != "" {
		b, err := os.ReadFile(*keyFile)
		if err != nil {
			return err
		}
		pemKey = b
	}
	if len(pemKey) == 0 {
		return fmt.Errorf("no signing key: set %s or pass -key-file", *keyEnv)
	}
	key, err := ParsePrivateKeyPEM(pemKey)
	if err != nil {
		return err
	}
	c, err := LoadCompat(*compat)
	if err != nil {
		return err
	}
	if *floor != "" {
		n, err := CompareVersions(*floor, c.SecurityFloor)
		if err != nil {
			return fmt.Errorf("-security-floor: %w", err)
		}
		if n > 0 {
			c.SecurityFloor = *floor
		}
	}
	var suffixes []string
	for _, s := range strings.Split(*exclude, ",") {
		if s = strings.TrimSpace(s); s != "" {
			suffixes = append(suffixes, s)
		}
	}
	b, err := Build(BuildOptions{Dir: *dir, Compat: c, Version: *version, Commit: *commit, Key: key, Exclude: suffixes})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	for _, f := range []struct {
		name string
		data []byte
	}{
		{"mnema-web-" + *version + ".zip", b.Zip},
		{manifestName, b.Manifest},
		{sigName, b.Signature},
	} {
		if err := os.WriteFile(filepath.Join(*out, f.name), f.data, 0o644); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(stdout, "bundle %s: %d files, %d bytes, key %s\n", b.Info.Version, len(b.Info.Files), len(b.Zip), b.Info.KeyID)
	return nil
}

func readPub(path string) (ed25519.PublicKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParsePublicKeyPEM(raw)
}

func runVerify(args []string, stdout, stderr io.Writer) error {
	fs := newFlags("verify", stderr)
	zipPath := fs.String("zip", "", "bundle to verify")
	pubPath := fs.String("pub", "web/desktop-signing.pub", "trusted public key (PEM)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pub, err := readPub(*pubPath)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(*zipPath)
	if err != nil {
		return err
	}
	m, err := Verify(data, []ed25519.PublicKey{pub})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "ok: version %s, web_client_api %d, %d files, key %s\n", m.Version, m.WebClientAPI, len(m.Files), m.KeyID)
	return nil
}

func runKeyID(args []string, stdout, stderr io.Writer) error {
	fs := newFlags("keyid", stderr)
	pubPath := fs.String("pub", "web/desktop-signing.pub", "public key (PEM)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pub, err := readPub(*pubPath)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stdout, KeyID(pub))
	return nil
}

const trustUsage = `usage: webbundle trust <command> [flags]

  keygen  -out DIR            new offline root key: DIR/offline-root.key (mode 600), DIR/offline-root.pub
  sign    -key-file PATH -out DIR [-prev trust.json] [-serial N]
          [-bundle-key PEM|HEX]... [-revoke-bundle-key ID]...
          [-updater-key TEXT]... [-revoke-updater-key ID]... [-valid-for 4320h] [-renew]
          signs a trust statement; writes DIR/trust.json and DIR/trust.json.sig
          with -prev the serial is prev+1 and earlier revocations are kept;
          -renew also keeps prev's allowed keys (re-sign before it expires)
  verify  -trust FILE -sig FILE -pub PEM
`

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func runTrust(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		_, _ = fmt.Fprint(stderr, trustUsage)
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
	_, _ = fmt.Fprint(stderr, trustUsage)
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
	pub, priv, _ := GenerateRootKey()
	if err := errors.Join(
		os.WriteFile(keyPath, MarshalPrivateKeyPEM(priv), 0o600),
		os.WriteFile(filepath.Join(*out, "offline-root.pub"), MarshalPublicKeyPEM(pub), 0o644),
	); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s (keep it offline) and offline-root.pub, key id %s\n", keyPath, KeyID(pub))
	return nil
}

// bundleKeyArg accepts a PEM file path or the raw key as hex.
func bundleKeyArg(v string) (ed25519.PublicKey, error) {
	if b, err := hex.DecodeString(v); err == nil && len(b) == ed25519.PublicKeySize {
		return ed25519.PublicKey(b), nil
	}
	return readPub(v)
}

func runTrustSign(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := newFlags("trust sign", stderr)
	keyFile := fs.String("key-file", "", "offline root private key (PKCS#8 PEM)")
	out := fs.String("out", "", "output directory")
	prev := fs.String("prev", "", "previous trust.json: serial becomes prev+1 and its revocations are kept")
	prevSig := fs.String("prev-sig", "", "signature of -prev (default: -prev plus .sig); must verify under this root key")
	yes := fs.Bool("yes", false, "sign without asking for confirmation (tests only)")
	var maxVersions listFlag
	fs.Var(&maxVersions, "max-version", "API=X.Y.Z: cap the rollback marks a client keeps for that web client API (repeatable, not carried over)")
	serial := fs.Uint64("serial", 0, "serial (default: prev+1, or 1)")
	validFor := fs.Duration("valid-for", DefaultTrustValidity, "how long the statement is valid")
	renew := fs.Bool("renew", false, "with -prev: keep its allowed bundle and updater keys")
	var bundleKeys, revokeBundle, updaterKeys, revokeUpdater listFlag
	fs.Var(&bundleKeys, "bundle-key", "allowed web bundle public key: PEM file or 64 hex digits (repeatable)")
	fs.Var(&revokeBundle, "revoke-bundle-key", "bundle key id to revoke (repeatable)")
	fs.Var(&updaterKeys, "updater-key", "allowed updater public key as in tauri.conf.json (repeatable)")
	fs.Var(&revokeUpdater, "revoke-updater-key", "updater key id to revoke (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyFile == "" || *out == "" {
		return errors.New("-key-file and -out are required")
	}
	pemKey, err := os.ReadFile(*keyFile)
	if err != nil {
		return err
	}
	root, err := ParsePrivateKeyPEM(pemKey)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Second)
	t := Trust{
		Schema: TrustSchema, Serial: *serial,
		IssuedAt:  now.Format(time.RFC3339),
		ExpiresAt:  now.Add(*validFor).Format(time.RFC3339),
		MaxVersion: map[string]string{},
	}
	var p *Trust
	if *prev != "" {
		if *prevSig == "" {
			*prevSig = *prev + ".sig"
		}
		// Only a statement this root key signed is carried forward: the file
		// on disk or in a pull request is not trusted by itself.
		raw, err := os.ReadFile(*prev)
		if err != nil {
			return err
		}
		sigRaw, err := os.ReadFile(*prevSig)
		if err != nil {
			return fmt.Errorf("-prev needs its signature: %w", err)
		}
		if p, err = VerifyTrust(raw, sigRaw, root.Public().(ed25519.PublicKey)); err != nil {
			return fmt.Errorf("%s does not verify under this root key: %w", *prev, err)
		}
		if t.Serial == 0 {
			t.Serial = p.Serial + 1
		}
		if t.Serial <= p.Serial {
			return fmt.Errorf("serial %d is not above the previous %d", t.Serial, p.Serial)
		}
		if *renew {
			revoked := map[string]bool{}
			for _, id := range append(append([]string{}, revokeBundle...), revokeUpdater...) {
				revoked[id] = true
			}
			for _, k := range p.BundleKeys {
				raw, _ := hex.DecodeString(k) // verified
				if !revoked[KeyID(raw)] {
					t.BundleKeys = append(t.BundleKeys, k)
				}
			}
			for _, k := range p.UpdaterKeys {
				id, _ := UpdaterKeyID(k) // verified
				if !revoked[id] {
					updaterKeys = append(updaterKeys, k)
				}
			}
		}
		revokeBundle = append(revokeBundle, p.RevokedBundleKeys...)
		revokeUpdater = append(revokeUpdater, p.RevokedUpdaterKeys...)
	}
	if t.Serial == 0 {
		t.Serial = 1
	}
	for _, v := range bundleKeys {
		pub, err := bundleKeyArg(v)
		if err != nil {
			return fmt.Errorf("-bundle-key %s: %w", v, err)
		}
		t.BundleKeys = append(t.BundleKeys, hex.EncodeToString(pub))
	}
	t.RevokedBundleKeys = dedupe(revokeBundle)
	t.UpdaterKeys = dedupe(updaterKeys)
	t.RevokedUpdaterKeys = dedupe(revokeUpdater)
	t.BundleKeys = dedupe(t.BundleKeys)
	for _, m := range maxVersions {
		api, version, ok := strings.Cut(m, "=")
		if !ok {
			return fmt.Errorf("-max-version %q is not API=X.Y.Z", m)
		}
		t.MaxVersion[api] = version
	}
	doc, sig, err := SignTrust(t, root)
	if err != nil {
		return err
	}
	describeTrust(stdout, &t, p)
	if !*yes {
		_, _ = fmt.Fprint(stdout, "Sign this statement? [y/N] ")
		answer, _ := bufio.NewReader(stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			return errors.New("aborted, nothing written")
		}
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	if err := errors.Join(
		os.WriteFile(filepath.Join(*out, "trust.json"), doc, 0o644),
		os.WriteFile(filepath.Join(*out, "trust.json.sig"), sig, 0o644),
	); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "signed trust statement serial %d: %d bundle key(s), %d revoked, %d updater key(s), %d revoked\n",
		t.Serial, len(t.BundleKeys), len(t.RevokedBundleKeys), len(t.UpdaterKeys), len(t.RevokedUpdaterKeys))
	return nil
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
	_, _ = fmt.Fprintf(stdout, "ok: serial %d, %d bundle key(s), valid until %s, root key %s\n", t.Serial, len(t.BundleKeys), t.ExpiresAt, KeyID(pub))
	return nil
}

// describeTrust prints the statement about to be signed with short ids, and
// what changed against the verified previous one (+ added, - removed).
func describeTrust(w io.Writer, t, prev *Trust) {
	if prev == nil {
		prev = &Trust{}
	}
	_, _ = fmt.Fprintf(w, "Trust statement serial %d, valid %s to %s\n", t.Serial, t.IssuedAt, t.ExpiresAt)
	bundleID := func(k string) string {
		raw, _ := hex.DecodeString(k)
		return KeyID(raw) + " (key " + k[:8] + "...)"
	}
	updaterID := func(k string) string { id, _ := UpdaterKeyID(k); return id }
	same := func(k string) string { return k }
	listChange(w, "allowed bundle keys", t.BundleKeys, prev.BundleKeys, bundleID)
	listChange(w, "revoked bundle key ids", t.RevokedBundleKeys, prev.RevokedBundleKeys, same)
	listChange(w, "allowed updater keys", t.UpdaterKeys, prev.UpdaterKeys, updaterID)
	listChange(w, "revoked updater key ids", t.RevokedUpdaterKeys, prev.RevokedUpdaterKeys, same)
	for api, v := range t.MaxVersion {
		_, _ = fmt.Fprintf(w, "  rollback cap: web client API %s at most %s\n", api, v)
	}
}

func listChange(w io.Writer, title string, now, before []string, label func(string) string) {
	_, _ = fmt.Fprintf(w, "%s:\n", title)
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
		_, _ = fmt.Fprintf(w, "  %s %s\n", mark, label(k))
	}
	for _, k := range before {
		if !is[k] {
			_, _ = fmt.Fprintf(w, "  - %s\n", label(k))
		}
	}
}
