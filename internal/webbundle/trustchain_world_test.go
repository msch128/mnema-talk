package webbundle

// The world the trust chain sequences run in: an offline root key, CI signing
// keys that rotate and leak, releases whose manifests carry the floors the
// release workflow gives them, and the statements the root key signs. Every
// artifact is built and signed by this package's own Build and SignTrust.

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func decodeHexKey(h string) (ed25519.PublicKey, error) {
	raw, err := hex.DecodeString(h)
	return ed25519.PublicKey(raw), err
}

func atoiAPI(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func itoaAPI(n int) string { return strconv.Itoa(n) }

// baselineFloor is web/desktop-compat.json's floor: what a release without a
// Security-Floor trailer declares, however high an earlier release raised it.
const baselineFloor = "0.7.23"

type release struct {
	raw     []byte
	version Version
	api     int
	floor   Version
	key     int
}

type statement struct {
	doc, sig []byte
	t        Trust
}

type world struct {
	t        *testing.T
	rng      *rand.Rand
	dir      string
	root     ed25519.PrivateKey
	keys     []ed25519.PrivateKey
	ci       int          // index of the key the release workflow signs with
	allowed  map[int]bool // keys the newest statement allows
	revoked  map[int]bool
	leaked   map[int]bool
	serial   uint64
	stmts    []statement
	releases []release
	next     [3]uint32 // next release version
	nextAPI  int
	minSrv   string    // min_server_version the next release declares
	now      time.Time // true time
	skew     time.Duration
	cache    map[string][]byte
}

func newWorld(t *testing.T, seed int64) *world {
	w := &world{
		t: t, rng: rand.New(rand.NewSource(seed)), dir: t.TempDir(),
		allowed: map[int]bool{0: true}, revoked: map[int]bool{}, leaked: map[int]bool{},
		next: [3]uint32{3, 0, 7}, nextAPI: 1, minSrv: "0.7.16", cache: map[string][]byte{},
		now: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC),
	}
	w.root = ed25519.NewKeyFromSeed(seedBytes(200))
	for i := 0; i < 4; i++ {
		w.keys = append(w.keys, ed25519.NewKeyFromSeed(seedBytes(byte(i+1))))
	}
	if err := os.WriteFile(filepath.Join(w.dir, "index.html"), []byte("<!doctype html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.sign(nil)
	return w
}

func seedBytes(b byte) []byte {
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = b
	}
	return s
}

func (w *world) pub(i int) ed25519.PublicKey { return w.keys[i].Public().(ed25519.PublicKey) }

func (w *world) clientNow() time.Time { return w.now.Add(w.skew) }

// bundle builds (once) a signed bundle.
func (w *world) bundle(v Version, api int, floor Version, key int) []byte {
	vs, fs := fmtVersion(v), fmtVersion(floor)
	id := fmt.Sprintf("%s/%d/%s/%d/%s", vs, api, fs, key, w.minSrv)
	if raw, ok := w.cache[id]; ok {
		return raw
	}
	b, err := Build(BuildOptions{
		Dir: w.dir, Version: vs, Commit: "test", Key: w.keys[key],
		Compat: Compat{WebClientAPI: api, MinServerVersion: w.minSrv, SecurityFloor: fs},
	})
	if err != nil {
		w.t.Fatalf("build %s: %v", id, err)
	}
	w.cache[id] = b.Zip
	return b.Zip
}

func fmtVersion(v Version) string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

// release cuts the next release with the CI key. A release with a security
// fix raises its own floor; every other one declares the baseline floor.
func (w *world) release(raise bool) release {
	if n := len(w.releases); n > 0 && w.rng.Intn(4) == 0 {
		// The web app starts to need something the previous server lacked.
		w.minSrv = fmtVersion(w.releases[n-1].version)
	}
	v := Version(w.next)
	w.next[2]++
	floor := mustVersion(baselineFloor)
	if raise {
		floor = v
	}
	r := release{raw: w.bundle(v, w.nextAPI, floor, w.ci), version: v, api: w.nextAPI, floor: floor, key: w.ci}
	w.releases = append(w.releases, r)
	return r
}

// sign issues the next statement from the root key's view of the keys.
func (w *world) sign(caps map[string]string) statement {
	w.serial++
	t := Trust{
		Schema: TrustSchema, Serial: w.serial,
		IssuedAt:   w.now.Format(time.RFC3339),
		ExpiresAt:  w.now.Add(DefaultTrustValidity).Format(time.RFC3339),
		BundleKeys: []string{}, RevokedBundleKeys: []string{},
		UpdaterKeys: []string{}, RevokedUpdaterKeys: []string{}, MaxVersion: map[string]string{},
	}
	for i := range w.keys {
		if w.allowed[i] {
			t.BundleKeys = append(t.BundleKeys, hex.EncodeToString(w.pub(i)))
		}
		if w.revoked[i] {
			t.RevokedBundleKeys = append(t.RevokedBundleKeys, KeyID(w.pub(i)))
		}
	}
	for k, v := range caps {
		t.MaxVersion[k] = v
	}
	doc, sig, err := SignTrust(t, w.root)
	if err != nil {
		w.t.Fatalf("sign trust: %v", err)
	}
	s := statement{doc: doc, sig: sig, t: t}
	w.stmts = append(w.stmts, s)
	return s
}

// unusedKey returns a key that was never allowed or revoked, or -1.
func (w *world) unusedKey() int {
	for i := range w.keys {
		if !w.allowed[i] && !w.revoked[i] {
			return i
		}
	}
	return -1
}
