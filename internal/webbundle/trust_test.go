package webbundle

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		ExpiresAt:  "2027-04-15T10:00:00Z",
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
		"non-UTC time":        func(x *Trust) { x.IssuedAt = "2026-10-17T12:00:00+02:00" },
		"bad expiry":          func(x *Trust) { x.ExpiresAt = "soon" },
		"expiry before issue": func(x *Trust) { x.ExpiresAt = x.IssuedAt },
		"expiry too far":      func(x *Trust) { x.ExpiresAt = "2030-01-01T00:00:00Z" },
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
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", keyFile, "-out", out1, "-bundle-key", hex.EncodeToString(oldPub)}, nil); code != 0 {
		t.Fatal(e)
	}
	out2 := filepath.Join(dir, "2")
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", keyFile, "-out", out2, "-prev", filepath.Join(out1, "trust.json"),
		"-bundle-key", hex.EncodeToString(newPub), "-revoke-bundle-key", KeyID(oldPub)}, nil); code != 0 {
		t.Fatal(e)
	}
	code, out, e := run([]string{"trust", "verify", "-trust", filepath.Join(out2, "trust.json"), "-sig", filepath.Join(out2, "trust.json.sig"), "-pub", filepath.Join(dir, "offline-root.pub")}, nil)
	if code != 0 || !strings.Contains(out, "serial 2") {
		t.Fatalf("verify: %d %s %s", code, out, e)
	}
	// A third statement carries the revocation forward even if not repeated.
	out3 := filepath.Join(dir, "3")
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", keyFile, "-out", out3, "-prev", filepath.Join(out2, "trust.json"), "-bundle-key", hex.EncodeToString(newPub)}, nil); code != 0 {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(filepath.Join(out3, "trust.json"))
	if !strings.Contains(string(b), KeyID(oldPub)) {
		t.Fatal("revocation was not carried forward")
	}
	// Serial may not go back.
	if code, _, _ := run([]string{"trust", "sign", "-yes", "-key-file", keyFile, "-out", filepath.Join(dir, "4"), "-prev", filepath.Join(out3, "trust.json"), "-serial", "2", "-bundle-key", hex.EncodeToString(newPub)}, nil); code == 0 {
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
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", key, "-out", signed, "-bundle-key", bundle}, nil); code != 0 {
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
		"sign flag":              {"trust", "sign", "-yes", "-nope"},
		"sign no key":            {"trust", "sign", "-yes", "-out", dir},
		"sign missing key file":  {"trust", "sign", "-yes", "-key-file", filepath.Join(dir, "none"), "-out", dir},
		"sign bad key file":      {"trust", "sign", "-yes", "-key-file", file, "-out", dir},
		"sign missing prev":      {"trust", "sign", "-yes", "-key-file", key, "-out", dir, "-prev", filepath.Join(dir, "none")},
		"sign bad prev":          {"trust", "sign", "-yes", "-key-file", key, "-out", dir, "-prev", garbage},
		"sign bad bundle key":    {"trust", "sign", "-yes", "-key-file", key, "-out", dir, "-bundle-key", file},
		"sign no bundle key":     {"trust", "sign", "-yes", "-key-file", key, "-out", filepath.Join(dir, "x")},
		"sign out under a file":  {"trust", "sign", "-yes", "-key-file", key, "-out", filepath.Join(file, "sub"), "-bundle-key", bundle},
		"sign unwritable sig":    {"trust", "sign", "-yes", "-key-file", key, "-out", blockedSig, "-bundle-key", bundle},
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

func TestTrustExpiry(t *testing.T) {
	tr, _, _, _ := baseTrust(t)
	before := time.Date(2027, 4, 15, 9, 59, 59, 0, time.UTC)
	if tr.Expired(before) || !tr.Expired(before.Add(time.Second)) {
		t.Fatal("expiry boundary")
	}
	tr.ExpiresAt = "garbage"
	if !tr.Expired(before) {
		t.Fatal("an unreadable expiry counts as expired")
	}
}

func TestCLIRenewAndExpiry(t *testing.T) {
	dir := t.TempDir()
	if code, _, e := run([]string{"trust", "keygen", "-out", dir}, nil); code != 0 {
		t.Fatal(e)
	}
	key, pub := filepath.Join(dir, "offline-root.key"), filepath.Join(dir, "offline-root.pub")
	bundle := hex.EncodeToString(bytes.Repeat([]byte{4}, 32))
	one := filepath.Join(dir, "1")
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", key, "-out", one, "-bundle-key", bundle, "-updater-key", testUpdaterKey()}, nil); code != 0 {
		t.Fatal(e)
	}
	two := filepath.Join(dir, "2")
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", key, "-out", two, "-prev", filepath.Join(one, "trust.json"), "-renew"}, nil); code != 0 {
		t.Fatal(e)
	}
	doc, _ := os.ReadFile(filepath.Join(two, "trust.json"))
	sig, _ := os.ReadFile(filepath.Join(two, "trust.json.sig"))
	pk, _ := readPub(pub)
	tr, err := VerifyTrust(doc, sig, pk)
	if err != nil || tr.Serial != 2 || len(tr.BundleKeys) != 1 || len(tr.UpdaterKeys) != 1 {
		t.Fatalf("renewed statement: %+v %v", tr, err)
	}
	// Without -renew the allowed keys are not carried over.
	if code, _, _ := run([]string{"trust", "sign", "-yes", "-key-file", key, "-out", filepath.Join(dir, "3"), "-prev", filepath.Join(one, "trust.json")}, nil); code == 0 {
		t.Fatal("a statement without bundle keys was signed")
	}
	// An already expired statement fails verification with a hint.
	old := filepath.Join(dir, "old")
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", key, "-out", old, "-bundle-key", bundle, "-valid-for", "1s"}, nil); code != 0 {
		t.Fatal(e)
	}
	time.Sleep(1100 * time.Millisecond)
	code, _, e := run([]string{"trust", "verify", "-pub", pub, "-trust", filepath.Join(old, "trust.json"), "-sig", filepath.Join(old, "trust.json.sig")}, nil)
	if code == 0 || !strings.Contains(e, "expired") {
		t.Fatalf("expired statement verified: %d %s", code, e)
	}
}

