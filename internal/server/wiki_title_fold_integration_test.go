//go:build integration

package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/wiki"
)

// One rule, two implementations (2026-11 defect hunt, lane 3, pair 5): a wiki
// title is "unique among the pages, ignoring case, so [[Title]] names one page"
// (doc/wiki.md). Both sides compare one title key: the server's titleKey
// (internal/wiki/title_key.go) and the client's wikiTitleKey, which
// resolveWikiLink uses (web/src/wiki/lib/wikiTree.ts).
// testdata/wiki_title_pairs.json holds pairs of titles and whether the server
// refuses the second next to the first; web/src/wiki/lib/wikiTitleFold.test.ts
// checks that the client resolves the second to the first exactly then.
//
//	WIKI_PAIRS_UPDATE=1 go test -tags=integration -run TestWikiTitlePairsMatchTheServer ./internal/server
//
// rewrites the server verdicts.

type wikiTitlePair struct {
	A        string `json:"a"`
	B        string `json:"b"`
	Conflict bool   `json:"conflict"`
}

func TestWikiTitlePairsMatchTheServer(t *testing.T) {
	const file = "testdata/wiki_title_pairs.json"
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var pairs []wikiTitlePair
	if err := json.Unmarshal(raw, &pairs); err != nil {
		t.Fatal(err)
	}
	a := newApp(t, false)
	admin := a.seedAdmin()
	// The store behind POST /api/wiki/pages, without the route's 20 edits a minute.
	store := wiki.NewStore(a.db)
	ctx := context.Background()
	for i := range pairs {
		first, err := store.Create(ctx, admin.user.ID, wiki.PageInput{Title: pairs[i].A, Content: "x"})
		if err != nil {
			t.Fatalf("%q: %v", pairs[i].A, err)
		}
		second, err := store.Create(ctx, admin.user.ID, wiki.PageInput{Title: pairs[i].B, Content: "x"})
		var apiErr *httpx.APIError
		conflict := errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict
		if err != nil && !conflict {
			t.Fatalf("%q: %v", pairs[i].B, err)
		}
		for _, p := range []*wiki.Page{first, second} {
			if p != nil {
				if _, err := store.Delete(ctx, admin.user.ID, p.Slug); err != nil {
					t.Fatal(err)
				}
			}
		}
		if os.Getenv("WIKI_PAIRS_UPDATE") == "1" {
			pairs[i].Conflict = conflict
		} else if conflict != pairs[i].Conflict {
			t.Errorf("%q next to %q: server conflict %v, the file says %v", pairs[i].B, pairs[i].A, conflict, pairs[i].Conflict)
		}
	}
	if os.Getenv("WIKI_PAIRS_UPDATE") == "1" {
		out, _ := json.MarshalIndent(pairs, "", " ")
		if err := os.WriteFile(file, escapeNonASCII(append(out, '\n')), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// escapeNonASCII keeps the vector file readable: every character outside
// printable ASCII is written as a JSON escape.
func escapeNonASCII(raw []byte) []byte {
	var b strings.Builder
	for _, r := range string(raw) {
		switch {
		case r >= 0x20 && r < 0x7f || r == '\n':
			b.WriteRune(r)
		case r > 0xffff:
			r -= 0x10000
			fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return []byte(b.String())
}
