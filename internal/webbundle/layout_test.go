package webbundle

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// The desktop clients (Tauri host web_bundle.rs / web_json.rs and the Linux
// client zip.ts) refuse a bundle whose archive layout or manifest JSON is not
// canonical. These tests pin the Go builder to exactly those rules, so a change
// to the builder cannot silently make every desktop client refuse a release.

const (
	sigLocal   = 0x04034b50
	sigCentral = 0x02014b50
	sigEnd     = 0x06054b50
	sigZip64   = 0x07064b50

	flagEncrypted = 1 | 0x40 | 0x2000 // traditional, strong, central directory encryption
	maxJSONDepth  = 126
)

type zipEntry struct {
	name   string
	flags  uint16
	made   uint16
	attrs  uint32
	offset uint32
}

func le16(b []byte, at int) int { return int(binary.LittleEndian.Uint16(b[at:])) }
func le32(b []byte, at int) int { return int(binary.LittleEndian.Uint32(b[at:])) }

// checkEnd validates the end record and returns the central directory range.
func checkEnd(data []byte) (start, end int, count int, err error) {
	if len(data) < 22 {
		return 0, 0, 0, errors.New("shorter than an end record")
	}
	eocd := len(data) - 22
	switch {
	case le32(data, eocd) != sigEnd:
		return 0, 0, 0, errors.New("end record is not the last 22 bytes (comment or trailing bytes)")
	case le16(data, eocd+20) != 0:
		return 0, 0, 0, errors.New("zip comment present")
	case len(data) >= 42 && le32(data, eocd-20) == sigZip64:
		return 0, 0, 0, errors.New("ZIP64 locator present")
	case le32(data, 0) != sigLocal:
		return 0, 0, 0, errors.New("bytes before the first entry")
	}
	count = le16(data, eocd+10)
	size, off := le32(data, eocd+12), le32(data, eocd+16)
	if le16(data, eocd+4) != 0 || le16(data, eocd+6) != 0 || le16(data, eocd+8) != count ||
		count == 0xffff || size == 0xffffffff || off == 0xffffffff || off+size != eocd {
		return 0, 0, 0, errors.New("multi-disk, ZIP64 marker or central directory not ending at the end record")
	}
	return off, eocd, count, nil
}

func readCentral(data []byte, start, end, count int) ([]zipEntry, error) {
	entries := make([]zipEntry, 0, count)
	at := start
	for range count {
		if at+46 > end || le32(data, at) != sigCentral {
			return nil, errors.New("malformed central directory")
		}
		nameLen := le16(data, at+28)
		next := at + 46 + nameLen + le16(data, at+30) + le16(data, at+32)
		if next > end {
			return nil, errors.New("central entry overruns the directory")
		}
		for _, v := range []int{le32(data, at+20), le32(data, at+24), le32(data, at+42)} {
			if v == 0xffffffff {
				return nil, errors.New("ZIP64 marker in an entry")
			}
		}
		entries = append(entries, zipEntry{
			name:   string(data[at+46 : at+46+nameLen]),
			flags:  uint16(le16(data, at+8)),
			made:   uint16(le16(data, at+4)),
			attrs:  uint32(le32(data, at+38)),
			offset: uint32(le32(data, at+42)),
		})
		at = next
	}
	return entries, nil
}

func checkEntry(e zipEntry) error {
	if !utf8.ValidString(e.name) {
		return fmt.Errorf("%q: name is not UTF-8", e.name)
	}
	if e.flags&flagEncrypted != 0 {
		return fmt.Errorf("%s: encryption flag set", e.name)
	}
	if strings.HasSuffix(e.name, "/") {
		return fmt.Errorf("%s: directory entry", e.name)
	}
	if e.made>>8 == 3 { // written with Unix modes: only regular files
		if kind := (e.attrs >> 16) & 0o170000; kind != 0 && kind != 0o100000 {
			return fmt.Errorf("%s: link, directory or device (mode %o)", e.name, kind)
		}
	}
	return nil
}

// checkLayout applies the archive rules of the desktop clients to raw bytes.
func checkLayout(data []byte) error {
	start, end, count, err := checkEnd(data)
	if err != nil {
		return err
	}
	entries, err := readCentral(data, start, end, count)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	startsFile := false
	for _, e := range entries {
		if err := checkEntry(e); err != nil {
			return err
		}
		folded := strings.ToLower(e.name)
		if seen[folded] {
			return fmt.Errorf("%s: duplicate name (case-folded)", e.name)
		}
		seen[folded] = true
		startsFile = startsFile || e.offset == 0
	}
	if !startsFile {
		return errors.New("no entry starts the file")
	}
	return nil
}

// manifestFields mirrors the desktop Manifest and FileEntry structs
// (deny_unknown_fields), in the order the builder writes them.
var (
	manifestFields = []string{"schema", "name", "version", "web_client_api", "min_server_version", "security_floor", "commit", "key_id", "files"}
	fileFields     = []string{"path", "sha256", "size"}
)

