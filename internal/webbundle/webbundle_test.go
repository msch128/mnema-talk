package webbundle

import (
	"archive/zip"
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

var okCompat = Compat{WebClientAPI: 1, MinServerVersion: "0.7.16", SecurityFloor: "0.0.0"}

func okOptions(t *testing.T, key ed25519.PrivateKey) BuildOptions {
	t.Helper()
	return BuildOptions{
		Dir: writeTree(t, map[string]string{
			"index.html":         "<html></html>",
			"assets/app-1.js":    "console.log(1)",
			"assets/app-1.js.br": "dropped",
			"assets/app-1.js.gz": "dropped",
			"assets/app.js.map":  "dropped",
			"fonts/inter.woff2":  "font",
		}),
		Compat:  okCompat,
		Version: "0.8.0",
		Commit:  "abc123",
		Key:     key,
		Exclude: []string{".br", ".gz", ".map"},
	}
}

func TestBuildAndVerifyRoundTrip(t *testing.T) {
	pub, priv := testKey(t)
	b, err := Build(okOptions(t, priv))
	if err != nil {
		t.Fatal(err)
	}
	if b.Info.KeyID != KeyID(pub) || len(b.Info.Files) != 3 {
		t.Fatalf("unexpected manifest: %+v", b.Info)
	}
	for _, f := range b.Info.Files {
		if strings.HasSuffix(f.Path, ".br") || strings.HasSuffix(f.Path, ".map") {
			t.Fatalf("excluded file %s was bundled", f.Path)
		}
	}
	m, err := Verify(b.Zip, []ed25519.PublicKey{pub})
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "0.8.0" || m.WebClientAPI != 1 || m.MinServerVersion != "0.7.16" || m.Commit != "abc123" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	_, priv := testKey(t)
	o := okOptions(t, priv)
	a, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(o)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Zip, b.Zip) {
		t.Fatal("same input produced different bundles")
	}
}

