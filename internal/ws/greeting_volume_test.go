package ws

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/pion/webrtc/v4"
)

// A community with hundreds of visible publishers still gets every media
// state on connect: the greeting must not overflow the send buffer, which
// would close the connection and send the client into a reconnect loop.
func TestGreetingWithManyPublishersReachesTheClientIntact(t *testing.T) {
	const publishers, perRoom = 300, 10
	h, voiceSFU := newSFUHub(t)
	h.Sessions = fakeAuth{user: auth.User{ID: uuid.New(), Username: "viewer"}}
	t.Cleanup(h.Close)
	for r := 0; r < publishers/perRoom; r++ {
		ch := uuid.New()
		for i := 0; i < perRoom; i++ {
			user := uuid.New()
			if _, _, err := voiceSFU.Join(ch, user, func(webrtc.SessionDescription) {}, func(*webrtc.ICECandidateInit) {}); err != nil {
				t.Fatal(err)
			}
			if err := voiceSFU.Room(ch).PublishTestVideo(user, "camera"); err != nil {
				t.Fatal(err)
			}
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	t.Cleanup(srv.Close)
	h.Origins = []string{srv.URL}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	var first string
	states := 0
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for states < publishers {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("connection ended after %d of %d media states: %v", states, publishers, err)
		}
		var ev Event
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatal(err)
		}
		if first == "" {
			first = ev.Type
		}
		if ev.Type == "webrtc_media_state" {
			states++
		}
	}
	if first != "server_info" {
		t.Fatalf("first event = %q, want server_info", first)
	}
}
