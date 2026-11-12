//go:build integration

package server

import (
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// TestRandomCommunitiesShowEachMemberWhatTheSnapshotAllows builds random
// communities through the admin API (@everyone's permissions, roles, members
// with roles, categories and channels with role, @everyone and member
// overwrites), keeps a mirror of what it set as a perm.State, and asks the
// real routes as every member: the channel list, the history of every
// channel, search for every channel's word, the mention inbox and
// GET /api/permissions. Each member must see a channel's word exactly when the
// snapshot built from the mirror grants View channels there, in both
// directions, and GET /api/permissions must equal the mirror's answer bit for
// bit. internal/perm's reference model checks the snapshot itself against the
// documented rules; this checks that the routes, the store and the loader say
// the same. Smoke: 3 communities; VIS_SWEEP=1: 400 (VIS_SEED shifts them).
func TestRandomCommunitiesShowEachMemberWhatTheSnapshotAllows(t *testing.T) {
	n, base := 3, uint64(100)
	if os.Getenv("VIS_SWEEP") == "1" {
		n = 400
	}
	if s, err := strconv.ParseUint(os.Getenv("VIS_SEED"), 10, 64); err == nil {
		base += s * 1000
	}
	checked, visible, flips := 0, 0, 0
	for i := range n {
		seed := base + uint64(i)
		t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
			w := newRandomWorld(t, seed)
			c, v := w.checkAs(t, "as built")
			c2, v2 := w.changeAndCheck(t)
			checked, visible, flips = checked+c+c2, visible+v+v2, flips+w.flips
		})
	}
	if checked < n*8 || visible == 0 || visible == checked || flips == 0 {
		t.Fatalf("%d member-channel pairs over %d communities, %d visible: both kinds and a change that turns one around (%d) are needed", checked, n, visible, flips)
	}
	t.Logf("%d communities, %d member-channel pairs, %d visible, %d turned around by a change", n, checked, visible, flips)
}

type randomWorld struct {
	a        *app
	owner    *client
	members  []*client
	st       perm.State
	word     map[uuid.UUID]string  // channel -> its word
	sockets  []*frameLog           // per member, opened before any channel existed
	lastSeen map[string]bool       // member and channel -> visible at the last check
	poster   map[uuid.UUID]*client // who writes the words of a channel
	flips    int                   // pairs whose visibility a change turned around
	r        *rand.Rand
}

func newRandomWorld(t *testing.T, seed uint64) *randomWorld {
	t.Helper()
	a := newApp(t, true)
	owner := a.seedAdmin()
	w := &randomWorld{a: a, owner: owner, word: map[uuid.UUID]string{}, lastSeen: map[string]bool{}, poster: map[uuid.UUID]*client{}, r: rand.New(rand.NewPCG(seed, seed+7))}
	ev := everyoneID(t, owner)
	w.st = perm.State{EveryoneID: ev, Roles: map[uuid.UUID]perm.Role{}, UserRoles: map[uuid.UUID][]uuid.UUID{},
		Owners: map[uuid.UUID]bool{owner.user.ID: true}, Bots: map[uuid.UUID]bool{}, ChannelCat: map[uuid.UUID]uuid.UUID{},
		Categories: map[uuid.UUID]bool{}, DMs: map[uuid.UUID]map[uuid.UUID]bool{}, ChannelOv: map[uuid.UUID]perm.Scope{}, CategoryOv: map[uuid.UUID]perm.Scope{}}
	w.st.Everyone = perm.DefaultEveryone
	if w.r.IntN(3) == 0 {
		w.st.Everyone &^= perm.ViewChannel
	}
	wantStatus(t, "everyone", owner.patch("/api/admin/roles/"+ev.String(), map[string]any{"permissions": w.st.Everyone}), http.StatusOK)
	w.st.Roles[ev] = perm.Role{ID: ev, Everyone: true, Permissions: w.st.Everyone}
	w.roles(t)
	for _, m := range w.members {
		w.sockets = append(w.sockets, recordFrames(t, m))
	}
	w.layout(t)
	w.conversations(t)
	waitForFrames()
	return w
}

func (w *randomWorld) bits() perm.Set {
	return []perm.Set{0, perm.ViewChannel, perm.SendMessages, perm.ViewChannel | perm.SendMessages}[w.r.IntN(4)]
}

// roles creates up to three roles (one sometimes with Administrator) and four
// members holding random subsets of them.
func (w *randomWorld) roles(t *testing.T) {
	t.Helper()
	var ids []uuid.UUID
	for i := range 1 + w.r.IntN(3) {
		p := w.bits() | perm.ViewWiki
		if w.r.IntN(6) == 0 {
			p |= perm.Administrator
		}
		id := createRole(t, w.owner, fmt.Sprintf("r%d", i), p)
		w.st.Roles[id] = perm.Role{ID: id, Permissions: p}
		ids = append(ids, id)
	}
	for i := range 4 {
		m := w.a.register(w.owner, fmt.Sprintf("m%d", i))
		var mine []uuid.UUID
		for _, id := range ids {
			if w.r.IntN(2) == 0 {
				mine = append(mine, id)
			}
		}
		if len(mine) > 0 {
			assignRoles(t, w.owner, m, mine...)
		}
		w.st.UserRoles[m.user.ID] = mine
		w.members = append(w.members, m)
	}
}

