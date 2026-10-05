//go:build integration

package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
)

type searchResult struct {
	Messages []struct {
		ID        uuid.UUID  `json:"id"`
		ChannelID uuid.UUID  `json:"channel_id"`
		Content   string     `json:"content"`
		ParentID  *uuid.UUID `json:"parent_id"`
	} `json:"messages"`
	HasMore bool `json:"has_more"`
}

func (c *client) search(t *testing.T, params map[string]string) (searchResult, int) {
	t.Helper()
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	res := c.get("/api/search?" + q.Encode())
	var out searchResult
	if res.status == http.StatusOK {
		res.decode(t, &out)
	}
	return out, res.status
}

func TestSearchFindsMessagesWithFilters(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	general := a.createChannel(admin, "allgemein", "text")
	music := a.createChannel(admin, "musik", "text")

	postMessage(t, admin, general, map[string]any{"content": "Treffen um 20 Uhr im Park"})
	postMessage(t, max, general, map[string]any{"content": "park ist gut, 100% dabei"})
	postMessage(t, max, music, map[string]any{"content": "Neues Album vom Park-Quartett"})
	postMessage(t, admin, general, map[string]any{"content": "ganz anderes Thema"})
	img := admin.upload("/api/channels/"+general.String()+"/upload", "file", "park.png", "image/png", pngBytes, map[string]string{"content": "Foto vom Park"})
	if img.status != http.StatusCreated {
		t.Fatalf("upload: %d", img.status)
	}

	got, _ := max.search(t, map[string]string{"q": "PARK"})
	if len(got.Messages) != 4 {
		t.Fatalf("q=PARK: %d results, want 4: %+v", len(got.Messages), got.Messages)
	}
	if got.Messages[0].Content != "Foto vom Park" {
		t.Fatalf("newest first expected, got %q", got.Messages[0].Content)
	}

	got, _ = max.search(t, map[string]string{"q": "park", "channel_id": music.String()})
	if len(got.Messages) != 1 || got.Messages[0].ChannelID != music {
		t.Fatalf("channel filter: %+v", got.Messages)
	}
	got, _ = max.search(t, map[string]string{"q": "park", "author_id": max.user.ID.String()})
	if len(got.Messages) != 2 {
		t.Fatalf("author filter: %+v", got.Messages)
	}
	got, _ = max.search(t, map[string]string{"has": "image"})
	if len(got.Messages) != 1 || got.Messages[0].Content != "Foto vom Park" {
		t.Fatalf("has=image: %+v", got.Messages)
	}
	got, _ = max.search(t, map[string]string{"q": "uhr park"})
	if len(got.Messages) != 1 {
		t.Fatalf("all terms must match: %+v", got.Messages)
	}
	got, _ = max.search(t, map[string]string{"q": "100%"})
	if len(got.Messages) != 1 {
		t.Fatalf("%% must be literal: %+v", got.Messages)
	}

	page, _ := max.search(t, map[string]string{"q": "park", "limit": "2"})
	if len(page.Messages) != 2 || !page.HasMore {
		t.Fatalf("first page: %+v", page)
	}
	rest, _ := max.search(t, map[string]string{"q": "park", "limit": "2", "before": page.Messages[1].ID.String()})
	if len(rest.Messages) != 2 || rest.HasMore || rest.Messages[0].ID == page.Messages[1].ID {
		t.Fatalf("second page: %+v", rest)
	}

	if _, status := max.search(t, map[string]string{}); status != http.StatusBadRequest {
		t.Fatalf("empty search: %d", status)
	}
	if res := a.anon().get("/api/search?q=park"); res.status != http.StatusUnauthorized {
		t.Fatalf("anonymous search: %d", res.status)
	}
}

// A thread reply is found by search and carries its root's id, so the client
// can jump to the root (the only valid ?around= anchor) and open the thread.
func TestSearchThreadReplyCarriesParentForJump(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	ch := a.createChannel(admin, "allgemein", "text")

	root := postMessage(t, admin, ch, map[string]any{"content": "Wurzel der Diskussion"})
	reply := postMessage(t, max, ch, map[string]any{"content": "Zebrastreifen in der Antwort", "parent_id": root})

	got, _ := max.search(t, map[string]string{"q": "zebrastreifen"})
	if len(got.Messages) != 1 || got.Messages[0].ID != reply {
		t.Fatalf("reply not found: %+v", got.Messages)
	}
	if got.Messages[0].ParentID == nil || *got.Messages[0].ParentID != root {
		t.Fatalf("parent_id missing: %+v", got.Messages[0])
	}

	if res := max.get("/api/channels/" + ch.String() + "/messages?around=" + reply.String()); res.status != http.StatusNotFound {
		t.Fatalf("around a reply must be rejected, got %d", res.status)
	}
	if res := max.get("/api/channels/" + ch.String() + "/messages?around=" + root.String()); res.status != http.StatusOK {
		t.Fatalf("around the root: %d", res.status)
	}
}
