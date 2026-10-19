package webbundle

import (
	"bufio"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// trustSignOpts holds the flags of `trust sign`.
type trustSignOpts struct {
	keyFile, out, prev, prevSig string
	yes, renew                  bool
	serial                      uint64
	validFor                    time.Duration
	maxVersions                 listFlag
	bundleKeys, revokeBundle    listFlag
	updaterKeys, revokeUpdater  listFlag
}

func parseTrustSignFlags(args []string, stderr io.Writer) (*trustSignOpts, error) {
	fs := newFlags("trust sign", stderr)
	o := &trustSignOpts{}
	fs.StringVar(&o.keyFile, "key-file", "", "offline root private key (PKCS#8 PEM)")
	fs.StringVar(&o.out, "out", "", "output directory")
	fs.StringVar(&o.prev, "prev", "", "previous trust.json: serial becomes prev+1 and its revocations are kept")
	fs.StringVar(&o.prevSig, "prev-sig", "", "signature of -prev (default: -prev plus .sig); must verify under this root key")
	fs.BoolVar(&o.yes, "yes", false, "sign without asking for confirmation (tests only)")
	fs.Var(&o.maxVersions, "max-version", "API=X.Y.Z: cap the rollback marks a client keeps for that web client API (repeatable, not carried over)")
	fs.Uint64Var(&o.serial, "serial", 0, "serial (default: prev+1, or 1)")
	fs.DurationVar(&o.validFor, "valid-for", DefaultTrustValidity, "how long the statement is valid")
	fs.BoolVar(&o.renew, "renew", false, "with -prev: keep its allowed bundle and updater keys")
	fs.Var(&o.bundleKeys, "bundle-key", "allowed web bundle public key: PEM file or 64 hex digits (repeatable)")
	fs.Var(&o.revokeBundle, "revoke-bundle-key", "bundle key id to revoke (repeatable)")
	fs.Var(&o.updaterKeys, "updater-key", "allowed updater public key as in tauri.conf.json (repeatable)")
	fs.Var(&o.revokeUpdater, "revoke-updater-key", "updater key id to revoke (repeatable)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if o.keyFile == "" || o.out == "" {
		return nil, errors.New("-key-file and -out are required")
	}
	return o, nil
}

func runTrustSign(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	o, err := parseTrustSignFlags(args, stderr)
	if err != nil {
		return err
	}
	pemKey, err := os.ReadFile(o.keyFile)
	if err != nil {
		return err
	}
	root, err := ParsePrivateKeyPEM(pemKey)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Second)
	t := Trust{
		Schema: TrustSchema, Serial: o.serial,
		IssuedAt:   now.Format(time.RFC3339),
		ExpiresAt:  now.Add(o.validFor).Format(time.RFC3339),
		MaxVersion: map[string]string{},
	}
	var prev *Trust
	if o.prev != "" {
		if prev, err = o.carryForward(&t, root.Public().(ed25519.PublicKey)); err != nil {
			return err
		}
	}
	if t.Serial == 0 {
		t.Serial = 1
	}
	if err := o.fillKeys(&t); err != nil {
		return err
	}
	if err := parseMaxVersions(&t, o.maxVersions); err != nil {
		return err
	}
	doc, sig, err := SignTrust(t, root)
	if err != nil {
		return err
	}
	describeTrust(stdout, &t, prev)
	if !o.yes && !confirmSigning(stdin, stdout) {
		return errors.New("aborted, nothing written")
	}
	if err := writeTrust(o.out, doc, sig); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "signed trust statement serial %d: %d bundle key(s), %d revoked, %d updater key(s), %d revoked\n",
		t.Serial, len(t.BundleKeys), len(t.RevokedBundleKeys), len(t.UpdaterKeys), len(t.RevokedUpdaterKeys))
	return nil
}

// carryForward loads the previous statement, which must verify under the root
// key, takes its serial and revocations (and with -renew its allowed keys)
// into t, and returns it.
func (o *trustSignOpts) carryForward(t *Trust, root ed25519.PublicKey) (*Trust, error) {
	if o.prevSig == "" {
		o.prevSig = o.prev + ".sig"
	}
	// Only a statement this root key signed is carried forward: the file
	// on disk or in a pull request is not trusted by itself.
	raw, err := os.ReadFile(o.prev)
	if err != nil {
		return nil, err
	}
	sigRaw, err := os.ReadFile(o.prevSig)
	if err != nil {
		return nil, fmt.Errorf("-prev needs its signature: %w", err)
	}
	p, err := VerifyTrust(raw, sigRaw, root)
	if err != nil {
		return nil, fmt.Errorf("%s does not verify under this root key: %w", o.prev, err)
	}
	if t.Serial == 0 {
		t.Serial = p.Serial + 1
	}
	if t.Serial <= p.Serial {
		return nil, fmt.Errorf("serial %d is not above the previous %d", t.Serial, p.Serial)
	}
	if o.renew {
		o.keepAllowedKeys(t, p)
	}
	o.revokeBundle = append(o.revokeBundle, p.RevokedBundleKeys...)
	o.revokeUpdater = append(o.revokeUpdater, p.RevokedUpdaterKeys...)
	return p, nil
}

// keepAllowedKeys copies the previous statement's allowed keys that this run
// does not revoke.
func (o *trustSignOpts) keepAllowedKeys(t, p *Trust) {
	revoked := map[string]bool{}
	for _, id := range append(append([]string{}, o.revokeBundle...), o.revokeUpdater...) {
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
			o.updaterKeys = append(o.updaterKeys, k)
		}
	}
}

// fillKeys adds the -bundle-key arguments and sets the deduplicated key and
// revocation lists.
func (o *trustSignOpts) fillKeys(t *Trust) error {
	for _, v := range o.bundleKeys {
		pub, err := bundleKeyArg(v)
		if err != nil {
			return fmt.Errorf("-bundle-key %s: %w", v, err)
		}
		t.BundleKeys = append(t.BundleKeys, hex.EncodeToString(pub))
	}
	t.RevokedBundleKeys = dedupe(o.revokeBundle)
	t.UpdaterKeys = dedupe(o.updaterKeys)
	t.RevokedUpdaterKeys = dedupe(o.revokeUpdater)
	t.BundleKeys = dedupe(t.BundleKeys)
	return nil
}

// parseMaxVersions reads API=X.Y.Z rollback caps into t.
func parseMaxVersions(t *Trust, caps listFlag) error {
	for _, m := range caps {
		api, version, ok := strings.Cut(m, "=")
		if !ok {
			return fmt.Errorf("-max-version %q is not API=X.Y.Z", m)
		}
		t.MaxVersion[api] = version
	}
	return nil
}

// confirmSigning asks on stdout and reads the answer from stdin.
func confirmSigning(stdin io.Reader, stdout io.Writer) bool {
	_, _ = fmt.Fprint(stdout, "Sign this statement? [y/N] ")
	answer, _ := bufio.NewReader(stdin).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(answer))
	return a == "y" || a == "yes"
}

func writeTrust(out string, doc, sig []byte) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	return errors.Join(
		os.WriteFile(filepath.Join(out, "trust.json"), doc, 0o644),
		os.WriteFile(filepath.Join(out, "trust.json.sig"), sig, 0o644),
	)
}