func runIn(stdin string, args []string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(args, func(string) string { return "" }, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestSignRefusesAnUnverifiedPrevious(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"root", "other"} {
		if code, _, e := run([]string{"trust", "keygen", "-out", filepath.Join(dir, d)}, nil); code != 0 {
			t.Fatal(e)
		}
	}
	key := filepath.Join(dir, "root", "offline-root.key")
	otherKey := filepath.Join(dir, "other", "offline-root.key")
	good := hex.EncodeToString(bytes.Repeat([]byte{1}, 32))
	evil := hex.EncodeToString(bytes.Repeat([]byte{9}, 32))
	one := filepath.Join(dir, "1")
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", key, "-out", one, "-bundle-key", good}, nil); code != 0 {
		t.Fatal(e)
	}
	prev := filepath.Join(one, "trust.json")
	args := func(extra ...string) []string {
		return append([]string{"trust", "sign", "-yes", "-key-file", key, "-out", filepath.Join(dir, "out"), "-renew", "-prev", prev}, extra...)
	}
	// A tampered previous statement: someone added their key.
	raw, _ := os.ReadFile(prev)
	tampered := filepath.Join(dir, "tampered.json")
	_ = os.WriteFile(tampered, bytes.Replace(raw, []byte(good), []byte(evil), 1), 0o600)
	sig, _ := os.ReadFile(prev + ".sig")
	_ = os.WriteFile(tampered+".sig", sig, 0o600)
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", key, "-out", filepath.Join(dir, "out"), "-renew", "-prev", tampered}, nil); code == 0 || !strings.Contains(e, "does not verify") {
		t.Fatalf("tampered prev: %d %s", code, e)
	}
	// No signature next to it.
	lone := filepath.Join(dir, "lone.json")
	_ = os.WriteFile(lone, raw, 0o600)
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", key, "-out", filepath.Join(dir, "out"), "-prev", lone}, nil); code == 0 || !strings.Contains(e, "needs its signature") {
		t.Fatalf("missing sig: %d %s", code, e)
	}
	// Signed by another root key.
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", otherKey, "-out", filepath.Join(dir, "x"), "-prev", prev}, nil); code == 0 {
		t.Fatalf("wrong root accepted: %s", e)
	}
	if _, err := os.Stat(filepath.Join(dir, "out")); err == nil {
		t.Fatal("something was written for a refused previous statement")
	}
	// An explicit -prev-sig works.
	moved := filepath.Join(dir, "sig-elsewhere")
	_ = os.WriteFile(moved, sig, 0o600)
	if code, _, e := run(args("-prev-sig", moved), nil); code != 0 {
		t.Fatal(e)
	}
}

