//go:build integration

package server

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// More writes in flight during the erasure (H-L10-05), beyond the ones the
// hunt tried: a poll vote, a reminder, a forum post, a wiki edit and a custom
// emoji (a stored file and a row the person created). Webhooks and bots are
// left out: only the Owner creates them, and the Owner account cannot be
// deleted.

// inFlightMore is what these sites build in their prep.
type inFlightMore struct {
	poll, option string
	forum        uuid.UUID
	page         string
	picture      []byte
}

// withRole gives alice a role with the permissions and signs her in again.
func (f *inFlightWorld) withRole(t *testing.T, p perm.Set) {
	t.Helper()
	assignRoles(t, f.admin, f.alice, createRole(t, f.admin, "in flight", p))
	f.alice = f.a.login("alice", memberPassword)
}

func moreInFlightSites() []inFlightSite {
	return []inFlightSite{
		{name: "poll vote", rows: `SELECT COUNT(*) FROM poll_votes WHERE user_id = $1`, prep: bobsPoll, write: func(f *inFlightWorld, _ string) response {
			return f.alice.put("/api/channels/"+f.ch.String()+"/messages/"+f.more.poll+"/poll/votes", map[string]any{"option_ids": []string{f.more.option}})
		}},
		{name: "reminder", rows: `SELECT COUNT(*) FROM message_reminders WHERE user_id = $1`, write: func(f *inFlightWorld, _ string) response {
			return f.alice.post("/api/me/reminders", map[string]any{"message_id": f.bobMsg, "remind_at": time.Now().Add(time.Hour)})
		}},
		{name: "forum post", prep: func(t *testing.T, f *inFlightWorld) {
			f.more.forum = f.a.createChannel(f.admin, "ideas", "forum")
		}, write: func(f *inFlightWorld, w string) response {
			return f.alice.post("/api/channels/"+f.more.forum.String()+"/posts", map[string]any{"title": w, "content": w})
		}},
		{name: "wiki edit", prep: adminsWikiPage, write: func(f *inFlightWorld, w string) response {
			return f.alice.patch("/api/wiki/pages/"+f.more.page, map[string]any{"base_rev": 1, "content": w})
		}},
		{name: "emoji", rows: `SELECT COUNT(*) FROM custom_emoji WHERE created_by = $1`, prep: func(t *testing.T, f *inFlightWorld) {
			f.withRole(t, perm.ManageEmoji)
			f.more.picture = squarePNG(t, 32)
		}, write: func(f *inFlightWorld, w string) response {
			return uploadEmoji(f.alice, w, w+".png", "image/png", f.more.picture)
		}},
	}
}

func bobsPoll(t *testing.T, f *inFlightWorld) {
	t.Helper()
	res := f.bob.post("/api/channels/"+f.ch.String()+"/polls", map[string]any{"question": "Pizza or pasta?", "options": []string{"Pizza", "Pasta"}})
	if res.status != http.StatusCreated {
		t.Fatalf("poll: %d %s", res.status, res.body)
	}
	var m struct {
		ID   string `json:"id"`
		Poll struct {
			Options []struct {
				ID string `json:"id"`
			} `json:"options"`
		} `json:"poll"`
	}
	res.decode(t, &m)
	f.more.poll, f.more.option = m.ID, m.Poll.Options[0].ID
}

func adminsWikiPage(t *testing.T, f *inFlightWorld) {
	t.Helper()
	f.withRole(t, perm.EditWiki)
	res := f.admin.post("/api/wiki/pages", map[string]any{"title": "Handbook", "content": "# Hello"})
	wantStatus(t, "page", res, http.StatusCreated)
	var p wikiPage
	res.decode(t, &p)
	f.more.page = p.Slug
}
