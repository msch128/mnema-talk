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
	// The permissions that open a part of the admin area decide which members see the admin console.
	area := regexp.MustCompile(`(?s)ADMIN_AREA_KEYS[^=]*= \[(.*?)\]`).FindSubmatch(src)
	if area == nil {
		t.Fatal("ADMIN_AREA_KEYS not found")
	}
	var webArea Set
	for _, m := range regexp.MustCompile(`'(\w+)'`).FindAllSubmatch(area[1], -1) {
		bit, ok := got[string(m[1])]
		if !ok {
			t.Fatalf("ADMIN_AREA_KEYS names an unknown permission %q", m[1])
		}
		webArea |= bit
	}
	if webArea != AdminArea {
		t.Errorf("ADMIN_AREA_KEYS is %d on the web and %d (AdminArea) on the server", webArea, AdminArea)
	}
	blocked := regexp.MustCompile(`(?s)SELF_ASSIGN_BLOCKED_KEYS[^=]*= \[(.*?)\]`).FindSubmatch(src)
	if blocked == nil {
		t.Fatal("SELF_ASSIGN_BLOCKED_KEYS not found")
	}
	var webBlocked Set
	for _, m := range regexp.MustCompile(`'(\w+)'`).FindAllSubmatch(blocked[1], -1) {
		webBlocked |= got[string(m[1])]
	}
	if webBlocked != SelfAssignBlocked {
		t.Errorf("SELF_ASSIGN_BLOCKED_KEYS is %d on the web and %d (SelfAssignBlocked) on the server", webBlocked, SelfAssignBlocked)
	}
	if m := regexp.MustCompile(`DEFAULT_EVERYONE = (\d+)`).FindSubmatch(src); m == nil || string(m[1]) != strconv.Itoa(int(DefaultEveryone)) {
		t.Errorf("DEFAULT_EVERYONE differs from %d", DefaultEveryone)
	}
}