func TestBuildRejectsBadInput(t *testing.T) {
	_, priv := testKey(t)
	cases := map[string]func(*BuildOptions){
		"version":        func(o *BuildOptions) { o.Version = "v1" },
		"compat":         func(o *BuildOptions) { o.Compat.WebClientAPI = 0 },
		"key":            func(o *BuildOptions) { o.Key = nil },
		"min newer":      func(o *BuildOptions) { o.Compat.MinServerVersion = "0.9.0" },
		"floor newer":    func(o *BuildOptions) { o.Compat.SecurityFloor = "0.9.0" },
		"missing dir":    func(o *BuildOptions) { o.Dir = filepath.Join(o.Dir, "nope") },
		"no index":       func(o *BuildOptions) { o.Dir = writeTree(t, map[string]string{"a.js": "x"}) },
		"invalid path":   func(o *BuildOptions) { o.Dir = writeTree(t, map[string]string{"index.html": "x", "bad\\name.js": "x"}) },
		"empty tree":     func(o *BuildOptions) { o.Dir = t.TempDir() },
		"symlink":        func(o *BuildOptions) { _ = os.Symlink("index.html", filepath.Join(o.Dir, "link.html")) },
		"unreadable":     func(o *BuildOptions) { _ = os.Chmod(filepath.Join(o.Dir, "index.html"), 0) },
		"excluded index": func(o *BuildOptions) { o.Exclude = []string{".html"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			o := okOptions(t, priv)
			mutate(&o)
			if _, err := Build(o); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestBuildSizeLimits(t *testing.T) {
	_, priv := testKey(t)
	o := okOptions(t, priv)
	big := make([]byte, MaxFileSize+1)
	if err := os.WriteFile(filepath.Join(o.Dir, "big.bin"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(o); err == nil {
		t.Fatal("expected the file size limit to apply")
	}
}

func TestBuildEntryLimit(t *testing.T) {
	_, priv := testKey(t)
	o := okOptions(t, priv)
	for i := 0; i < MaxEntries; i++ {
		if err := os.WriteFile(filepath.Join(o.Dir, "f"+strings.Repeat("x", 1)+string(rune('a'+i%26))+strings.Repeat("y", i/26)+".txt"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Build(o); err == nil {
		t.Fatal("expected the entry limit to apply")
	}
}

// rebuild makes a zip from the entries of a valid bundle after mutate edited
// them, so the tests can produce each kind of broken bundle.
func rebuild(t *testing.T, good []byte, mutate func(map[string][]byte) []string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(good), int64(len(good)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	var order []string
	for _, f := range zr.File {
		rc, _ := f.Open()
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		_ = rc.Close()
		entries[f.Name] = b.Bytes()
		order = append(order, f.Name)
	}
	if o := mutate(entries); o != nil {
		order = o
	} else {
		kept := order[:0]
		for _, n := range order {
			if _, ok := entries[n]; ok {
				kept = append(kept, n)
			}
		}
		order = kept
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, name := range order {
		w, _ := zw.Create(name)
		_, _ = w.Write(entries[name])
	}
	_ = zw.Close()
	return out.Bytes()
}

// resign replaces the manifest and signs it again with key.
func resign(entries map[string][]byte, m Manifest, key ed25519.PrivateKey) {
	raw, _ := json.Marshal(m)
	entries["manifest.json"] = raw
	entries["manifest.sig"] = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, raw)))
}

func TestVerifyRejectsTampering(t *testing.T) {
	pub, priv := testKey(t)
	otherPub, otherPriv := testKey(t)
	b, err := Build(okOptions(t, priv))
	if err != nil {
		t.Fatal(err)
	}
	keys := []ed25519.PublicKey{pub}
	good := b.Info

	signed := func(edit func(*Manifest)) func(map[string][]byte) []string {
		return func(e map[string][]byte) []string {
			m := good
			m.Files = append([]File(nil), good.Files...)
			edit(&m)
			resign(e, m, priv)
			return nil
		}
	}
	cases := map[string]func(map[string][]byte) []string{
		"changed file": func(e map[string][]byte) []string { e["app/index.html"] = []byte("<html>evil</html>"); return nil },
		"extra file": func(e map[string][]byte) []string {
			e["app/evil.js"] = []byte("x")
			return append(entryNames(e), "app/evil.js")
		},
		"missing file": func(e map[string][]byte) []string { delete(e, "app/assets/app-1.js"); return nil },
		"swapped file": func(e map[string][]byte) []string {
			delete(e, "app/assets/app-1.js")
			e["app/assets/other.js"] = []byte("x")
			return entryNames(e)
		},
		"no manifest":  func(e map[string][]byte) []string { delete(e, "manifest.json"); return nil },
		"no signature": func(e map[string][]byte) []string { delete(e, "manifest.sig"); return nil },
		"edited manifest": func(e map[string][]byte) []string {
			e["manifest.json"] = bytes.Replace(e["manifest.json"], []byte(`"0.8.0"`), []byte(`"9.9.9"`), 1)
			return nil
		},
		"signature not base64": func(e map[string][]byte) []string { e["manifest.sig"] = []byte("***"); return nil },
		"signature too short":  func(e map[string][]byte) []string { e["manifest.sig"] = []byte("AAAA"); return nil },
		"wrong key": func(e map[string][]byte) []string {
			resign(e, good, otherPriv)
			return nil
		},
		"duplicate entry": func(e map[string][]byte) []string { return append(entryNames(e), "app/index.html") },
		"oversized manifest": func(e map[string][]byte) []string {
			e["manifest.json"] = make([]byte, maxManifestSize+1)
			return nil
		},
		"oversized signature": func(e map[string][]byte) []string { e["manifest.sig"] = make([]byte, 2000); return nil },
		"unknown field": func(e map[string][]byte) []string {
			raw := append([]byte(`{"surprise":1,`), e["manifest.json"][1:]...)
			e["manifest.json"] = raw
			e["manifest.sig"] = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, raw)))
			return nil
		},
		"not json": func(e map[string][]byte) []string {
			e["manifest.json"] = []byte("nope")
			e["manifest.sig"] = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte("nope"))))
			return nil
		},
		"bad schema":     signed(func(m *Manifest) { m.Schema = 2 }),
		"bad name":       signed(func(m *Manifest) { m.Name = "other" }),
		"bad key id":     signed(func(m *Manifest) { m.KeyID = "0000000000000000" }),
		"bad version":    signed(func(m *Manifest) { m.Version = "1" }),
		"bad compat":     signed(func(m *Manifest) { m.WebClientAPI = 0 }),
		"min after ver":  signed(func(m *Manifest) { m.MinServerVersion = "9.0.0" }),
		"no files":       signed(func(m *Manifest) { m.Files = nil }),
		"bad path":       signed(func(m *Manifest) { m.Files[0].Path = "../x" }),
		"duplicate path": signed(func(m *Manifest) { m.Files[1].Path = m.Files[0].Path }),
		"bad hash":       signed(func(m *Manifest) { m.Files[0].SHA256 = "zz" }),
		"short hash":     signed(func(m *Manifest) { m.Files[0].SHA256 = "00" }),
		"negative size":  signed(func(m *Manifest) { m.Files[0].Size = -1 }),
		"huge size":      signed(func(m *Manifest) { m.Files[0].Size = MaxFileSize + 1 }),
		"huge total": signed(func(m *Manifest) {
			m.Files = append([]File(nil), m.Files...)
			for i := range m.Files {
				m.Files[i].Size = MaxFileSize
			}
			m.Files = append(m.Files, File{Path: "z1", SHA256: m.Files[0].SHA256, Size: MaxFileSize}, File{Path: "z2", SHA256: m.Files[0].SHA256, Size: MaxFileSize})
		}),
		"no index": signed(func(m *Manifest) {
			for i, f := range m.Files {
				if f.Path == "index.html" {
					m.Files[i].Path = "home.html"
				}
			}
		}),
		"wrong size": signed(func(m *Manifest) { m.Files[0].Size++ }),
		"wrong hash": signed(func(m *Manifest) { m.Files[0].SHA256 = strings.Repeat("0", 64) }),
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			data := rebuild(t, b.Zip, mutate)
			if _, err := Verify(data, keys); err == nil {
				t.Fatal("tampered bundle was accepted")
			}
		})
	}
	t.Run("untrusted key", func(t *testing.T) {
		if _, err := Verify(b.Zip, []ed25519.PublicKey{otherPub, {1, 2}}); err == nil {
			t.Fatal("bundle accepted without its key")
		}
	})
	t.Run("second key verifies", func(t *testing.T) {
		if _, err := Verify(b.Zip, []ed25519.PublicKey{otherPub, pub}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("not a zip", func(t *testing.T) {
		if _, err := Verify([]byte("PK nope"), keys); err == nil {
			t.Fatal("garbage accepted")
		}
	})
	t.Run("too many entries", func(t *testing.T) {
		data := rebuild(t, b.Zip, func(e map[string][]byte) []string {
			names := entryNames(e)
			for i := 0; i < MaxEntries+3; i++ {
				n := "app/pad" + strings.Repeat("a", i%50) + string(rune('A'+i%26)) + strings.Repeat("b", i/50)
				e[n] = nil
				names = append(names, n)
			}
			return names
		})
		if _, err := Verify(data, keys); err == nil {
			t.Fatal("expected the entry limit to apply")
		}
	})
}

func entryNames(e map[string][]byte) []string {
	names := make([]string, 0, len(e))
	for n := range e {
		names = append(names, n)
	}
	return names
}

func TestVerifyBoundsDecompression(t *testing.T) {
	pub, priv := testKey(t)
	b, err := Build(okOptions(t, priv))
	if err != nil {
		t.Fatal(err)
	}
	// A file whose declared size is small but whose stream is larger: the
	// reader must stop at the limit instead of trusting the header.
	data := rebuild(t, b.Zip, func(e map[string][]byte) []string {
		e["app/index.html"] = make([]byte, MaxFileSize+10)
		return nil
	})
	if _, err := Verify(data, []ed25519.PublicKey{pub}); err == nil {
		t.Fatal("oversized entry accepted")
	}
}

func TestVerifyReportsDamagedEntries(t *testing.T) {
	pub, priv := testKey(t)
	b, err := Build(okOptions(t, priv))
	if err != nil {
		t.Fatal(err)
	}
	keys := []ed25519.PublicKey{pub}
	copyWith := func(damage func(zw *zip.Writer, f *zip.File, body []byte)) []byte {
		zr, _ := zip.NewReader(bytes.NewReader(b.Zip), int64(len(b.Zip)))
		var out bytes.Buffer
		zw := zip.NewWriter(&out)
		for _, f := range zr.File {
			rc, _ := f.Open()
			var body bytes.Buffer
			_, _ = body.ReadFrom(rc)
			_ = rc.Close()
			if f.Name != "app/index.html" {
				w, _ := zw.Create(f.Name)
				_, _ = w.Write(body.Bytes())
				continue
			}
			damage(zw, f, body.Bytes())
		}
		_ = zw.Close()
		return out.Bytes()
	}
	t.Run("corrupt stream", func(t *testing.T) {
		data := copyWith(func(zw *zip.Writer, f *zip.File, body []byte) {
			garbage := bytes.Repeat([]byte{0xff}, 16)
			h := &zip.FileHeader{Name: f.Name, Method: zip.Deflate, CRC32: f.CRC32, CompressedSize64: uint64(len(garbage)), UncompressedSize64: uint64(len(body))}
			w, _ := zw.CreateRaw(h)
			_, _ = w.Write(garbage)
		})
		if _, err := Verify(data, keys); err == nil {
			t.Fatal("damaged entry accepted")
		}
	})
	t.Run("unsupported method", func(t *testing.T) {
		data := copyWith(func(zw *zip.Writer, f *zip.File, body []byte) {
			h := &zip.FileHeader{Name: f.Name, Method: 99, CRC32: f.CRC32, CompressedSize64: uint64(len(body)), UncompressedSize64: uint64(len(body))}
			w, _ := zw.CreateRaw(h)
			_, _ = w.Write(body)
		})
		if _, err := Verify(data, keys); err == nil {
			t.Fatal("unreadable entry accepted")
		}
	})
}

func TestValidPath(t *testing.T) {
	for _, p := range []string{"index.html", "assets/a-b.js", "\u00fcnï/x.js"} {
		if !ValidPath(p) {
			t.Errorf("%q should be valid", p)
		}
	}
	for _, p := range []string{"", "/abs", "a//b", "a/./b", "a/../b", "..", ".", "a/", `a\b`, "a\x00b", "a\x7f", "a\nb", "\xff", strings.Repeat("a", maxPathLen+1)} {
		if ValidPath(p) {
			t.Errorf("%q should be invalid", p)
		}
	}
}

func TestVersions(t *testing.T) {
	for _, s := range []string{"0.0.0", "1.2.3", "10.20.30"} {
		if _, err := ParseVersion(s); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	for _, s := range []string{"", "1", "1.2", "1.2.3.4", "v1.2.3", "1.2.x", "01.2.3", "1..3", "1.2.3-rc1", "99999999999.0.0", "-1.0.0"} {
		if _, err := ParseVersion(s); err == nil {
			t.Errorf("%q should be rejected", s)
		}
	}
	order := [][2]string{{"0.7.16", "0.7.17"}, {"0.7.17", "0.8.0"}, {"0.9.0", "1.0.0"}, {"0.7.9", "0.7.10"}}
	for _, pair := range order {
		if n, _ := CompareVersions(pair[0], pair[1]); n != -1 {
			t.Errorf("%s < %s expected", pair[0], pair[1])
		}
		if n, _ := CompareVersions(pair[1], pair[0]); n != 1 {
			t.Errorf("%s > %s expected", pair[1], pair[0])
		}
	}
	if n, _ := CompareVersions("1.2.3", "1.2.3"); n != 0 {
		t.Error("equal versions")
	}
	if _, err := CompareVersions("x", "1.2.3"); err == nil {
		t.Error("left side error")
	}
	if _, err := CompareVersions("1.2.3", "x"); err == nil {
		t.Error("right side error")
	}
}

func TestCompat(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "c.json")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if c, err := LoadCompat(write(`{"web_client_api":2,"min_server_version":"0.7.16","security_floor":"0.7.1"}`)); err != nil || c.WebClientAPI != 2 {
		t.Fatalf("%+v %v", c, err)
	}
	for name, body := range map[string]string{
		"unknown":   `{"web_client_api":1,"min_server_version":"0.7.16","security_floor":"0.0.0","x":1}`,
		"api":       `{"web_client_api":0,"min_server_version":"0.7.16","security_floor":"0.0.0"}`,
		"min":       `{"web_client_api":1,"min_server_version":"x","security_floor":"0.0.0"}`,
		"floor":     `{"web_client_api":1,"min_server_version":"0.7.16","security_floor":"x"}`,
		"not json":  `nope`,
		"trailing?": ``,
	} {
		if _, err := LoadCompat(write(body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if _, err := LoadCompat(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("missing file")
	}
}

func TestKeyParsing(t *testing.T) {
	pub, priv := testKey(t)
	privDER, _ := x509.MarshalPKCS8PrivateKey(priv)
	pubDER, _ := x509.MarshalPKIXPublicKey(pub)
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	if k, err := ParsePrivateKeyPEM(privPEM); err != nil || !k.Equal(priv) {
		t.Fatal("private key round trip", err)
	}
	if k, err := ParsePublicKeyPEM(pubPEM); err != nil || !k.Equal(pub) {
		t.Fatal("public key round trip", err)
	}
	if _, err := ParsePrivateKeyPEM([]byte("x")); err == nil {
		t.Error("no PEM")
	}
	if _, err := ParsePublicKeyPEM([]byte("x")); err == nil {
		t.Error("no PEM")
	}
	junk := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk")})
	if _, err := ParsePrivateKeyPEM(junk); err == nil {
		t.Error("junk private key")
	}
	if _, err := ParsePublicKeyPEM(junk); err == nil {
		t.Error("junk public key")
	}
	// An RSA-shaped key of the right container but the wrong algorithm.
	ecPriv, ecPub := otherAlgorithmKeys(t)
	if _, err := ParsePrivateKeyPEM(ecPriv); err == nil {
		t.Error("non-Ed25519 private key")
	}
	if _, err := ParsePublicKeyPEM(ecPub); err == nil {
		t.Error("non-Ed25519 public key")
	}
}
