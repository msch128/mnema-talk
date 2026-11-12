package webbundle

// The desktop client implements the bundle and statement format twice (Rust
// for Windows and macOS, TypeScript for Linux) and checks both against shared
// vectors in its own repository (test-vectors/). This test reads the same
// vectors and checks that this package, which signs what those clients
// verify, accepts and refuses the same inputs. It runs where a checkout of
// mnema-talk-desktop exists: MNEMA_DESKTOP_VECTORS=<checkout>/test-vectors.

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// knownDifferences are the inputs on which this package and the desktop
// clients disagree. Hunt finding H-L11-03 listed nine (a manifest without
// "commit", a time before 1970, keys in another case, duplicate keys, missing
// or null lists, the cap keys "01" and "+1"); strictjson.go closed them. The
// list may only shrink: the test turns red when a listed input agrees, and
// when an unlisted one disagrees.
var knownDifferences = map[string]bool{}

// agree reports a disagreement unless it is a known one, and a known one that
// no longer disagrees.
func agree(t *testing.T, name string, same bool, detail string) bool {
	t.Helper()
	switch {
	case knownDifferences[name] && same:
		t.Errorf("%s: Go and the desktop clients agree now; remove it from knownDifferences", name)
	case !knownDifferences[name] && !same:
		t.Errorf("%s: %s", name, detail)
	}
	return same
}

type vectorBundle struct {
	Name    string                     `json:"name"`
	Trusted []string                   `json:"trusted"`
	Zip     string                     `json:"zip"`
	Result  map[string]json.RawMessage `json:"result"`
}

type vectorStatement struct {
	Name   string                     `json:"name"`
	Doc    string                     `json:"doc"`
	Sig    string                     `json:"sig"`
	Now    int64                      `json:"now"`
	Verify map[string]json.RawMessage `json:"verify"`
}

