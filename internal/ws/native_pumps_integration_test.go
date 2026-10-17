//go:build integration

package ws

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// eventFeed reads a socket continuously (so control frames such as the
// server's ping are answered) and hands out the event types it sees. A
// gorilla connection cannot be read again after a read timeout, hence the
// goroutine instead of deadline polling.
type eventFeed struct {
	events chan string
}

func newEventFeed(c *websocket.Conn) *eventFeed {
	f := &eventFeed{events: make(chan string, 4096)}
	go func() {
		defer close(f.events)
		for {
			_, raw, err := c.ReadMessage()
			if err != nil {
				return
			}
			var event struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(raw, &event) == nil {
				select {
				case f.events <- event.Type:
				default:
				}
			}
		}
	}()
	return f
}

// await returns once an event of the wanted type arrived.
func (f *eventFeed) await(t *testing.T, want string) {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case got, ok := <-f.events:
			if !ok {
				t.Fatalf("socket closed while waiting for %q", want)
			}
			if got == want {
				return
			}
		case <-timeout:
			t.Fatalf("event %q never arrived", want)
		}
	}
}

// quiet collects the event types that arrive until none has for d.
func (f *eventFeed) quiet(d time.Duration) []string {
	var seen []string
	for {
		select {
		case got, ok := <-f.events:
			if !ok {
				return seen
			}
			seen = append(seen, got)
		case <-time.After(d):
			return seen
		}
	}
}

func (f *eventFeed) requireNone(t *testing.T, event string) {
	t.Helper()
	for _, got := range f.quiet(150 * time.Millisecond) {
		if got == event {
			t.Fatalf("unexpected %q event", event)
		}
	}
}

// requireClosed waits for the server to end the connection.
func (f *eventFeed) requireClosed(t *testing.T) {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-f.events:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("socket not closed")
		}
	}
}

// Frames over the per-second budget are dropped, not punished: the socket
// stays open and serves the next window.
func TestNativeMetadataDropsFloodedFramesAndKeepsServing(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	s, d := metadataTLSFixture(t, a, a.native)
	c := metadataDial(t, s, d, grant.Access)
	feed := newEventFeed(c)
	feed.await(t, "presence_snapshot")
	ping := []byte(`{"type":"ping","payload":{"t":1}}`)
	for range maxFramesPerSec + 60 {
		if err := c.WriteMessage(websocket.TextMessage, ping); err != nil {
			t.Fatalf("flood frame refused: %v", err)
		}
	}
	feed.quiet(300 * time.Millisecond)
	time.Sleep(time.Second) // the budget window rolls over
	if err := c.WriteMessage(websocket.TextMessage, ping); err != nil {
		t.Fatal(err)
	}
	feed.await(t, "pong")
}

// A hub that closes between authentication and the upgrade answers 503
// instead of admitting a socket nobody will tear down.
type closeHubAfterAuth struct {
	NativeAuthenticator
	hub *Hub
}

func (a closeHubAfterAuth) AuthenticateNativeRequest(r *http.Request) (auth.NativeLease, error) {
	lease, err := a.NativeAuthenticator.AuthenticateNativeRequest(r)
	a.hub.Close()
	return lease, err
}

func TestNativeMetadataRefusesAdmissionWhenHubClosesMidHandshake(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	s, _ := metadataTLSFixture(t, a, closeHubAfterAuth{NativeAuthenticator: a.native, hub: a.hub})
	req, err := http.NewRequest(http.MethodGet, s.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+grant.Access)
	res, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("handshake while the hub closes: status %d", res.StatusCode)
	}
}

// The server's heartbeat reaches a metadata socket, and a socket whose
// security state was sealed is closed by its next write.
func TestNativeMetadataHeartbeatAndSealedWriter(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	a.hub.PingInterval = 20 * time.Millisecond
	grant := a.login(t)
	s, d := metadataTLSFixture(t, a, a.native)
	c := metadataDial(t, s, d, grant.Access)
	var pings atomic.Int32
	defaultPing := c.PingHandler()
	c.SetPingHandler(func(data string) error {
		pings.Add(1)
		return defaultPing(data)
	})
	feed := newEventFeed(c)
	feed.quiet(300 * time.Millisecond)
	if pings.Load() < 2 {
		t.Fatalf("heartbeats received: %d", pings.Load())
	}
	a.client(t, grant.Family).native.securityClosed.Store(true)
	feed.requireClosed(t)
	metadataDrainMembership(t, a.hub)
}

func TestNativeMetadataSealedWriterStopsOnBroadcast(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	s, d := metadataTLSFixture(t, a, a.native)
	c := metadataDial(t, s, d, grant.Access)
	feed := newEventFeed(c)
	feed.await(t, "presence_snapshot")
	a.client(t, grant.Family).native.securityClosed.Store(true)
	a.hub.Broadcast("channels_changed", map[string]any{})
	feed.requireClosed(t)
	metadataDrainMembership(t, a.hub)
}

func TestNativeMetadataRejectsNonMetadataEvent(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	s, d := metadataTLSFixture(t, a, a.native)
	c := metadataDial(t, s, d, grant.Access)
	feed := newEventFeed(c)
	feed.await(t, "presence_snapshot")
	if err := c.WriteMessage(websocket.TextMessage, []byte(`{"type":"voice_join","payload":{}}`)); err != nil {
		t.Fatal(err)
	}
	feed.requireClosed(t)
	metadataDrainMembership(t, a.hub)
}

