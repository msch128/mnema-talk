//go:build integration

package wiki

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/userdata"
)

// exportedRevisions builds wiki_revisions.json for alice under the given limits.
func exportedRevisions(t *testing.T, w *world, limits userdata.Limits) (list []map[string]any, truncated bool) {
	t.Helper()
	files, err := accountData{}.Export(context.Background(), w.p, w.alice, userdata.Channels{}, limits)
	if err != nil || len(files) != 1 || files[0].Name != "wiki_revisions.json" {
		t.Fatalf("files = %+v, %v", files, err)
	}
	value, cut, err := files[0].Build()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatal(err)
	}
	return list, cut
}

func TestTheExportListsEveryRevisionTheMemberSavedOldestFirst(t *testing.T) {
	w := newWorld(t)
	p := w.page(t, "Lore", nil)
	for i, text := range []string{"second", "third", "fourth"} {
		if _, err := w.s.Change(context.Background(), w.alice, p.Slug, PageChange{BaseRev: i + 1, Content: &text}, false); err != nil {
			t.Fatal(err)
		}
	}
	list, cut := exportedRevisions(t, w, userdata.Limits{Page: 2, MaxRows: 100, MessageBytes: 1 << 20})
	if cut || len(list) != 4 {
		t.Fatalf("cut %v, %d revisions: %+v", cut, len(list), list)
	}
	for i, row := range list {
		if row["page"] != "lore" || int(row["rev"].(float64)) != i+1 {
			t.Fatalf("row %d = %+v", i, row)
		}
	}
}

// A page holds up to 50 KB and a member can have saved thousands of revisions:
// the export keeps to the same byte bound as the member's messages and says it
// was cut, instead of holding every text in memory at once.
func TestTheExportStopsAtItsByteBoundAndSaysSo(t *testing.T) {
	w := newWorld(t)
	p := w.page(t, "Lore", nil)
	big := strings.Repeat("x", 20*1024)
	for i := 0; i < 5; i++ {
		text := big + string(rune('a'+i))
		if _, err := w.s.Change(context.Background(), w.alice, p.Slug, PageChange{BaseRev: i + 1, Content: &text}, false); err != nil {
			t.Fatal(err)
		}
	}
	list, cut := exportedRevisions(t, w, userdata.Limits{Page: 2, MaxRows: 100, MessageBytes: 50 * 1024})
	if !cut || len(list) == 0 || len(list) >= 5 {
		t.Fatalf("cut %v with %d of 6 revisions", cut, len(list))
	}
	if first := int(list[0]["rev"].(float64)); first != 1 {
		t.Fatalf("the oldest revision comes first, got %d", first)
	}
}
