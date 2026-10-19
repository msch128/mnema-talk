package webbundle

import (
	"crypto/ed25519"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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
