package webbundle

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func otherAlgorithmKeys(t *testing.T) (privPEM, pubPEM []byte) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	priv, _ := x509.MarshalPKCS8PrivateKey(k)
	pub, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: priv}), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pub})
}

func run(args []string, env map[string]string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(args, func(k string) string { return env[k] }, &out, &errb)
	return code, out.String(), errb.String()
}

func TestCLIBuildVerifyKeyID(t *testing.T) {
	pub, priv := testKey(t)
	privDER, _ := x509.MarshalPKCS8PrivateKey(priv)
	pubDER, _ := x509.MarshalPKIXPublicKey(pub)
	privPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}))
	dir := t.TempDir()
	pubFile := filepath.Join(dir, "pub.pem")
	keyFile := filepath.Join(dir, "key.pem")
	compat := filepath.Join(dir, "compat.json")
	for p, body := range map[string]string{
		pubFile: string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})),
		keyFile: privPEM,
		compat:  `{"web_client_api":1,"min_server_version":"0.7.16","security_floor":"0.0.0"}`,
	} {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tree := writeTree(t, map[string]string{"index.html": "x", "a.js.map": "m"})
	out := filepath.Join(dir, "out")
	build := []string{"build", "-dir", tree, "-compat", compat, "-version", "0.8.0", "-commit", "c", "-out", out}

	// Key from the environment.
	code, stdout, stderr := run(build, map[string]string{"WEB_BUNDLE_SIGNING_KEY": privPEM})
	if code != 0 || !strings.Contains(stdout, "bundle 0.8.0: 1 files") {
		t.Fatalf("build: %d %q %q", code, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "PRIVATE KEY") {
		t.Fatal("the key was printed")
	}
	for _, name := range []string{"mnema-web-0.8.0.zip", "manifest.json", "manifest.sig"} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Fatal(err)
		}
	}
	// Key from a file, other variable name.
	code, _, stderr = run(append([]string{}, append(build, "-key-file", keyFile)...), nil)
	if code != 0 {
		t.Fatalf("build with key file: %d %q", code, stderr)
	}
	code, _, _ = run(append(append([]string{}, build...), "-key-env", "OTHER"), map[string]string{"OTHER": privPEM})
	if code != 0 {
		t.Fatal("build with a custom variable name")
	}

	code, stdout, stderr = run([]string{"verify", "-zip", filepath.Join(out, "mnema-web-0.8.0.zip"), "-pub", pubFile}, nil)
	if code != 0 || !strings.HasPrefix(stdout, "ok: version 0.8.0") {
		t.Fatalf("verify: %d %q %q", code, stdout, stderr)
	}
	code, stdout, _ = run([]string{"keyid", "-pub", pubFile}, nil)
	if code != 0 || strings.TrimSpace(stdout) != KeyID(pub) {
		t.Fatalf("keyid: %d %q", code, stdout)
	}

	// -security-floor raises the compat floor, never lowers it, and may not
	// exceed the bundle's own version.
	floorOut := filepath.Join(dir, "floor")
	code, _, stderr = run(append(append([]string{}, build...), "-out", floorOut, "-security-floor", "0.8.0", "-key-file", keyFile), nil)
	if code != 0 {
		t.Fatalf("build with a floor: %d %q", code, stderr)
	}
	if raw, _ := os.ReadFile(filepath.Join(floorOut, "manifest.json")); !strings.Contains(string(raw), `"security_floor": "0.8.0"`) {
		t.Fatalf("floor not raised: %s", raw)
	}
	lowerCompat := filepath.Join(dir, "compat-high.json")
	_ = os.WriteFile(lowerCompat, []byte(`{"web_client_api":1,"min_server_version":"0.7.16","security_floor":"0.7.5"}`), 0o600)
	lowOut := filepath.Join(dir, "low")
	code, _, stderr = run([]string{"build", "-dir", tree, "-compat", lowerCompat, "-version", "0.8.0", "-out", lowOut, "-security-floor", "0.7.1", "-key-file", keyFile}, nil)
	if code != 0 {
		t.Fatalf("build with a lower floor: %d %q", code, stderr)
	}
	if raw, _ := os.ReadFile(filepath.Join(lowOut, "manifest.json")); !strings.Contains(string(raw), `"security_floor": "0.7.5"`) {
		t.Fatalf("floor lowered: %s", raw)
	}
	if code, _, _ = run(append(append([]string{}, build...), "-out", floorOut, "-security-floor", "0.9.0", "-key-file", keyFile), nil); code == 0 {
		t.Fatal("a floor above the bundle version was accepted")
	}
	if code, _, _ = run(append(append([]string{}, build...), "-out", floorOut, "-security-floor", "x", "-key-file", keyFile), nil); code == 0 {
		t.Fatal("a malformed floor was accepted")
	}

	// Failures.
	_, otherPub := otherAlgorithmKeys(t)
	badPub := filepath.Join(dir, "bad.pem")
	_ = os.WriteFile(badPub, otherPub, 0o600)
	zipPath := filepath.Join(out, "mnema-web-0.8.0.zip")
	failing := map[string][]string{
		"no command":          nil,
		"unknown command":     {"sign"},
		"build no out":        {"build"},
		"build bad flag":      {"build", "-nope"},
		"build help":          {"build", "-h"},
		"build no key":        {"build", "-out", out, "-dir", tree, "-compat", compat, "-version", "0.8.0"},
		"build missing file":  append(append([]string{}, build...), "-key-file", filepath.Join(dir, "none")),
		"build bad key":       append(append([]string{}, build...), "-key-file", compat),
		"build bad compat":    {"build", "-out", out, "-dir", tree, "-compat", filepath.Join(dir, "none"), "-version", "0.8.0", "-key-file", keyFile},
		"build bad version":   {"build", "-out", out, "-dir", tree, "-compat", compat, "-version", "x", "-key-file", keyFile},
		"build out is a file": {"build", "-out", compat, "-dir", tree, "-compat", compat, "-version", "0.8.0", "-key-file", keyFile},
		"verify bad flag":     {"verify", "-nope"},
		"verify no pub":       {"verify", "-zip", zipPath, "-pub", filepath.Join(dir, "none")},
		"verify no zip":       {"verify", "-zip", filepath.Join(dir, "none"), "-pub", pubFile},
		"verify wrong key":    {"verify", "-zip", zipPath, "-pub", badPub},
		"verify not a zip":    {"verify", "-zip", compat, "-pub", pubFile},
		"keyid bad flag":      {"keyid", "-nope"},
		"keyid no pub":        {"keyid", "-pub", filepath.Join(dir, "none")},
	}
	for name, args := range failing {
		t.Run(name, func(t *testing.T) {
			if code, _, _ := run(args, nil); code == 0 {
				t.Fatal("expected a failure")
			}
		})
	}
	t.Run("output cannot be written", func(t *testing.T) {
		ro := filepath.Join(dir, "ro")
		_ = os.MkdirAll(ro, 0o755)
		_ = os.Chmod(ro, 0o500)
		defer func() { _ = os.Chmod(ro, 0o700) }()
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		args := append(append([]string{}, build[:len(build)-1]...), ro, "-key-file", keyFile)
		if code, _, _ := run(args, nil); code == 0 {
			t.Fatal("expected a failure")
		}
	})
}