// A full native socket whose security state was sealed ends on its next read,
// write or heartbeat; the browser-only revalidation tick leaves native
// sockets alone.
func TestNativeSocketSealedStateEndsReadWriteAndHeartbeat(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		a := newNativeWSApp(t, nil, nil)
		grant := a.login(t)
		c := a.dial(t, grant)
		feed := newEventFeed(c)
		feed.await(t, "voice_rooms")
		a.client(t, grant.Family).native.securityClosed.Store(true)
		if err := c.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping","payload":{}}`)); err != nil {
			t.Fatal(err)
		}
		feed.requireClosed(t)
	})
	t.Run("write", func(t *testing.T) {
		a := newNativeWSApp(t, nil, nil)
		grant := a.login(t)
		c := a.dial(t, grant)
		feed := newEventFeed(c)
		feed.await(t, "voice_rooms")
		a.client(t, grant.Family).native.securityClosed.Store(true)
		a.hub.Broadcast("channels_changed", map[string]any{})
		feed.requireClosed(t)
	})
	t.Run("heartbeat", func(t *testing.T) {
		a := newNativeWSApp(t, nil, nil)
		a.hub.PingInterval = 20 * time.Millisecond
		grant := a.login(t)
		c := a.dial(t, grant)
		feed := newEventFeed(c)
		feed.await(t, "voice_rooms")
		a.client(t, grant.Family).native.securityClosed.Store(true)
		feed.requireClosed(t)
	})
	t.Run("revalidation tick", func(t *testing.T) {
		a := newNativeWSApp(t, nil, nil)
		a.hub.RevalidateInterval = 20 * time.Millisecond
		grant := a.login(t)
		c := a.dial(t, grant)
		feed := newEventFeed(c)
		feed.await(t, "voice_rooms")
		feed.quiet(200 * time.Millisecond) // several ticks pass
		if err := c.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping","payload":{}}`)); err != nil {
			t.Fatal(err)
		}
		feed.await(t, "pong")
	})
}

// Application events from a native socket reach exactly the members they
// are meant for: not the sender's account when excluded, not members outside
// the voice room, nothing when the payload cannot be encoded, and nothing
// once the sender's security state is sealed.
func TestNativeApplicationEventAudience(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	first, second := a.login(t), a.login(t)
	sender, listener := a.dial(t, first), a.dial(t, second)
	senderFeed, listenerFeed := newEventFeed(sender), newEventFeed(listener)
	senderFeed.await(t, "voice_rooms")
	listenerFeed.await(t, "voice_rooms")
	listenerFeed.quiet(100 * time.Millisecond)
	client := a.client(t, first.Family)

	client.emitApplicationEvent("audience_unencodable", make(chan int), false, nil)
	listenerFeed.requireNone(t, "audience_unencodable")

	client.emitApplicationEvent("audience_excluded", map[string]any{}, true, nil)
	listenerFeed.requireNone(t, "audience_excluded") // same account as the sender

	room := uuid.New()
	client.emitApplicationEvent("audience_room", map[string]any{}, false, &room)
	listenerFeed.requireNone(t, "audience_room") // the listener is not in that room

	client.emitApplicationEvent("audience_everyone", map[string]any{}, false, nil)
	listenerFeed.await(t, "audience_everyone")

	client.native.securityClosed.Store(true)
	client.emitApplicationEvent("audience_sealed", map[string]any{}, false, nil)
	listenerFeed.requireNone(t, "audience_sealed")
}

// A bogus answer or candidate on an established native media session is
// rejected without a log line that carries request data, and the socket
// survives.
func TestNativeMediaRejectsBogusAnswerAndCandidate(t *testing.T) {
	relay, err := sfu.NewSFU(0, 0, []string{"127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relay.Close() })
	a := newNativeWSApp(t, relay, nil)
	channel := mediaProbeChannel(t, a)
	grant := a.login(t)
	conn := a.dial(t, grant)
	readNativeEvent(t, conn, "voice_rooms")

	var logs lockedBuffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	endpoint := mediaProbeAttach(t, conn, channel, false)
	client := a.client(t, grant.Family)
	mediaProbeWait(t, func() bool { return client.peer() != nil }, "native peer never created")
	// Before the answer a candidate is held for it; once the session has a
	// remote description a bogus one is refused on the spot.
	mediaProbeWait(t, func() bool { return client.peer().PC.RemoteDescription() != nil }, "native session never negotiated")
	secret := "bogus-" + uuid.NewString()
	if err := endpoint.write("webrtc_answer", map[string]string{"type": "answer", "sdp": secret}); err != nil {
		t.Fatal(err)
	}
	if err := endpoint.write("webrtc_candidate", map[string]string{"candidate": secret}); err != nil {
		t.Fatal(err)
	}
	mediaProbeWait(t, func() bool {
		out := logs.String()
		return strings.Contains(out, "native sfu answer rejected") && strings.Contains(out, "sfu remote candidate dropped")
	}, "bogus native answer/candidate not rejected")
	if strings.Contains(logs.String(), secret) {
		t.Fatal("rejection log carried request data")
	}
	select {
	case <-endpoint.ended:
		t.Fatal("socket closed after a rejected answer/candidate")
	default:
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
