package onboarding

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestCleanVanitySlugMatrix(t *testing.T) {
	ok := map[string]string{
		"meinclan": "meinclan", "MeinClan": "meinclan", "  owls  ": "owls", "a1b": "a1b",
		"my-clan-2": "my-clan-2", strings.Repeat("a", 32): strings.Repeat("a", 32), "": "",
	}
	for in, want := range ok {
		got, err := CleanVanitySlug(in)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v, want %q", in, got, err, want)
		}
	}
	bad := []string{
		"ab", strings.Repeat("a", 33), "-owls", "owls-", "my--clan", "my_clan", "my clan", "own.s", "ünï", "a/b", "../x",
		"admin", "API", "Join", "login", "wiki", "events", "assets", "health", "static", "settings", "invite",
	}
	for _, in := range bad {
		_, err := CleanVanitySlug(in)
		if ae, ok := httpx.AsAPIError(err); !ok || ae.Status != 400 {
			t.Errorf("%q: %v, want a 400", in, err)
		}
	}
}

// A new top-level route of the web app, a new file at the root of the web
// build or a new API prefix must be a reserved word, so no vanity link ever
// reads like one of them.
func TestReservedSlugsCoverTheRoutesOfTheApp(t *testing.T) {
	root := filepath.Join("..", "..", "web")
	router, err := os.ReadFile(filepath.Join(root, "src", "lib", "router.ts"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range regexp.MustCompile(`parts\[0\] === '([a-z0-9-]+)'`).FindAllStringSubmatch(string(router), -1) {
		names = append(names, m[1])
	}
	if len(names) < 5 {
		t.Fatalf("found only %v in router.ts: has the route table moved?", names)
	}
	files, err := os.ReadDir(filepath.Join(root, "public"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".") {
			continue
		}
		base := strings.TrimSuffix(f.Name(), filepath.Ext(f.Name()))
		// pwa-icon-512 and the like: the prefix before a size or variant.
		base = regexp.MustCompile(`-(\d+|maskable.*|dark|light|mono)$`).ReplaceAllString(base, "")
		names = append(names, base)
	}
	names = append(names, "api", "invite", "join", "login", "assets", "sw", "ws")
	for _, name := range names {
		if !reservedSlugs[name] && !reservedSlugs[strings.SplitN(name, "-", 2)[0]] {
			t.Errorf("%q is a route or file of the app but not a reserved vanity slug", name)
		}
	}
}