func readVectors(t *testing.T, dir, name string, into any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func vectorsDir(t *testing.T) string {
	dir := os.Getenv("MNEMA_DESKTOP_VECTORS")
	if dir == "" {
		t.Skip("MNEMA_DESKTOP_VECTORS not set (a mnema-talk-desktop checkout's test-vectors directory)")
	}
	return dir
}

func TestDesktopBundleVectorsAgreeWithGo(t *testing.T) {
	dir := vectorsDir(t)
	var v struct {
		Versions []struct {
			Input  string    `json:"input"`
			Parsed *[]uint32 `json:"parsed"`
		} `json:"versions"`
		Paths []struct {
			Path  string `json:"path"`
			Valid bool   `json:"valid"`
		} `json:"paths"`
		Bundles []vectorBundle `json:"bundles"`
	}
	readVectors(t, dir, "bundle.json", &v)
	for _, c := range v.Versions {
		if _, err := ParseVersion(c.Input); (err == nil) != (c.Parsed != nil) {
			t.Errorf("version %q: Go parses=%v, desktop parses=%v", c.Input, err == nil, c.Parsed != nil)
		}
	}
	for _, c := range v.Paths {
		if ValidPath(c.Path) != c.Valid {
			t.Errorf("path %q: Go valid=%v, desktop valid=%v", c.Path, ValidPath(c.Path), c.Valid)
		}
	}
	for _, c := range v.Bundles {
		checkBundleVector(t, c)
	}
	if len(v.Bundles) == 0 || len(v.Versions) == 0 || len(v.Paths) == 0 {
		t.Fatal("no vectors read")
	}
	t.Logf("%d versions, %d paths, %d bundles compared", len(v.Versions), len(v.Paths), len(v.Bundles))
}

func checkBundleVector(t *testing.T, c vectorBundle) {
	var keys []ed25519.PublicKey
	for _, h := range c.Trusted {
		raw, err := hex.DecodeString(h)
		if err != nil {
			t.Fatalf("%s: key %q", c.Name, h)
		}
		keys = append(keys, raw)
	}
	zipBytes, err := base64.StdEncoding.DecodeString(c.Zip)
	if err != nil {
		t.Fatalf("%s: zip is not base64", c.Name)
	}
	m, err := Verify(zipBytes, keys)
	_, desktopOK := c.Result["ok"]
	if !agree(t, "bundle "+c.Name, (err == nil) == desktopOK, fmt.Sprintf("Go err=%v, desktop ok=%v", err, desktopOK)) || err != nil {
		return
	}
	var want struct {
		Version       string `json:"version"`
		SecurityFloor string `json:"security_floor"`
		KeyID         string `json:"key_id"`
	}
	if json.Unmarshal(c.Result["ok"], &want) == nil && (want.Version != m.Version || want.SecurityFloor != m.SecurityFloor || want.KeyID != m.KeyID) {
		t.Errorf("bundle %q: Go reads %s/%s/%s, desktop %s/%s/%s", c.Name, m.Version, m.SecurityFloor, m.KeyID, want.Version, want.SecurityFloor, want.KeyID)
	}
}

func TestDesktopTrustVectorsAgreeWithGo(t *testing.T) {
	dir := vectorsDir(t)
	var v struct {
		Root  string `json:"root"`
		Times []struct {
			Input string `json:"input"`
			Secs  *int64 `json:"secs"`
		} `json:"times"`
		Statements []vectorStatement `json:"statements"`
	}
	readVectors(t, dir, "trust.json", &v)
	root, err := hex.DecodeString(v.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range v.Times {
		checkTimeVector(t, c.Input, c.Secs)
	}
	for _, c := range v.Statements {
		got, err := VerifyTrust([]byte(c.Doc), []byte(c.Sig), root)
		_, desktopOK := c.Verify["ok"]
		if (err == nil) != desktopOK {
			t.Errorf("statement %q: Go err=%v, desktop ok=%v", c.Name, err, desktopOK)
			continue
		}
		if err == nil && got.Expired(time.Unix(c.Now, 0)) != (c.Name == "expired") {
			t.Errorf("statement %q: Go expired=%v at %d", c.Name, got.Expired(time.Unix(c.Now, 0)), c.Now)
		}
	}
	if len(v.Statements) == 0 || len(v.Times) == 0 {
		t.Fatal("no vectors read")
	}
	t.Logf("%d times, %d statements compared", len(v.Times), len(v.Statements))
}

// TestDesktopJSONVectorsAgreeWithGo: json.json holds signed manifests and
// statements that differ only in JSON spelling (repeated and escaped keys,
// null, numbers as 1.0 or "1", trailing data, invalid UTF-8, ...).
func TestDesktopJSONVectorsAgreeWithGo(t *testing.T) {
	dir := vectorsDir(t)
	var v struct {
		Root       string            `json:"root"`
		Bundles    []vectorBundle    `json:"bundles"`
		Statements []vectorStatement `json:"statements"`
	}
	readVectors(t, dir, "json.json", &v)
	root, err := hex.DecodeString(v.Root)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range v.Bundles {
		checkBundleVector(t, c)
	}
	for _, c := range v.Statements {
		doc, err := base64.StdEncoding.DecodeString(c.Doc)
		if err != nil {
			t.Fatalf("%s: doc is not base64", c.Name)
		}
		_, err = VerifyTrust(doc, []byte(c.Sig), root)
		_, desktopOK := c.Verify["ok"]
		agree(t, "json statement "+c.Name, (err == nil) == desktopOK, fmt.Sprintf("Go err=%v, desktop ok=%v", err, desktopOK))
	}
	if len(v.Bundles) == 0 || len(v.Statements) == 0 {
		t.Fatal("no vectors read")
	}
	t.Logf("%d bundles, %d statements compared", len(v.Bundles), len(v.Statements))
}

func checkTimeVector(t *testing.T, input string, secs *int64) {
	got, err := parseUTC(input)
	if !agree(t, "time "+input, (err == nil) == (secs != nil), fmt.Sprintf("Go parses=%v, desktop parses=%v", err == nil, secs != nil)) {
		return
	}
	if err == nil && got.Unix() != *secs {
		t.Errorf("time %q: Go %d, desktop %d", input, got.Unix(), *secs)
	}
}

// TestVerifyTrustReadsJSONLikeTheDesktopClients: statements that differ only
// in JSON spelling, signed by a root key. desktopOK is what the Linux client
// (linux/src/core/trust.ts) answered when probed with these documents; the
// Rust host reads them with serde (deny_unknown_fields, required lists, a map
// that refuses duplicates). Where Go answers otherwise the case is in
// knownDifferences (H-L11-03).
func TestVerifyTrustReadsJSONLikeTheDesktopClients(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	const keys = `"bundle_keys":["8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c"],"updater_keys":[],"revoked_updater_keys":[]`
	const head = `"schema":1,"issued_at":"2026-11-01T00:00:00Z","expires_at":"2027-03-01T00:00:00Z",`
	const revoked = `"revoked_bundle_keys":[],`
	cases := []struct {
		name, doc string
		desktopOK bool
	}{
		{"plain", `{"serial":5,` + head + revoked + keys + `}`, true},
		{"upper-case key", `{"Serial":5,` + head + revoked + keys + `}`, false},
		{"duplicate serial", `{"serial":9,"serial":5,` + head + revoked + keys + `}`, false},
		{"duplicate cap", `{"serial":5,` + head + revoked + keys + `,"max_version":{"1":"9.0.0","1":"1.0.0"}}`, false},
		{"missing revoked list", `{"serial":5,` + head + keys + `}`, false},
		{"null revoked list", `{"serial":5,` + head + `"revoked_bundle_keys":null,` + keys + `}`, false},
		{"cap api 01", `{"serial":5,` + head + revoked + keys + `,"max_version":{"01":"1.0.0"}}`, true},
		{"cap api +1", `{"serial":5,` + head + revoked + keys + `,"max_version":{"+1":"1.0.0"}}`, true},
		{"serial as float", `{"serial":5.0,` + head + revoked + keys + `}`, false},
	}
	for _, c := range cases {
		sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(c.doc))))
		_, err := VerifyTrust([]byte(c.doc), sig, pub)
		agree(t, "statement "+c.name, (err == nil) == c.desktopOK, fmt.Sprintf("Go err=%v, desktop ok=%v", err, c.desktopOK))
	}
}
