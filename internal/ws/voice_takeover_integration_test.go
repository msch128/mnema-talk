//go:build integration

package ws

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func joinFrame(t *testing.T, ch uuid.UUID, takeover bool) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"channel_id": ch, "takeover": takeover})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The voice_join frame with takeover:true, through the real handler, database
// and permission checks: the other session's voice ends with the reason, its
// connection and text events carry on, and a join without the flag keeps the
// old behaviour (another Talk is left alone).
func TestVoiceJoinFrameWithTakeoverMovesVoiceBetweenSessions(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	var oldID, newID uuid.UUID
	for _, id := range []*uuid.UUID{&oldID, &newID} {
		if err := p.QueryRow(ctx, `INSERT INTO channels (name, type) VALUES ('Talk', 'voice') RETURNING id`).Scan(id); err != nil {
			t.Fatal(err)
		}
	}
	h := NewHub(p, nil, newSeatTestSFU(t), nil)
	h.SetPermissions(perm.NewService(p))
	t.Cleanup(h.Close)
	u := wsFixtureUser(t, p)
	a, b := testClient(h, u), testClient(h, u)
	defer h.unregister(a)
	defer h.unregister(b)

	a.handle("voice_join", joinFrame(t, oldID, false))
	if !seatedIn(a, oldID) {
		t.Fatal("first session did not join")
	}
	// Without the flag the second session sits in its own Talk, as before.
	b.handle("voice_join", joinFrame(t, newID, false))
	if !seatedIn(a, oldID) || !seatedIn(b, newID) {
		t.Fatal("a plain join in another Talk changed the other session")
	}
	h.leaveCurrentVoice(b)
	sentEvents(t, a, "voice_kicked")

	b.handle("voice_join", joinFrame(t, newID, true))

	if a.currentVoice() != nil || !seatedIn(b, newID) {
		t.Fatalf("takeover did not move the voice: a=%v b=%v", a.currentVoice(), b.currentVoice())
	}
	kicked := sentEvents(t, a, "voice_kicked")
	if len(kicked) != 1 || kicked[0]["reason"] != "voice_taken_over" || kicked[0]["channel_id"] != oldID.String() {
		t.Fatalf("voice_kicked = %+v", kicked)
	}
	if a.closed.Load() {
		t.Fatal("the old session was disconnected")
	}
	a.SendEvent("message_create", map[string]any{"id": "m"})
	if len(sentEvents(t, a, "message_create")) != 1 {
		t.Fatal("the old session lost its text events")
	}
	if inRoom(h, oldID, u.ID) || !inRoom(h, newID, u.ID) {
		t.Fatal("presence did not move with the voice")
	}
}
