//go:build integration

package server

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Conversations in the random communities: up to two among the members (two
// to four of them, so an Administrator member is in some and not in others),
// each answered once so its members may keep writing, sometimes with a call.
// The mirror records the members; the snapshot then says that only they read
// it, whatever roles anyone holds, and the same routes must agree.

// conversations creates them, mirrors them and posts a word in each.
func (w *randomWorld) conversations(t *testing.T) {
	t.Helper()
	for range w.r.IntN(3) {
		var in []*client
		for _, m := range w.members {
			if w.r.IntN(2) == 0 {
				in = append(in, m)
			}
		}
		if len(in) < 2 {
			continue
		}
		dm := in[0].mustDM(t, in[1:]...).ID
		w.st.DMs[dm] = map[uuid.UUID]bool{}
		for _, m := range in {
			w.st.DMs[dm][m.user.ID] = true
		}
		w.poster[dm] = in[0]
		word := fmt.Sprintf("zqdm%c%07d", 'a'+rune(w.r.IntN(26)), w.r.IntN(10_000_000))
		postMessage(t, in[0], dm, map[string]any{"content": word + " @m0 @m1 @m2 @m3"})
		postMessage(t, in[1], dm, map[string]any{"content": "ok"})
		w.word[dm] = word
		if w.r.IntN(2) == 0 {
			w.call(t, in[0], dm)
		}
	}
}

// call starts the conversation's Talk and mirrors it; its chat is a channel
// the members write in like any other.
func (w *randomWorld) call(t *testing.T, starter *client, dm uuid.UUID) {
	t.Helper()
	res := starter.post("/api/dms/"+dm.String()+"/talk", nil)
	if res.status != http.StatusCreated && res.status != http.StatusOK {
		t.Fatalf("call: %d %s", res.status, res.body)
	}
	var talk struct {
		ID uuid.UUID `json:"channel_id"`
	}
	res.decode(t, &talk)
	if w.st.DMTalks == nil {
		w.st.DMTalks = map[uuid.UUID]uuid.UUID{}
	}
	w.st.DMTalks[talk.ID] = dm
	w.st.ChannelCat[talk.ID] = uuid.Nil
	w.poster[talk.ID] = starter
	w.word[talk.ID] = w.say(t, talk.ID, fmt.Sprintf("zqcall%c%07d", 'a'+rune(w.r.IntN(26)), w.r.IntN(10_000_000)))
}

// private reports whether ch belongs to a conversation (no overwrites, no
// entry in GET /api/permissions' channel list to compare).
func (w *randomWorld) private(ch uuid.UUID) bool {
	_, dm := w.st.DMs[ch]
	_, call := w.st.DMTalks[ch]
	return dm || call
}

// judge says what is wrong with one surface's answer, or "". A conversation is
// never in the channel list or the mention inbox, for its members too
// (doc/architecture.md); the chat of its call reaches the inbox of members
// (not of its author, a self-mention), so there only a word reaching a
// non-member counts. Everything else must match the snapshot exactly.
func (w *randomWorld) judge(ch uuid.UUID, surface string, ok, sees bool) string {
	_, dm := w.st.DMs[ch]
	_, call := w.st.DMTalks[ch]
	switch {
	case surface == "channel list or inbox" && dm:
		if ok {
			return "a conversation's word is in the channel list or inbox"
		}
	case surface == "channel list or inbox" && call:
		if ok && !sees {
			return "a call's word reached a non-member's channel list or inbox"
		}
	case ok != sees:
		return fmt.Sprintf("%s says %v, the snapshot of the mirror says %v", surface, ok, sees)
	}
	return ""
}

// say posts word in ch as its poster and returns it, or "" when the mirror
// says the poster cannot see ch: a call's chat follows the community's View
// channels (H-L6-04), so a call may be closed to all of its members. A
// channel without a word is left out of the checks.
func (w *randomWorld) say(t *testing.T, ch uuid.UUID, word string) string {
	t.Helper()
	p := w.poster[ch]
	snap := perm.NewSnapshot(w.st, time.Now())
	if !snap.Channel(perm.Subject{ID: p.user.ID, Owner: p == w.owner}, ch).Has(perm.SendMessages | perm.ViewChannel) {
		return ""
	}
	postMessage(t, p, ch, map[string]any{"content": word + " @m0 @m1 @m2 @m3"})
	return word
}
