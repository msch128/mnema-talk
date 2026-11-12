//go:build integration

package server

import (
	"encoding/base64"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The conversation half of the revocation matrix. The Talk of a conversation
// is for its members only (doc/talks.md), so a change that takes membership
// away must end what the member held in that Talk, as a role or overwrite
// change does in TestRevocationMatrix. Each row takes membership away from
// carol in a fresh conversationWorld while she and alice sit in its Talk.
//
// Cells and bounds:
//   - seat: at once (internal/ws/perms.go:156-160), within 500 ms.
//   - told: the remaining members see her leave the call (voice_state_update
//     leave), so their voice list matches the member list.
//   - room events: talk_reaction is sent to the room by seat, so it stops
//     only when the seat does (internal/ws/voice_mute.go sendToVoiceRoomExcept).
//   - key messages: media_handshake, the same room relay (internal/ws/media.go).
const (
	cellTold        = "told"
	cellRoomEvents  = "room events"
	cellKeyMessages = "key messages"
)

// seatBound is the seat's bound in TestRevocationMatrix for a member whose
// session goes on.
const seatBound = 500 * time.Millisecond

var conversationCells = []string{cellSeat, cellTold, cellRoomEvents, cellKeyMessages}

type conversationRow struct {
	name   string
	revoke func(*testing.T, *conversationWorld)
}

func conversationRows() []conversationRow {
	return []conversationRow{
		{name: "member leaves a group conversation", revoke: func(t *testing.T, w *conversationWorld) {
			wantStatus(t, "carol leaves", w.carol.delete("/api/dms/"+w.dm.String()+"/members/@me"), http.StatusNoContent)
		}},
	}
}

// conversationWorld is a group conversation of alice, bob and carol whose Talk
// alice started and alice and carol sit in.
type conversationWorld struct {
	*dmFixture
	dm, talk         uuid.UUID
	aliceWS, carolWS *wsConn
}

func newConversationWorld(t *testing.T) *conversationWorld {
	t.Helper()
	f := newDMFixture(t, true)
	w := &conversationWorld{dmFixture: f, dm: f.alice.mustDM(t, f.bob, f.carol).ID}
	res := f.alice.post("/api/dms/"+w.dm.String()+"/talk", nil)
	wantStatus(t, "start a Talk", res, http.StatusCreated)
	var out struct {
		ChannelID uuid.UUID `json:"channel_id"`
	}
	res.decode(t, &out)
	w.talk = out.ChannelID
	w.aliceWS = f.alice.dialLiveWSQuery(f.a.origin(), "?dm=1")
	w.carolWS = f.carol.dialLiveWSQuery(f.a.origin(), "?dm=1")
	w.aliceWS.send("voice_join", map[string]any{"channel_id": w.talk})
	w.carolWS.send("voice_join", map[string]any{"channel_id": w.talk})
	deadline := time.Now().Add(soon)
	for !w.seated(f.alice) || !w.seated(f.carol) {
		if time.Now().After(deadline) {
			t.Fatal("alice and carol could not take a seat in the conversation's Talk")
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.aliceWS.drain()
	w.carolWS.drain()
	return w
}

func (w *conversationWorld) seated(c *client) bool {
	return slices.Contains(w.a.router.Hub.VoiceMembers(w.talk), c.user.ID)
}

// measure probes every cell right after the revocation answered.
func (w *conversationWorld) measure() map[string]cellResult {
	start := time.Now()
	got := map[string]cellResult{}
	got[cellSeat] = w.probeSeat(start)
	got[cellTold] = w.probeTold(start)
	w.carolWS.drain()
	w.aliceWS.send("talk_reaction", map[string]any{"emoji": "👍"})
	_, relayed := w.carolWS.expect("talk_reaction", time.Second)
	got[cellRoomEvents] = cellResult{stopped: !relayed, took: time.Since(start)}
	// An MLS message, framing only (0, version 1, 0, wire format 5): the hub
	// relays it without reading it.
	wire := base64.StdEncoding.EncodeToString(append([]byte{0, 1, 0, 5}, make([]byte, 64)...))
	w.aliceWS.send("media_handshake", map[string]any{"channel_id": w.talk, "wire": wire})
	_, relayed = w.carolWS.expect("media_handshake", time.Second)
	got[cellKeyMessages] = cellResult{stopped: !relayed, took: time.Since(start)}
	return got
}

func (w *conversationWorld) probeSeat(start time.Time) cellResult {
	deadline := start.Add(seatBound + time.Second) // past the bound, to measure how far off it is
	for w.seated(w.carol) {
		if time.Now().After(deadline) {
			return cellResult{stopped: false, took: time.Since(start)}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cellResult{stopped: true, took: time.Since(start)}
}

// probeTold reports whether alice learned that carol left the call. "stopped"
// here means the stale listing stopped: the leave reached her.
func (w *conversationWorld) probeTold(start time.Time) cellResult {
	deadline := time.After(time.Second)
	for {
		select {
		case ev, ok := <-w.aliceWS.events:
			if !ok {
				return cellResult{took: time.Since(start), note: "(alice's socket closed)"}
			}
			p := string(ev.Payload)
			if ev.Type == "voice_state_update" && strings.Contains(p, `"leave"`) && strings.Contains(p, w.carol.user.ID.String()) {
				return cellResult{stopped: true, took: time.Since(start)}
			}
		case <-deadline:
			return cellResult{took: time.Since(start), note: "(no voice leave for carol)"}
		}
	}
}

func TestConversationRevocationMatrix(t *testing.T) {
	for _, row := range conversationRows() {
		t.Run(strings.ReplaceAll(row.name, " ", "_"), func(t *testing.T) {
			w := newConversationWorld(t)
			row.revoke(t, w)
			got := w.measure()
			line := []string{row.name}
			for _, cell := range conversationCells {
				res := got[cell]
				line = append(line, cell+": "+res.String())
				switch {
				case !res.stopped:
					t.Errorf("%s / %s stayed open: %s", row.name, cell, res)
				case cell == cellSeat && res.took > seatBound:
					t.Errorf("%s / %s stopped after %v, bound %v", row.name, cell, res.took, seatBound)
				}
			}
			t.Log(strings.Join(line, " | "))
		})
	}
}
