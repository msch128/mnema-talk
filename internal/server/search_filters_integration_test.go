//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
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

func TestSearchFiltersV3DuringAndDates(t *testing.T) {
	f := newAccessFixture(t, false)
	open := f.a.createChannel(f.owner, "open", "text")
	postMessage(t, f.owner, open, map[string]any{"content": "message posted today"})

	// "Today" is the day in the zone the search names, not in UTC: between
	// 22:00 and midnight UTC it is already tomorrow at +02:00.
	today := time.Now().In(time.FixedZone("", 2*60*60)).Format("2006-01-02")
	got, status := f.owner.search(t, map[string]string{"during": today, "tz": "+02:00"})
	if status != http.StatusOK || len(got.Messages) != 1 {
		t.Fatalf("during today: status=%d, count=%d", status, len(got.Messages))
	}

	got, status = f.owner.search(t, map[string]string{"during": "2020-01-01"})
	if status != http.StatusOK || len(got.Messages) != 0 {
		t.Fatalf("during 2020: status=%d, count=%d", status, len(got.Messages))
	}

	for name, params := range map[string]map[string]string{
		"bad during date":  {"during": "invalid-date"},
		"bad during month": {"during": "2026-13-01"},
		"bad tz":           {"during": today, "tz": "invalid-tz"},
	} {
		if _, status := f.owner.search(t, params); status != http.StatusBadRequest {
			t.Errorf("%s: status=%d, want 400", name, status)
		}
	}
}

func TestSearchFiltersV3FilenameAndPermissions(t *testing.T) {
	f := newAccessFixture(t, false)
	secret := f.privateChannel(t, "staff-only", "text")
	open := f.a.createChannel(f.owner, "open", "text")

	u1 := f.owner.upload("/api/channels/"+open.String()+"/upload", "file", "public-budget.png", "image/png", pngBytes, map[string]string{"content": "public budget report"})
	if u1.status != http.StatusCreated {
		t.Fatalf("upload open: %d", u1.status)
	}
	u2 := f.owner.upload("/api/channels/"+secret.String()+"/upload", "file", "secret-plans.png", "image/png", pngBytes, map[string]string{"content": "confidential roadmap"})
	if u2.status != http.StatusCreated {
		t.Fatalf("upload secret: %d", u2.status)
	}

	// Substring and case-insensitive filename search
	got, status := f.owner.search(t, map[string]string{"filename": "BUDGET"})
	if status != http.StatusOK || len(got.Messages) != 1 {
		t.Fatalf("filename BUDGET: status=%d, count=%d", status, len(got.Messages))
	}

	// has=file returns both for owner
	got, status = f.owner.search(t, map[string]string{"has": "file"})
	if status != http.StatusOK || len(got.Messages) != 2 {
		t.Fatalf("has=file: status=%d, count=%d", status, len(got.Messages))
	}

	// Permission enforcement: insider can see secret file; outsider NEVER sees it
	got, status = f.insider.search(t, map[string]string{"filename": "secret"})
	if status != http.StatusOK || len(got.Messages) != 1 {
		t.Fatalf("insider filename secret: status=%d, count=%d", status, len(got.Messages))
	}
	got, status = f.outsider.search(t, map[string]string{"filename": "secret"})
	if status != http.StatusOK || len(got.Messages) != 0 {
		t.Fatalf("outsider must never see attachment from secret channel: status=%d, got=%+v", status, got.Messages)
	}
}

