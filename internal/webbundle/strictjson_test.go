package webbundle

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// TestVerifyReadsManifestJSONLikeTheDesktopClients: signed manifests that
// differ only in JSON spelling. desktopOK is the clients' verdict for the
// same spelling in their shared vectors (test-vectors/json.json and
// bundle.json of mnema-talk-desktop: serde_json with deny_unknown_fields, and
// the Linux client's port of it).
func TestVerifyReadsManifestJSONLikeTheDesktopClients(t *testing.T) {
	pub, priv := testKey(t)
	b, err := Build(okOptions(t, priv))
	if err != nil {
		t.Fatal(err)
	}
	compact, _ := json.Marshal(b.Info)
	plain := string(compact)
	edit := func(old, repl string) string {
		if !strings.Contains(plain, old) {
			t.Fatalf("%q is not in the manifest", old)
		}
		return strings.Replace(plain, old, repl, 1)
	}
	cases := []struct {
		name, manifest string
		desktopOK      bool
	}{
		{"plain", plain, true},
		{"indented", string(b.Manifest), true},
		{"an escaped key", edit(`"commit"`, `"commit"`), true},
		{"a missing commit", edit(`"commit":"abc123",`, ``), false},
		{"commit as null", edit(`"commit":"abc123"`, `"commit":null`), false},
		{"a key in another case", edit(`"commit"`, `"Commit"`), false},
		{"a repeated key", edit(`"commit":"abc123"`, `"commit":"x","commit":"abc123"`), false},
		{"a repeated key written two ways", edit(`"commit":"abc123"`, `"commit":"x","commit":"abc123"`), false},
		{"a repeated key inside a file", edit(`"path":`, `"path":"x","path":`), false},
		{"a key in another case inside a file", edit(`"sha256"`, `"SHA256"`), false},
		{"a second document", plain + plain, false},
		{"a character after the document", plain + "x", false},
		{"a lone surrogate", edit(`"commit":"abc123"`, `"commit":"\ud800"`), false},
		{"a surrogate pair", edit(`"commit":"abc123"`, `"commit":"😀"`), true},
		{"a high surrogate before another escape", edit(`"commit":"abc123"`, `"commit":"\ud83dA"`), false},
		{"a high surrogate at the end of a string", edit(`"commit":"abc123"`, `"commit":"\ud83d"`), false},
		{"an escaped backslash before u", edit(`"commit":"abc123"`, `"commit":"\\ud800"`), true},
		{"a value that is not UTF-8", edit(`"commit":"abc123"`, "\"commit\":\"\xff\""), false},
		{"web_client_api above u32", edit(`"web_client_api":1`, `"web_client_api":4294967297`), false},
		{"schema as 1.0", edit(`"schema":1`, `"schema":1.0`), false},
		{"files as null", plain[:strings.Index(plain, `"files":`)] + `"files":null}`, false},
	}
	for _, c := range cases {
		zip := rebuild(t, b.Zip, func(e map[string][]byte) []string {
			e["manifest.json"] = []byte(c.manifest)
			e["manifest.sig"] = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(c.manifest))))
			return nil
		})
		if _, err := Verify(zip, []ed25519.PublicKey{pub}); (err == nil) != c.desktopOK {
			t.Errorf("%s: Go err=%v, desktop ok=%v", c.name, err, c.desktopOK)
		}
	}
}

func TestTimesBefore1970AreRefused(t *testing.T) {
	for in, ok := range map[string]bool{"1969-12-31T23:59:59Z": false, "0001-01-01T00:00:00Z": false, "1970-01-01T00:00:00Z": true} {
		if _, err := parseUTC(in); (err == nil) != ok {
			t.Errorf("%s: err=%v, want ok=%v", in, err, ok)
		}
	}
}

// TestCapKeysAreReadLikeTheClientsAndWrittenCanonically: a statement the
// clients read with "01" or "+1" verifies, two keys naming one API do not
// (the Rust host would keep one, the Linux client the other), and the signing
// tool writes only the canonical spelling.
func TestCapKeysAreReadLikeTheClientsAndWrittenCanonically(t *testing.T) {
	pub, priv := testKey(t)
	const rest = `"schema":1,"serial":5,"issued_at":"2026-11-01T00:00:00Z","expires_at":"2027-03-01T00:00:00Z","revoked_bundle_keys":[],` +
		`"bundle_keys":["8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c"],"updater_keys":[],"revoked_updater_keys":[]`
	for caps, ok := range map[string]bool{
		`{"01":"1.0.0"}`: true, `{"+1":"1.0.0"}`: true, `{"1":"1.0.0","2":"2.0.0"}`: true,
		`{"1":"9.0.0","01":"1.0.0"}`: false, `{"+1":"9.0.0","1":"1.0.0"}`: false, `{"-1":"1.0.0"}`: false,
		`{"++1":"1.0.0"}`: false, `{"0":"1.0.0"}`: false, `{"4294967296":"1.0.0"}`: false, `{"1":null}`: false,
	} {
		doc := []byte(`{` + rest + `,"max_version":` + caps + `}`)
		sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, doc)))
		if _, err := VerifyTrust(doc, sig, pub); (err == nil) != ok {
			t.Errorf("max_version %s: err=%v, want ok=%v", caps, err, ok)
		}
	}
	tr, _, rootPriv, _ := baseTrust(t)
	tr.MaxVersion = map[string]string{"01": "1.0.0"}
	if _, _, err := SignTrust(tr, rootPriv); err == nil {
		t.Error("SignTrust wrote the cap key 01")
	}
}

// TestEverythingSignTrustWritesVerifies: the lists and the cap map the tool
// leaves unset are written as [] and {}, never as null, which the clients and
// VerifyTrust refuse.
func TestEverythingSignTrustWritesVerifies(t *testing.T) {
	tr, rootPub, rootPriv, _ := baseTrust(t)
	full := tr
	other, _ := testKey(t)
	full.RevokedBundleKeys = []string{KeyID(other)}
	full.UpdaterKeys = []string{testUpdaterKey()}
	full.RevokedUpdaterKeys = []string{"0123456789abcdef"}
	full.MaxVersion = map[string]string{"1": "1.0.0", "2": "2.0.0"}
	for name, s := range map[string]Trust{"unset lists": tr, "every field": full} {
		doc, sig, err := SignTrust(s, rootPriv)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(string(doc), "null") {
			t.Errorf("%s: SignTrust wrote null:\n%s", name, doc)
		}
		if _, err := VerifyTrust(doc, sig, rootPub); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
