package webbundle

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testUpdaterKey builds a minisign public key in the form of tauri.conf.json:
// key id 2f6bbfae9c291e87 in file order, then 32 key bytes.
func testUpdaterKey() string {
	raw := append([]byte("Ed"), 0x2f, 0x6b, 0xbf, 0xae, 0x9c, 0x29, 0x1e, 0x87)
	raw = append(raw, bytes.Repeat([]byte{7}, 32)...)
	text := "untrusted comment: minisign public key: 871E299CAEBF6B2F\n" + base64.StdEncoding.EncodeToString(raw) + "\n"
	return base64.StdEncoding.EncodeToString([]byte(text))
}

func baseTrust(t *testing.T) (Trust, ed25519.PublicKey, ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	rootPub, rootPriv := testKey(t)
	bundlePub, _ := testKey(t)
	return Trust{
		Schema:     TrustSchema,
		Serial:     1,
		IssuedAt:   "2026-10-17T10:00:00Z",
		BundleKeys: []string{hex.EncodeToString(bundlePub)},
	}, rootPub, rootPriv, bundlePub
}

func TestTrustSignVerifyRoundTrip(t *testing.T) {
	tr, rootPub, rootPriv, bundlePub := baseTrust(t)
	tr.UpdaterKeys = []string{testUpdaterKey()}
	doc, sig, err := SignTrust(tr, rootPriv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyTrust(doc, sig, rootPub)
	if err != nil {
		t.Fatal(err)
	}
	if got.Serial != 1 || !got.AllowsBundleKey(bundlePub) {
		t.Fatalf("unexpected statement %+v", got)
	}
}

func TestTrustRefusesWrongKeyAndTampering(t *testing.T) {
	tr, rootPub, rootPriv, _ := baseTrust(t)
	doc, sig, err := SignTrust(tr, rootPriv)
	if err != nil {
		t.Fatal(err)
	}
	otherPub, _ := testKey(t)
	if _, err := VerifyTrust(doc, sig, otherPub); err == nil {
		t.Fatal("a statement verified under another root key")
	}
	tampered := bytes.Replace(doc, []byte(`"serial": 1`), []byte(`"serial": 9`), 1)
	if _, err := VerifyTrust(tampered, sig, rootPub); err == nil {
		t.Fatal("a tampered statement verified")
	}
	badSig := []byte(base64.StdEncoding.EncodeToString(make([]byte, 64)))
	if _, err := VerifyTrust(doc, badSig, rootPub); err == nil {
		t.Fatal("a zero signature verified")
	}
	if _, err := VerifyTrust(doc, []byte("not base64!"), rootPub); err == nil {
		t.Fatal("a malformed signature verified")
	}
}

func TestTrustRevokedKeyIsRefused(t *testing.T) {
	tr, rootPub, rootPriv, oldPub := baseTrust(t)
	newPub, _ := testKey(t)
	tr.Serial = 2
	tr.BundleKeys = []string{hex.EncodeToString(newPub)}
	tr.RevokedBundleKeys = []string{KeyID(oldPub)}
	doc, sig, err := SignTrust(tr, rootPriv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyTrust(doc, sig, rootPub)
	if err != nil {
		t.Fatal(err)
	}
	if got.AllowsBundleKey(oldPub) {
		t.Fatal("the revoked key is still allowed")
	}
	if !got.AllowsBundleKey(newPub) {
		t.Fatal("the new key is not allowed")
	}
}

func TestTrustRejectsInconsistentStatements(t *testing.T) {
	tr, _, rootPriv, pub := baseTrust(t)
	for name, edit := range map[string]func(*Trust){
		"allowed and revoked": func(x *Trust) { x.RevokedBundleKeys = []string{KeyID(pub)} },
		"no bundle key":       func(x *Trust) { x.BundleKeys = nil },
		"serial zero":         func(x *Trust) { x.Serial = 0 },
		"bad time":            func(x *Trust) { x.IssuedAt = "yesterday" },
		"bad key":             func(x *Trust) { x.BundleKeys = []string{"abcd"} },
		"bad revoked id":      func(x *Trust) { x.RevokedBundleKeys = []string{"XYZ"} },
		"bad updater key":     func(x *Trust) { x.UpdaterKeys = []string{"AAAA"} },
		"schema":              func(x *Trust) { x.Schema = 2 },
	} {
		c := tr
		edit(&c)
		if _, _, err := SignTrust(c, rootPriv); err == nil {
			t.Errorf("%s: signed", name)
		}
	}
}

func TestUpdaterKeyID(t *testing.T) {
	id, err := UpdaterKeyID(testUpdaterKey())
	if err != nil {
		t.Fatal(err)
	}
	if id != "2f6bbfae9c291e87" { // minisign shows it reversed: 871E299CAEBF6B2F
		t.Fatalf("id %s", id)
	}
	if _, err := UpdaterKeyID("%%%"); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestCLITrustFlow(t *testing.T) {
	dir := t.TempDir()
	if code, _, e := run([]string{"trust", "keygen", "-out", dir}, nil); code != 0 {
		t.Fatal(e)
	}
	keyFile := filepath.Join(dir, "offline-root.key")
	if st, err := os.Stat(keyFile); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("root key mode: %v %v", st, err)
	}
	if code, _, _ := run([]string{"trust", "keygen", "-out", dir}, nil); code == 0 {
		t.Fatal("keygen overwrote an existing key")
	}
	oldPub, _ := testKey(t)
	newPub, _ := testKey(t)
	out1 := filepath.Join(dir, "1")
	if code, _, e := run([]string{"trust", "sign", "-key-file", keyFile, "-out", out1, "-bundle-key", hex.EncodeToString(oldPub)}, nil); code != 0 {
		t.Fatal(e)
	}
	out2 := filepath.Join(dir, "2")
	if code, _, e := run([]string{"trust", "sign", "-key-file", keyFile, "-out", out2, "-prev", filepath.Join(out1, "trust.json"),
		"-bundle-key", hex.EncodeToString(newPub), "-revoke-bundle-key", KeyID(oldPub)}, nil); code != 0 {
		t.Fatal(e)
	}
	code, out, e := run([]string{"trust", "verify", "-trust", filepath.Join(out2, "trust.json"), "-sig", filepath.Join(out2, "trust.json.sig"), "-pub", filepath.Join(dir, "offline-root.pub")}, nil)
	if code != 0 || !strings.Contains(out, "serial 2") {
		t.Fatalf("verify: %d %s %s", code, out, e)
	}
	// A third statement carries the revocation forward even if not repeated.
	out3 := filepath.Join(dir, "3")
	if code, _, e := run([]string{"trust", "sign", "-key-file", keyFile, "-out", out3, "-prev", filepath.Join(out2, "trust.json"), "-bundle-key", hex.EncodeToString(newPub)}, nil); code != 0 {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(out3, "trust.json"))
	if !strings.Contains(string(b), KeyID(oldPub)) {
		t.Fatal("revocation was not carried forward")
	}
	// Serial may not go back.
	if code, _, _ := run([]string{"trust", "sign", "-key-file", keyFile, "-out", filepath.Join(dir, "4"), "-prev", filepath.Join(out3, "trust.json"), "-serial", "2", "-bundle-key", hex.EncodeToString(newPub)}, nil); code == 0 {
		t.Fatal("a lower serial was signed")
	}
}

// The statement committed for the desktop client must verify under the
// committed root key and keep the current CI signing key allowed.
func TestCommittedTrustStatement(t *testing.T) {
	root, err := readPub("../../desktop-trust/root.pub")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile("../../desktop-trust/trust.json")
	if err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile("../../desktop-trust/trust.json.sig")
	if err != nil {
		t.Fatal(err)
	}
	tr, err := VerifyTrust(doc, sig, root)
	if err != nil {
		t.Fatal(err)
	}
	ci, err := readPub("../../web/desktop-signing.pub")
	if err != nil {
		t.Fatal(err)
	}
	if !tr.AllowsBundleKey(ci) {
		t.Fatal("the key pinned in web/desktop-signing.pub is not allowed by desktop-trust/trust.json")
	}
}

func TestUpdaterKeyIDRejectsMalformedKeys(t *testing.T) {
	wrap := func(text string) string { return base64.StdEncoding.EncodeToString([]byte(text)) }
	good := bytes.Repeat([]byte{1}, 40)
	for name, key := range map[string]string{
		"not base64":      "%%%",
		"one line":        wrap("untrusted comment: x"),
		"line not base64": wrap("c\n%%%\n"),
		"wrong prefix":    wrap("c\n" + base64.StdEncoding.EncodeToString(append([]byte("XX"), good...)) + "\n"),
		"wrong length":    wrap("c\n" + base64.StdEncoding.EncodeToString(append([]byte("Ed"), good[:5]...)) + "\n"),
	} {
		if _, err := UpdaterKeyID(key); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestTrustValidateEachField(t *testing.T) {
	tr, _, rootPriv, _ := baseTrust(t)
	upper := strings.ToUpper(tr.BundleKeys[0])
	updater := testUpdaterKey()
	updaterID, _ := UpdaterKeyID(updater)
	for name, edit := range map[string]func(*Trust){
		"uppercase key":           func(x *Trust) { x.BundleKeys = []string{upper} },
		"bad revoked updater id":  func(x *Trust) { x.RevokedUpdaterKeys = []string{"nothex"} },
		"updater allowed+revoked": func(x *Trust) { x.UpdaterKeys = []string{updater}; x.RevokedUpdaterKeys = []string{updaterID} },
	} {
		c := tr
		edit(&c)
		if _, _, err := SignTrust(c, rootPriv); err == nil {
			t.Errorf("%s: signed", name)
		}
	}
}

func TestVerifyTrustRefusesSignedGarbage(t *testing.T) {
	_, rootPub, rootPriv, _ := baseTrust(t)
	sign := func(doc []byte) []byte {
		return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(rootPriv, doc)))
	}
	for name, doc := range map[string][]byte{
		"not json":      []byte("{"),
		"unknown field": []byte(`{"schema":1,"serial":1,"extra":true}`),
		"invalid":       []byte(`{"schema":1,"serial":1}`),
		"too large":     bytes.Repeat([]byte(" "), maxTrustSize+1),
	} {
		if _, err := VerifyTrust(doc, sign(doc), rootPub); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestAllowsBundleKeyNeedsTheKeyListed(t *testing.T) {
	tr, _, _, _ := baseTrust(t)
	other, _ := testKey(t)
	if tr.AllowsBundleKey(other) {
		t.Fatal("an unlisted key is allowed")
	}
}

func TestCLITrustErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "good")
	if code, _, e := run([]string{"trust", "keygen", "-out", good}, nil); code != 0 {
		t.Fatal(e)
	}
	key := filepath.Join(good, "offline-root.key")
	pub := filepath.Join(good, "offline-root.pub")
	bundle := hex.EncodeToString(bytes.Repeat([]byte{3}, 32))
	signed := filepath.Join(dir, "signed")
	if code, _, e := run([]string{"trust", "sign", "-key-file", key, "-out", signed, "-bundle-key", bundle}, nil); code != 0 {
		t.Fatal(e)
	}
	trustFile, sigFile := filepath.Join(signed, "trust.json"), filepath.Join(signed, "trust.json.sig")
	garbage := filepath.Join(dir, "garbage.json")
	if err := os.WriteFile(garbage, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Output paths that cannot be written: a file as directory, and
	// directories where the files should go.
	blockedPub := filepath.Join(dir, "blocked-pub")
	if err := os.MkdirAll(filepath.Join(blockedPub, "offline-root.pub"), 0o700); err != nil {
		t.Fatal(err)
	}
	blockedSig := filepath.Join(dir, "blocked-sig")
	if err := os.MkdirAll(filepath.Join(blockedSig, "trust.json.sig"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"trust alone":            {"trust"},
		"unknown trust command":  {"trust", "nope"},
		"keygen flag":            {"trust", "keygen", "-nope"},
		"keygen no out":          {"trust", "keygen"},
		"keygen existing key":    {"trust", "keygen", "-out", good},
		"keygen under a file":    {"trust", "keygen", "-out", filepath.Join(file, "sub")},
		"keygen unwritable pub":  {"trust", "keygen", "-out", blockedPub},
		"sign flag":              {"trust", "sign", "-nope"},
		"sign no key":            {"trust", "sign", "-out", dir},
		"sign missing key file":  {"trust", "sign", "-key-file", filepath.Join(dir, "none"), "-out", dir},
		"sign bad key file":      {"trust", "sign", "-key-file", file, "-out", dir},
		"sign missing prev":      {"trust", "sign", "-key-file", key, "-out", dir, "-prev", filepath.Join(dir, "none")},
		"sign bad prev":          {"trust", "sign", "-key-file", key, "-out", dir, "-prev", garbage},
		"sign bad bundle key":    {"trust", "sign", "-key-file", key, "-out", dir, "-bundle-key", file},
		"sign no bundle key":     {"trust", "sign", "-key-file", key, "-out", filepath.Join(dir, "x")},
		"sign out under a file":  {"trust", "sign", "-key-file", key, "-out", filepath.Join(file, "sub"), "-bundle-key", bundle},
		"sign unwritable sig":    {"trust", "sign", "-key-file", key, "-out", blockedSig, "-bundle-key", bundle},
		"verify flag":            {"trust", "verify", "-nope"},
		"verify missing pub":     {"trust", "verify", "-pub", filepath.Join(dir, "none"), "-trust", trustFile, "-sig", sigFile},
		"verify missing trust":   {"trust", "verify", "-pub", pub, "-trust", filepath.Join(dir, "none"), "-sig", sigFile},
		"verify missing sig":     {"trust", "verify", "-pub", pub, "-trust", trustFile, "-sig", filepath.Join(dir, "none")},
		"verify wrong statement": {"trust", "verify", "-pub", pub, "-trust", garbage, "-sig", sigFile},
	} {
		if code, _, _ := run(args, nil); code == 0 {
			t.Errorf("%s: succeeded", name)
		}
	}
	if code, _, e := run([]string{"trust", "verify", "-pub", pub, "-trust", trustFile, "-sig", sigFile}, nil); code != 0 {
		t.Fatal(e)
	}
}