func TestSearchFiltersV3AuthorType(t *testing.T) {
	f := newAccessFixture(t, false)
	open := f.a.createChannel(f.owner, "open", "text")

	postMessage(t, f.owner, open, map[string]any{"content": "author member post"})

	var botUser, whUser uuid.UUID
	ctx := t.Context()
	err := f.a.db.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role, is_bot) VALUES ('helper-bot', 'Helper', 'x', 'user', TRUE) RETURNING id`).Scan(&botUser)
	if err != nil {
		t.Fatalf("insert bot: %v", err)
	}
	err = f.a.db.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role, is_bot) VALUES ('wh-hook', 'Webhook', 'x', 'user', TRUE) RETURNING id`).Scan(&whUser)
	if err != nil {
		t.Fatalf("insert webhook: %v", err)
	}

	if _, err := f.a.db.Exec(ctx, `INSERT INTO messages (channel_id, user_id, content) VALUES ($1, $2, $3)`, open, botUser, "author bot post"); err != nil {
		t.Fatalf("insert bot msg: %v", err)
	}
	if _, err := f.a.db.Exec(ctx, `INSERT INTO messages (channel_id, user_id, content) VALUES ($1, $2, $3)`, open, whUser, "author webhook post"); err != nil {
		t.Fatalf("insert webhook msg: %v", err)
	}

	for typ, wantContent := range map[string]string{
		"member":  "author member post",
		"bot":     "author bot post",
		"webhook": "author webhook post",
	} {
		got, status := f.owner.search(t, map[string]string{"author": typ})
		if status != http.StatusOK || len(got.Messages) != 1 || got.Messages[0].Content != wantContent {
			t.Fatalf("author=%s: status=%d, got=%+v", typ, status, got.Messages)
		}
	}

	if _, status := f.owner.search(t, map[string]string{"author": "invalid"}); status != http.StatusBadRequest {
		t.Fatalf("invalid author status: %d", status)
	}
}

func TestSearchFiltersV3Sort(t *testing.T) {
	f := newAccessFixture(t, false)
	open := f.a.createChannel(f.owner, "open", "text")

	m1 := postMessage(t, f.owner, open, map[string]any{"content": "alpha sort test"})
	time.Sleep(10 * time.Millisecond)
	m2 := postMessage(t, f.owner, open, map[string]any{"content": "beta sort test"})
	time.Sleep(10 * time.Millisecond)
	m3 := postMessage(t, f.owner, open, map[string]any{"content": "gamma sort test"})

	// Newest first (default and sort=newest and sort=relevance)
	got, status := f.owner.search(t, map[string]string{"q": "sort test", "sort": "newest"})
	if status != http.StatusOK || len(got.Messages) != 3 || got.Messages[0].ID != m3 || got.Messages[2].ID != m1 {
		t.Fatalf("sort newest: %+v", got.Messages)
	}
	got, status = f.owner.search(t, map[string]string{"q": "sort test", "sort": "relevance"})
	if status != http.StatusOK || len(got.Messages) != 3 || got.Messages[0].ID != m3 {
		t.Fatalf("sort relevance: %+v", got.Messages)
	}

	// Oldest first
	got, status = f.owner.search(t, map[string]string{"q": "sort test", "sort": "oldest"})
	if status != http.StatusOK || len(got.Messages) != 3 || got.Messages[0].ID != m1 || got.Messages[2].ID != m3 {
		t.Fatalf("sort oldest: %+v", got.Messages)
	}

	// Oldest pagination with before
	page, status := f.owner.search(t, map[string]string{"q": "sort test", "sort": "oldest", "limit": "2"})
	if status != http.StatusOK || len(page.Messages) != 2 || !page.HasMore || page.Messages[0].ID != m1 || page.Messages[1].ID != m2 {
		t.Fatalf("oldest page 1: %+v", page)
	}
	rest, status := f.owner.search(t, map[string]string{"q": "sort test", "sort": "oldest", "limit": "2", "before": m2.String()})
	if status != http.StatusOK || len(rest.Messages) != 1 || rest.HasMore || rest.Messages[0].ID != m3 {
		t.Fatalf("oldest page 2: %+v", rest)
	}

	if _, status := f.owner.search(t, map[string]string{"q": "sort test", "sort": "bad"}); status != http.StatusBadRequest {
		t.Fatalf("bad sort status: %d", status)
	}
}
