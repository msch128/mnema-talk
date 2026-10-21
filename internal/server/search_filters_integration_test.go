//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"
)

func TestSearchMentionsDatesAndVideoFilters(t *testing.T) {
	f := newAccessFixture(t, false)
	secret := f.privateChannel(t, "staff-only", "text")
	open := f.a.createChannel(f.owner, "open", "text")

	postMessage(t, f.owner, open, map[string]any{"content": "hello @insider from the open channel"})
	postMessage(t, f.owner, secret, map[string]any{"content": "psst @insider only staff sees this"})
	postMessage(t, f.owner, open, map[string]any{"content": "no mention here"})
	vid := f.owner.upload("/api/channels/"+open.String()+"/upload", "file", "clip.mp4", "video/mp4", pngBytes, map[string]string{"content": "a clip"})
	_ = vid // the upload sniffer may reject fake video bytes; has=video is then simply empty

	insider := f.insider.user.ID.String()
	got, status := f.insider.search(t, map[string]string{"mentions": insider})
	if status != http.StatusOK || len(got.Messages) != 2 {
		t.Fatalf("insider mentions: %d %+v", status, got.Messages)
	}
	got, _ = f.outsider.search(t, map[string]string{"mentions": insider})
	if len(got.Messages) != 1 || got.Messages[0].ChannelID != open {
		t.Fatalf("outsider must not see the hidden channel's mention: %+v", got.Messages)
	}
	if _, status = f.outsider.search(t, map[string]string{"mentions": "nope"}); status != http.StatusBadRequest {
		t.Fatalf("bad mentions id: %d", status)
	}

	now := time.Now().UTC()
	past := now.Add(-time.Hour).Format(time.RFC3339)
	future := now.Add(time.Hour).Format(time.RFC3339)
	if got, _ = f.owner.search(t, map[string]string{"after": past, "before_date": future}); len(got.Messages) < 3 {
		t.Fatalf("window around now: %+v", got.Messages)
	}
	if got, _ = f.owner.search(t, map[string]string{"after": future}); len(got.Messages) != 0 {
		t.Fatalf("after future: %+v", got.Messages)
	}
	if got, _ = f.owner.search(t, map[string]string{"before_date": past}); len(got.Messages) != 0 {
		t.Fatalf("before past: %+v", got.Messages)
	}
	for name, params := range map[string]map[string]string{
		"inverted window": {"after": future, "before_date": past},
		"equal window":    {"after": past, "before_date": past},
		"bad after":       {"after": "yesterday"},
		"bad before_date": {"before_date": "2026-01-01"},
		"bad has":         {"has": "audio"},
	} {
		if _, status := f.owner.search(t, params); status != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, status)
		}
	}
	if _, status := f.owner.search(t, map[string]string{"has": "video"}); status != http.StatusOK {
		t.Fatalf("has=video: %d", status)
	}
}

func TestSearchReplyHitsCarryRootSnippets(t *testing.T) {
	f := newAccessFixture(t, false)
	ch := f.a.createChannel(f.owner, "talk", "text")
	root := postMessage(t, f.owner, ch, map[string]any{"content": "root of the discussion"})
	postMessage(t, f.owner, ch, map[string]any{"content": "quokka reply", "parent_id": root})

	res := f.owner.get("/api/search?q=quokka")
	var out struct {
		Roots map[string]struct {
			Snippet string `json:"snippet"`
		} `json:"roots"`
	}
	res.decode(t, &out)
	if out.Roots[root.String()].Snippet != "root of the discussion" {
		t.Fatalf("roots: %+v", out.Roots)
	}
	res = f.owner.get("/api/search?q=discussion")
	out.Roots = nil
	res.decode(t, &out)
	if len(out.Roots) != 0 {
		t.Fatalf("a root hit needs no roots: %+v", out.Roots)
	}
}