func TestSignShowsTheResultAndNeedsConfirmation(t *testing.T) {
	dir := t.TempDir()
	if code, _, e := run([]string{"trust", "keygen", "-out", dir}, nil); code != 0 {
		t.Fatal(e)
	}
	key := filepath.Join(dir, "offline-root.key")
	oldK, newK := hex.EncodeToString(bytes.Repeat([]byte{1}, 32)), hex.EncodeToString(bytes.Repeat([]byte{2}, 32))
	one := filepath.Join(dir, "1")
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", key, "-out", one, "-bundle-key", oldK, "-updater-key", testUpdaterKey()}, nil); code != 0 {
		t.Fatal(e)
	}
	oldRaw, _ := hex.DecodeString(oldK)
	two := filepath.Join(dir, "2")
	base := []string{"trust", "sign", "-key-file", key, "-out", two, "-prev", filepath.Join(one, "trust.json"), "-renew",
		"-bundle-key", newK, "-revoke-bundle-key", KeyID(oldRaw), "-max-version", "1=0.9.0"}
	// Declined (and an empty answer): nothing is written.
	for _, answer := range []string{"n\n", ""} {
		code, out, _ := runIn(answer, base)
		if code == 0 {
			t.Fatal("signed without confirmation")
		}
		if _, err := os.Stat(two); err == nil {
			t.Fatal("written without confirmation")
		}
		for _, want := range []string{"+ ", "- ", KeyID(oldRaw), "rollback cap: web client API 1 at most 0.9.0", "allowed updater keys"} {
			if !strings.Contains(out, want) {
				t.Errorf("summary lacks %q:\n%s", want, out)
			}
		}
	}
	// The revoked key is not carried over by -renew; confirmed with y.
	if code, _, e := runIn("y\n", base); code != 0 {
		t.Fatal(e)
	}
	doc, _ := os.ReadFile(filepath.Join(two, "trust.json"))
	if strings.Contains(string(doc), oldK) || !strings.Contains(string(doc), newK) || !strings.Contains(string(doc), `"1": "0.9.0"`) {
		t.Fatalf("unexpected statement:\n%s", doc)
	}
	// A third statement drops the cap (caps are not carried) but keeps revocations.
	three := filepath.Join(dir, "3")
	if code, _, e := run([]string{"trust", "sign", "-yes", "-key-file", key, "-out", three, "-prev", filepath.Join(two, "trust.json"), "-renew"}, nil); code != 0 {
		t.Fatal(e)
	}
	doc3, _ := os.ReadFile(filepath.Join(three, "trust.json"))
	if strings.Contains(string(doc3), "0.9.0") || !strings.Contains(string(doc3), KeyID(oldRaw)) {
		t.Fatalf("unexpected renewal:\n%s", doc3)
	}
	// Bad caps.
	for _, bad := range []string{"nocap", "x=0.9.0", "0=0.9.0", "01=0.9.0", "1=latest"} {
		if code, _, _ := run([]string{"trust", "sign", "-yes", "-key-file", key, "-out", filepath.Join(dir, "bad"), "-bundle-key", newK, "-max-version", bad}, nil); code == 0 {
			t.Errorf("-max-version %s accepted", bad)
		}
	}
}

type deadWriter struct{}

func (deadWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

func TestTrustSignSucceedsWhenTheStatusLineCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	if code, _, e := run([]string{"trust", "keygen", "-out", dir}, nil); code != 0 {
		t.Fatal(e)
	}
	pub, _ := testKey(t)
	out := filepath.Join(dir, "signed")
	args := []string{"-yes", "-key-file", filepath.Join(dir, "offline-root.key"), "-out", out, "-bundle-key", hex.EncodeToString(pub)}
	if err := runTrustSign(args, strings.NewReader(""), deadWriter{}, io.Discard); err != nil {
		t.Fatalf("signing failed because stdout was gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "trust.json")); err != nil {
		t.Fatalf("trust statement not written: %v", err)
	}
}