// scope sets random overwrites on a channel or category and mirrors them.
func (w *randomWorld) scope(t *testing.T, kind string, id uuid.UUID) perm.Scope {
	t.Helper()
	sc := perm.Scope{Roles: map[uuid.UUID]perm.AllowDeny{}, Members: map[uuid.UUID]perm.AllowDeny{}}
	var list []map[string]any
	for role := range w.st.Roles {
		if w.r.IntN(2) == 0 {
			ad := perm.AllowDeny{Allow: w.bits(), Deny: w.bits()}
			ad.Deny &^= ad.Allow
			sc.Roles[role] = ad
			list = append(list, overwrite("role", role, ad.Allow, ad.Deny))
		}
	}
	for _, m := range w.members {
		if w.r.IntN(4) == 0 {
			ad := perm.AllowDeny{Allow: w.bits(), Deny: w.bits()}
			ad.Deny &^= ad.Allow
			sc.Members[m.user.ID] = ad
			list = append(list, overwrite("member", m.user.ID, ad.Allow, ad.Deny))
		}
	}
	wantStatus(t, "overwrites", putOverwrites(w.owner, kind, id, list...), http.StatusNoContent)
	return sc
}

// layout creates up to two categories and four channels with overwrites, and
// posts one word per channel that mentions every member.
func (w *randomWorld) layout(t *testing.T) {
	t.Helper()
	cats := []uuid.UUID{uuid.Nil}
	for i := range w.r.IntN(3) {
		res := w.owner.post("/api/admin/categories", map[string]any{"name": fmt.Sprintf("cat%d", i)})
		wantStatus(t, "category", res, http.StatusCreated)
		id := idOf(t, res)
		w.st.Categories[id] = true
		w.st.CategoryOv[id] = w.scope(t, "categories", id)
		cats = append(cats, id)
	}
	for i := range 2 + w.r.IntN(3) {
		word := fmt.Sprintf("zqrand%c%c%06d", 'a'+rune(i), 'a'+rune(w.r.IntN(26)), w.r.IntN(1_000_000))
		body := map[string]any{"name": "ch-" + word, "type": "text"}
		cat := cats[w.r.IntN(len(cats))]
		if cat != uuid.Nil {
			body["category_id"] = cat
		}
		res := w.owner.post("/api/admin/channels", body)
		wantStatus(t, "channel", res, http.StatusCreated)
		ch := idOf(t, res)
		w.st.ChannelCat[ch] = cat
		w.st.ChannelOv[ch] = w.scope(t, "channels", ch)
		w.word[ch] = word
		w.poster[ch] = w.owner
		postMessage(t, w.owner, ch, map[string]any{"content": word + " @m0 @m1 @m2 @m3"})
	}
}

// check asks every member every question and returns the pairs compared.
func (w *randomWorld) checkAs(t *testing.T, state string) (pairs, visible int) {
	t.Helper()
	snap := perm.NewSnapshot(w.st, time.Now())
	for i, m := range w.members {
		sub := perm.Subject{ID: m.user.ID}
		listed := string(m.get("/api/channels").body) + string(m.get("/api/mentions").body)
		var frames strings.Builder
		for _, ev := range w.sockets[i].all() {
			frames.WriteString(ev.Type + " " + string(ev.Payload) + "\n")
		}
		info := myPerms(t, m)
		for ch, word := range w.word {
			if word == "" {
				continue // nobody could write there (see say)
			}
			want := snap.Channel(sub, ch)
			sees := want.Has(perm.ViewChannel)
			history := m.get("/api/channels/" + ch.String() + "/messages")
			q := map[string]string{"q": word}
			if _, dm := w.st.DMs[ch]; dm {
				q["dm"] = "true" // conversations are searched on request only (doc/architecture.md)
			}
			found, _ := m.search(t, q)
			got := map[string]bool{
				"channel list or inbox": strings.Contains(listed, word),
				"history":               history.status == http.StatusOK && strings.Contains(string(history.body), word),
				"search":                len(found.Messages) > 0,
				"socket":                strings.Contains(frames.String(), word),
			}
			for surface, ok := range got {
				if msg := w.judge(ch, surface, ok, sees); msg != "" {
					t.Errorf("[%s] %s in %s: %s (perms %b)", state, m.user.Username, word, msg, want)
				}
			}
			if gotBits := channelPerms(info, ch); gotBits != want && sees && !w.private(ch) {
				t.Errorf("[%s] %s in %s: GET /api/permissions %b, the mirror %b", state, m.user.Username, word, gotBits, want)
			}
			pairs++
			key := m.user.ID.String() + ch.String()
			if before, ok := w.lastSeen[key]; ok && before != sees {
				w.flips++
			}
			w.lastSeen[key] = sees
			if sees {
				visible++
			}
		}
	}
	return pairs, visible
}

// waitForFrames gives the hub time to deliver what was just posted.
func waitForFrames() { time.Sleep(300 * time.Millisecond) }
