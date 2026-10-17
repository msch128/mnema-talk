package perm

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// The web app mirrors the bits in web/src/lib/permissions.ts so it can decide
// what to show without a round trip. The two tables must not drift.
func TestWebMirrorsTheCatalog(t *testing.T) {
	src, err := os.ReadFile("../../web/src/lib/permissions.ts")
	if err != nil {
		t.Fatal(err)
	}
	table := regexp.MustCompile(`(?s)PERMISSION_BITS = \{(.*?)\} as const`).FindSubmatch(src)
	if table == nil {
		t.Fatal("PERMISSION_BITS not found")
	}
	got := map[string]Set{}
	for _, m := range regexp.MustCompile(`(\w+):\s*(\d+)`).FindAllSubmatch(table[1], -1) {
		n, _ := strconv.ParseUint(string(m[2]), 10, 64)
		got[string(m[1])] = Set(n)
	}
	if len(got) != len(Catalog) {
		t.Fatalf("web has %d permissions, the server %d", len(got), len(Catalog))
	}
	for _, i := range Catalog {
		if got[i.Key] != i.Bit {
			t.Errorf("%s: web %d, server %d", i.Key, got[i.Key], i.Bit)
		}
	}
	if m := regexp.MustCompile(`DEFAULT_EVERYONE = (\d+)`).FindSubmatch(src); m == nil || string(m[1]) != strconv.Itoa(int(DefaultEveryone)) {
		t.Errorf("DEFAULT_EVERYONE differs from %d", DefaultEveryone)
	}
}
