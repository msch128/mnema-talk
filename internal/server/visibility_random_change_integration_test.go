//go:build integration

package server

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Right at the start, wrong later: after a random community has been checked,
// one thing changes (@everyone's or a role's permissions, a member's roles,
// the overwrites of a channel or a category), the mirror follows, every
// channel gets a fresh word, and the same questions are asked at once. A
// route, a cache or the hub that still answers from the old state disagrees
// with the mirror. Three changes per community.

// change applies one random change through the API and mirrors it; it
// returns what it did, for the failure message.
func (w *randomWorld) change(t *testing.T) string {
	t.Helper()
	switch w.r.IntN(5) {
	case 0:
		w.st.Everyone ^= perm.ViewChannel
		wantStatus(t, "everyone", w.owner.patch("/api/admin/roles/"+w.st.EveryoneID.String(), map[string]any{"permissions": w.st.Everyone}), http.StatusOK)
		w.st.Roles[w.st.EveryoneID] = perm.Role{ID: w.st.EveryoneID, Everyone: true, Permissions: w.st.Everyone}
		return "@everyone's View channels flipped"
	case 1:
		role := w.customRole()
		if role == uuid.Nil {
			return "nothing (no custom role)"
		}
		r := w.st.Roles[role]
		r.Permissions ^= perm.ViewChannel
		w.st.Roles[role] = r
		wantStatus(t, "role", w.owner.patch("/api/admin/roles/"+role.String(), map[string]any{"permissions": r.Permissions}), http.StatusOK)
		return "a role's View channels flipped"
	case 2:
		m := w.members[w.r.IntN(len(w.members))]
		var roles []uuid.UUID
		for id, r := range w.st.Roles {
			if !r.Everyone && w.r.IntN(2) == 0 {
				roles = append(roles, id)
			}
		}
		if res := setRoles(w.owner, m.user.ID, roles...); res.status != http.StatusNoContent {
			t.Fatalf("roles: %d %s", res.status, res.body)
		}
		w.st.UserRoles[m.user.ID] = roles
		return "a member's roles replaced"
	case 3:
		ch := w.anyChannel()
		w.st.ChannelOv[ch] = w.scope(t, "channels", ch)
		return "a channel's overwrites replaced"
	}
	for cat := range w.st.Categories {
		w.st.CategoryOv[cat] = w.scope(t, "categories", cat)
		return "a category's overwrites replaced"
	}
	return "nothing (no category)"
}

func (w *randomWorld) customRole() uuid.UUID {
	for id, r := range w.st.Roles {
		if !r.Everyone {
			return id
		}
	}
	return uuid.Nil
}

func (w *randomWorld) anyChannel() uuid.UUID {
	for ch := range w.word {
		return ch
	}
	return uuid.Nil
}

// freshWords posts a new word in every channel, mentioning every member.
func (w *randomWorld) freshWords(t *testing.T, round int) {
	t.Helper()
	for ch := range w.word {
		word := fmt.Sprintf("zqchg%d%c%07d", round, 'a'+rune(w.r.IntN(26)), w.r.IntN(10_000_000))
		postMessage(t, w.owner, ch, map[string]any{"content": word + " @m0 @m1 @m2 @m3"})
		w.word[ch] = word
	}
}

// changeAndCheck runs three changes, each followed by fresh words and the
// full check, and returns the pairs compared.
func (w *randomWorld) changeAndCheck(t *testing.T) (pairs, visible int) {
	t.Helper()
	for round := range 3 {
		what := w.change(t)
		w.freshWords(t, round)
		waitForFrames()
		p, v := w.checkAs(t, what)
		pairs, visible = pairs+p, visible+v
	}
	return pairs, visible
}
