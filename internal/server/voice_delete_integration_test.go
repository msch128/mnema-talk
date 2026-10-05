//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// Deleting a voice channel disconnects everyone in its room and ends their
// presence there, instead of leaving them in a room that no longer exists.
func TestDeletingVoiceChannelEvictsMembers(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	voice := a.createChannel(admin, "Lounge", "voice")

	watcher, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	member, _, err := max.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	member.send("voice_join", map[string]any{"channel_id": voice})
	if _, ok := watcher.expect("voice_state_update", 2*time.Second); !ok {
		t.Fatal("voice join not announced")
	}

	if res := admin.delete("/api/admin/channels/" + voice.String()); res.status != http.StatusNoContent {
		t.Fatalf("delete channel: %d %s", res.status, res.body)
	}
	payload, ok := member.expect("voice_kicked", 2*time.Second)
	if !ok || !strings.Contains(string(payload), voice.String()) {
		t.Fatalf("member not told about the closed room: %s", payload)
	}
	payload, ok = watcher.expect("voice_state_update", 2*time.Second)
	if !ok || !strings.Contains(string(payload), `"leave"`) {
		t.Fatalf("leave not announced: %s", payload)
	}
	if snap := a.router.Hub.OnlineUserIDs(); len(snap) == 0 {
		t.Fatal("connections must stay open; only the voice room ends")
	}
}