// checkManifest applies the strict JSON rules of the desktop clients.
func checkManifest(raw []byte) error {
	if bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")) {
		return errors.New("manifest starts with a BOM")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := walkJSON(dec, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after the manifest")
	}
	return checkShape(raw)
}

// walkJSON reads one value, refusing repeated object keys and deep nesting.
func walkJSON(dec *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return errors.New("nesting too deep")
	}
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for dec.More() {
		if delim == '{' {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			if seen[key.(string)] {
				return fmt.Errorf("duplicate key %q", key)
			}
			seen[key.(string)] = true
		}
		if err := walkJSON(dec, depth+1); err != nil {
			return err
		}
	}
	_, err = dec.Token()
	return err
}

// checkShape requires objects (never arrays) with exactly the desktop fields
// and the right types.
func checkShape(raw []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return fmt.Errorf("manifest is not an object: %w", err)
	}
	if err := sameKeys(top, manifestFields); err != nil {
		return err
	}
	var files []json.RawMessage
	if err := json.Unmarshal(top["files"], &files); err != nil {
		return fmt.Errorf("files: %w", err)
	}
	for i, f := range files {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(f, &obj); err != nil {
			return fmt.Errorf("files[%d] is not an object: %w", i, err)
		}
		if err := sameKeys(obj, fileFields); err != nil {
			return fmt.Errorf("files[%d]: %w", i, err)
		}
	}
	var typed struct {
		Schema           uint32 `json:"schema"`
		Name             string `json:"name"`
		Version          string `json:"version"`
		WebClientAPI     uint32 `json:"web_client_api"`
		MinServerVersion string `json:"min_server_version"`
		SecurityFloor    string `json:"security_floor"`
		Commit           string `json:"commit"`
		KeyID            string `json:"key_id"`
		Files            []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
			Size   uint64 `json:"size"`
		} `json:"files"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(&typed)
}

func sameKeys(got map[string]json.RawMessage, want []string) error {
	for _, k := range want {
		if _, ok := got[k]; !ok {
			return fmt.Errorf("missing field %q", k)
		}
	}
	if len(got) != len(want) {
		return fmt.Errorf("unknown fields (have %d, desktop knows %d)", len(got), len(want))
	}
	return nil
}

func zipMember(t *testing.T, data []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := zr.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBuiltBundleSatisfiesDesktopLayout(t *testing.T) {
	_, priv := testKey(t)
	b, err := Build(okOptions(t, priv))
	if err != nil {
		t.Fatal(err)
	}
	if err := checkLayout(b.Zip); err != nil {
		t.Fatalf("layout: %v", err)
	}
	manifest := zipMember(t, b.Zip, manifestName)
	if !bytes.Equal(manifest, b.Manifest) {
		t.Fatal("manifest in the zip differs from the signed bytes")
	}
	if err := checkManifest(manifest); err != nil {
		t.Fatalf("manifest: %v", err)
	}
}

func TestLayoutCheckRefusesWhatDesktopRefuses(t *testing.T) {
	_, priv := testKey(t)
	b, err := Build(okOptions(t, priv))
	if err != nil {
		t.Fatal(err)
	}
	withComment := append(bytes.Clone(b.Zip), 'x')
	withPrefix := append([]byte("junk"), b.Zip...)
	dupName := buildZip(t, func(zw *zip.Writer) {
		for _, n := range []string{"a.txt", "A.txt"} {
			w, _ := zw.Create(n)
			_, _ = w.Write([]byte("x"))
		}
	})
	dir := buildZip(t, func(zw *zip.Writer) { _, _ = zw.Create("d/") })
	for name, data := range map[string][]byte{"trailing byte": withComment, "prefix": withPrefix, "case duplicate": dupName, "directory": dir} {
		if checkLayout(data) == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	for name, doc := range map[string]string{
		"bom":       "\xef\xbb\xbf{}",
		"duplicate": `{"schema":1,"schema":1}`,
		"unknown":   strings.Replace(string(b.Manifest), `"schema"`, `"extra": 1, "schema"`, 1),
		"trailing":  string(b.Manifest) + "{}",
		"array":     `[1]`,
	} {
		if checkManifest([]byte(doc)) == nil {
			t.Errorf("manifest %s was accepted", name)
		}
	}
}

func buildZip(t *testing.T, fill func(*zip.Writer)) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fill(zw)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestPublishedBundlesSatisfyDesktopLayout checks real release assets when
// MNEMA_BUNDLE_DIR points at a directory of mnema-web-*.zip files.
func TestPublishedBundlesSatisfyDesktopLayout(t *testing.T) {
	dir := os.Getenv("MNEMA_BUNDLE_DIR")
	if dir == "" {
		t.Skip("MNEMA_BUNDLE_DIR not set")
	}
	paths, err := filepath.Glob(filepath.Join(dir, "*", "mnema-web-*.zip"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no bundles under %s: %v", dir, err)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkLayout(data); err != nil {
			t.Errorf("%s: layout: %v", p, err)
		}
		if err := checkManifest(zipMember(t, data, manifestName)); err != nil {
			t.Errorf("%s: manifest: %v", p, err)
		}
		t.Logf("%s ok", p)
	}
}
