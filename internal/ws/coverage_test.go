package ws

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/sfu"
)

func TestUnencodableEventsNeverReachRecipients(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	c := testClient(h, auth.User{ID: uuid.New()})
	ch := voiceCh()
	h.joinVoice(c, ch)
	drainTypes(t, c)
	bad := make(chan int)
	h.Broadcast("user_update", bad)
	h.broadcastExcept(uuid.New(), "bad", bad)
	h.SendToUsers([]uuid.UUID{c.User.ID}, "bad", bad)
	h.sendToVoiceRoom(ch.ID, "bad", bad)
	c.SendEvent("bad", bad)
	if len(c.send) != 0 {
		t.Fatal("unencodable event was delivered")
	}
}

func TestTypingEvictsExpiredChannelEntries(t *testing.T) {
	c := newClient(nil, nil, auth.User{}, 0)
	now := time.Now()
	for range maxTypingChannels {
		if !c.allowTyping(uuid.New(), now) {
			t.Fatal("initial channel refused")
		}
	}
	fresh := uuid.New()
	if c.allowTyping(fresh, now) {
		t.Fatal("bounded typing cache accepted new entry while full")
	}
	if !c.allowTyping(fresh, now.Add(typingThrottle)) {
		t.Fatal("expired channel entries blocked a new channel")
	}
	if len(c.typing) != 1 {
		t.Fatalf("expired entries left in cache: %d", len(c.typing))
	}
}

func TestHubIgnoresUnknownClientIdleAndInvalidProfiles(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	c := testClient(h, auth.User{ID: uuid.New(), DisplayName: "Original"})
	drainTypes(t, c)
	stranger := newClient(h, nil, auth.User{ID: uuid.New()}, 0)
	h.setIdle(stranger, true)
	if stranger.idle || len(c.send) != 0 {
		t.Fatal("unknown client changed live presence")
	}
	h.applyUserUpdate((*auth.User)(nil))
	h.applyUserUpdate(auth.User{DisplayName: "No identity"})
	update := c.User
	update.DisplayName = "Updated"
	h.applyUserUpdate(&update)
	h.mu.RLock()
	got := h.profileLocked(c)
	fallback := h.profileLocked(stranger)
	h.mu.RUnlock()
	if got.DisplayName != "Updated" || fallback.ID != stranger.User.ID {
		t.Fatal("profile refresh or unregistered fallback failed")
	}
	h.mu.Lock()
	h.chosen[c.User.ID] = ""
	h.mu.Unlock()
	if h.presenceSnapshot()[c.User.ID] != auth.PresenceOnline {
		t.Fatal("unset chosen presence did not default online")
	}
}

func TestStatsCountDistinctUsersAndOccupiedRooms(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	u := auth.User{ID: uuid.New()}
	a := testClient(h, u)
	b := testClient(h, u)
	c := testClient(h, auth.User{ID: uuid.New()})
	first, second := voiceCh(), voiceCh()
	h.joinVoice(a, first)
	h.joinVoice(b, second)
	h.joinVoice(c, first)
	want := Stats{Connections: 3, OnlineUsers: 2, VoiceRooms: 2, VoiceParticipants: 3}
	if got := h.Stats(); got != want {
		t.Fatalf("stats = %+v, want %+v", got, want)
	}
}

func TestVoiceSwitchCancelsOldGraceAndPreservesNewRoom(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	h.VoiceGrace = time.Hour
	t.Cleanup(h.Close)
	u := auth.User{ID: uuid.New()}
	old := testClient(h, u)
	first, second := voiceCh(), voiceCh()
	h.joinVoice(old, first)
	h.unregister(old)
	next := testClient(h, u)
	h.joinVoice(next, second)
	if inRoom(h, first.ID, u.ID) || !inRoom(h, second.ID, u.ID) {
		t.Fatal("switching after disconnect retained old room")
	}
	h.joinVoice(next, first)
	if inRoom(h, second.ID, u.ID) || !inRoom(h, first.ID, u.ID) {
		t.Fatal("switching live rooms retained previous room")
	}
	h.joinVoice(next, first)
	if next.currentVoice() == nil || *next.currentVoice() != first.ID {
		t.Fatal("same-room renegotiation left room")
	}
	h.mu.RLock()
	graces := len(h.grace)
	h.mu.RUnlock()
	if graces != 0 {
		t.Fatal("room switch retained grace timer")
	}
	next.shutdown()
	h.joinVoice(next, second)
	if *next.currentVoice() != first.ID {
		t.Fatal("closed client joined another room")
	}
	fresh := newClient(h, nil, auth.User{ID: uuid.New()}, 0)
	h.Close()
	h.joinVoice(fresh, second)
	if fresh.currentVoice() != nil {
		t.Fatal("closed hub accepted voice join")
	}
}

func TestKickAndGraceReplacement(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	h.VoiceGrace = time.Hour
	t.Cleanup(h.Close)
	c := testClient(h, auth.User{ID: uuid.New()})
	ch := voiceCh()
	h.joinVoice(c, ch)
	h.unregister(c)
	key := voiceKey{c.User.ID, ch.ID}
	h.startGrace(key)
	if !h.KickFromVoice(c.User.ID) || inRoom(h, ch.ID, c.User.ID) {
		t.Fatal("kick did not remove disconnected participant")
	}
	if h.KickFromVoice(c.User.ID) {
		t.Fatal("repeated kick reported a removed participant")
	}
}

func TestVoiceMuteOutsideRoomAndRepeatedStateAreIgnored(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	c := testClient(h, auth.User{ID: uuid.New()})
	drainTypes(t, c)
	h.setMuteState(c, MuteState{Muted: true})
	if len(h.voiceMute) != 0 || len(c.send) != 0 {
		t.Fatal("mute outside voice modified room state")
	}
	h.joinVoice(c, voiceCh())
	h.setMuteState(c, MuteState{Muted: true})
	drainTypes(t, c)
	h.setMuteState(c, MuteState{Muted: true})
	if len(c.send) != 0 {
		t.Fatal("unchanged mute state emitted an event")
	}
}

func TestSignalingRejectsMalformedAndUnavailableSubscriptions(t *testing.T) {
	h, s := newSFUHub(t)
	ch := voiceCh()
	c := testClient(h, auth.User{ID: uuid.New()})
	h.joinVoice(c, ch)
	t.Cleanup(func() { h.leaveCurrentVoice(c); h.Close() })
	for _, typ := range []string{"webrtc_answer", "webrtc_candidate", "voice_mute_state", "webrtc_subscribe"} {
		c.handle(typ, []byte(`{`))
	}
	c.handle("webrtc_answer", []byte(`{"type":"answer","sdp":"invalid SDP"}`))
	c.handle("webrtc_candidate", []byte(`{"candidate":"invalid candidate"}`))
	c.handle("webrtc_diag", []byte(`{"connection":"testing"}`))
	valid := []byte(`{"kind":"camera","all":true,"on":true}`)
	outsider := newClient(h, nil, auth.User{ID: uuid.New()}, 0)
	outsider.setVoice(&ch.ID)
	outsider.handleSubscribe(valid) // room exists, but the viewer has no media peer.
	absent := uuid.New()
	outsider.setVoice(&absent)
	outsider.handleSubscribe(valid)
	c.handle("webrtc_request_keyframe", nil)
	c.handle("webrtc_screenshare_start", nil)
	room := s.Room(ch.ID)
	if err := room.PublishTestVideo(c.User.ID, sfu.SourceCamera); err != nil {
		t.Fatal(err)
	}
	c.handle("webrtc_camera_stop", nil)
	if room.MediaStates()[c.User.ID].Camera {
		t.Fatal("camera_stop retained the published camera")
	}
	c.setPeer(nil)
	c.handle("webrtc_answer", []byte(`{"type":"answer","sdp":"invalid SDP"}`))
	c.handle("webrtc_candidate", []byte(`{"candidate":"invalid candidate"}`))
}

func TestFreshConnectionReceivesExistingMediaState(t *testing.T) {
	h, s := newSFUHub(t)
	ch := voiceCh()
	sharer := testClient(h, auth.User{ID: uuid.New()})
	h.joinVoice(sharer, ch)
	t.Cleanup(func() { h.CloseVoiceChannel(ch.ID); h.Close() })
	if err := s.Room(ch.ID).PublishTestVideo(sharer.User.ID, sfu.SourceScreen); err != nil {
		t.Fatal(err)
	}
	newcomer := testClient(h, auth.User{ID: uuid.New()})
	if got := events(t, newcomer, "webrtc_media_state"); len(got) < 1 || got[0]["user_id"] != sharer.User.ID.String() || got[0]["screen"] != true {
		t.Fatalf("initial media snapshot: %+v", got)
	}
}

type rejectedCoverageAuth struct{ err error }

func (a rejectedCoverageAuth) AuthenticateRequest(*http.Request) (*auth.User, int, error) {
	return nil, 0, a.err
}
func TestHandshakeAuthenticationAndOriginErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{{"api", httpx.ErrUnauthorized("missing session"), 401}, {"internal", errors.New("fixture database failure"), 500}} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHub(nil, rejectedCoverageAuth{tc.err}, nil, nil)
			rr := httptest.NewRecorder()
			h.HandleWebSocket(rr, httptest.NewRequest("GET", "http://example.test/api/ws", nil))
			if rr.Code != tc.status {
				t.Fatalf("status=%d want=%d", rr.Code, tc.status)
			}
			if strings.Contains(rr.Body.String(), "fixture database failure") {
				t.Fatal("raw authentication error exposed")
			}
		})
	}
	h := NewHub(nil, fakeAuth{user: auth.User{ID: uuid.New()}}, nil, nil)
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWebSocket))
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)
	for _, origin := range []string{"", "https://foreign.example"} {
		_, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": []string{origin}})
		if err == nil || response == nil || response.StatusCode != 403 {
			t.Fatalf("origin %q accepted: %v %v", origin, response, err)
		}
		response.Body.Close()
	}
}

func TestPendingRegistrationAndPromotionRequireLiveHandshake(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	c := newClient(h, nil, auth.User{ID: uuid.New()}, 0)
	fresh := c.User
	if h.registerVerified(c, &fresh) {
		t.Fatal("promoted client without a pending handshake")
	}
	h.Close()
	if h.registerPending(c) {
		t.Fatal("pending handshake accepted by closed hub")
	}
}

// websocketPair upgrades a real loopback socket while leaving pump ownership
// with the test, so transport failures can be exercised without fake connections.
func websocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err == nil {
			accepted <- conn
		}
	}))
	t.Cleanup(srv.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	server := <-accepted
	t.Cleanup(func() { server.Close() })
	return server, client
}

func TestWriterStopsOnTransportFailureAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "write-failure", true: "shutdown"}[shutdown], func(t *testing.T) {
			server, client := websocketPair(t)
			c := newClient(nil, server, auth.User{}, 0)
			done := make(chan struct{})
			if shutdown {
				c.shutdown()
			} else {
				server.Close()
				c.send <- []byte("message")
			}
			go func() { c.writePump(); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("writer did not stop")
			}
			if shutdown {
				client.SetReadDeadline(time.Now().Add(time.Second))
				_, _, err := client.ReadMessage()
				if err == nil {
					t.Fatal("shutdown did not close wire connection")
				}
			}
		})
	}
}

func TestReadPumpProcessesValidFramesAfterInvalidAndFloodedFrames(t *testing.T) {
	server, remote := websocketPair(t)
	h := NewHub(nil, nil, nil, nil)
	c := newClient(h, server, auth.User{ID: uuid.New()}, 0)
	h.register(c)
	drainTypes(t, c)
	done := make(chan struct{})
	go func() { c.readPump(); close(done) }()
	t.Cleanup(func() { remote.Close(); <-done })
	send := func(body []byte) {
		t.Helper()
		if err := remote.WriteMessage(websocket.TextMessage, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := remote.WriteControl(websocket.PongMessage, nil, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	send([]byte(`{`))
	send([]byte(`{"type":"presence_idle","payload":{"idle":true}}`))
	send([]byte(`{"type":"ping","payload":{"t":123}}`))
	deadline := time.After(2 * time.Second)
	for {
		select {
		case raw := <-c.send:
			var ev struct {
				Type    string `json:"type"`
				Payload struct {
					T float64 `json:"t"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(raw, &ev); err != nil {
				t.Fatal(err)
			}
			if ev.Type == "pong" {
				if ev.Payload.T != 123 {
					t.Fatal("ping timestamp changed")
				}
				goto flood
			}
		case <-deadline:
			t.Fatal("read loop did not answer ping")
		}
	}
flood:
	for range maxFramesPerSec + 20 {
		send([]byte(`{"type":"unknown","payload":{}}`))
	}
	send([]byte(`{"type":"webrtc_candidate","payload":{"candidate":"` + strings.Repeat("x", 2048) + `"}}`))
	remote.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("read pump did not unregister closed connection")
	}
	if h.OnlineCount() != 0 || !c.closed.Load() {
		t.Fatal("read pump retained disconnected user")
	}
}

func TestSubscriptionLimitsRejectAdditionalScreenAndKeepExistingShare(t *testing.T) {
	h, s := newSFUHub(t)
	ch := voiceCh()
	c := testClient(h, auth.User{ID: uuid.New()})
	h.joinVoice(c, ch)
	t.Cleanup(func() { h.CloseVoiceChannel(ch.ID); h.Close(); s.Close() })
	for range 64 {
		c.handleSubscribe([]byte(`{"kind":"screen","user_id":"` + uuid.NewString() + `","on":true}`))
	}
	extra := uuid.New()
	sharer := testClient(h, auth.User{ID: extra})
	h.joinVoice(sharer, ch)
	c.handleSubscribe([]byte(`{"kind":"screen","user_id":"` + extra.String() + `","on":true}`))
	if err := s.Room(ch.ID).PublishTestVideo(extra, sfu.SourceScreen); err != nil {
		t.Fatal(err)
	}
	for _, id := range s.Room(ch.ID).ScreenViewers()[extra] {
		if id == c.User.ID {
			t.Fatal("over-limit subscription accepted")
		}
	}
}

func TestEmptyScreenViewersAreAnArray(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	ch := voiceCh()
	c := testClient(h, auth.User{ID: uuid.New()})
	h.joinVoice(c, ch)
	drainTypes(t, c)
	sharer := uuid.New()
	h.announceScreenViewers(ch.ID, sharer, nil)
	expectViewers(t, c, ch.ID, sharer)
}

func TestGraceCanBeClaimedWithoutRemovingPresence(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	h.VoiceGrace = time.Hour
	t.Cleanup(h.Close)
	c := testClient(h, auth.User{ID: uuid.New()})
	ch := voiceCh()
	h.joinVoice(c, ch)
	h.dropVoiceForGrace(c)
	key := voiceKey{c.User.ID, ch.ID}
	if g := h.takeGrace(key); g == nil || g.channelID != ch.ID {
		t.Fatal("pending grace was not claimed")
	}
	if !inRoom(h, ch.ID, c.User.ID) || h.takeGrace(key) != nil {
		t.Fatal("claim removed presence or did not cancel timer")
	}
}

func TestVoiceJoinSurvivesUnavailableSFU(t *testing.T) {
	h, s := newSFUHub(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	c := testClient(h, auth.User{ID: uuid.New()})
	ch := voiceCh()
	h.joinVoice(c, ch)
	if c.currentVoice() == nil || *c.currentVoice() != ch.ID || c.peer() != nil || !inRoom(h, ch.ID, c.User.ID) {
		t.Fatal("failed SFU join corrupted voice presence")
	}
	h.leaveCurrentVoice(c)
	h.Close()
}

func TestServerInfoReportsRelease(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	h.Version = "1.2.3"
	c := testClient(h, auth.User{ID: uuid.New()})
	got := events(t, c, "server_info")
	if len(got) != 1 || got[0]["version"] != "1.2.3" {
		t.Fatalf("server version frame=%+v", got)
	}
}

// Close the hub at the HTTP-to-WebSocket ownership transition to exercise
// the race between the second shutdown check and pending registration.
type closeOnHijackWriter struct {
	http.ResponseWriter
	hub *Hub
}

func (w closeOnHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := w.ResponseWriter.(http.Hijacker).Hijack()
	if err == nil {
		w.hub.Close()
	}
	return conn, rw, err
}
func TestShutdownDuringUpgradeClosesUntrackedSocket(t *testing.T) {
	h := NewHub(nil, fakeAuth{user: auth.User{ID: uuid.New()}}, nil, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.HandleWebSocket(closeOnHijackWriter{w, h}, r) }))
	t.Cleanup(srv.Close)
	t.Cleanup(h.Close)
	h.Origins = []string{srv.URL}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, body, err := conn.ReadMessage(); err == nil {
		t.Fatalf("closed hub admitted socket: %s", body)
	} else if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
		t.Fatal("untracked socket did not close")
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.pending) != 0 || len(h.clients) != 0 {
		t.Fatal("shutdown race retained socket")
	}
}

func TestDisconnectUserStopsEverySessionAndPreservesOtherUsers(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	u := auth.User{ID: uuid.New()}
	first, second := testClient(h, u), testClient(h, u)
	other := testClient(h, auth.User{ID: uuid.New()})
	h.DisconnectUser(u.ID)
	for _, c := range []*Client{first, second} {
		select {
		case <-c.done:
		default:
			t.Fatal("target session remains active")
		}
		before := len(c.send)
		c.SendEvent("after-revocation", nil)
		if len(c.send) != before {
			t.Fatal("revoked session accepted private event")
		}
		h.unregister(c)
		h.unregister(c) // Repeated teardown must preserve the other user.
	}
	if other.closed.Load() || h.OnlineCount() != 1 {
		t.Fatal("disconnect affected another user's session")
	}
}

func TestReconnectWithinGraceResumesOriginalVoiceStay(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	h.VoiceGrace = time.Hour
	t.Cleanup(h.Close)
	u := auth.User{ID: uuid.New()}
	old := testClient(h, u)
	ch := voiceCh()
	h.joinVoice(old, ch)
	joined := h.voiceSnapshot()[ch.ID][u.ID].JoinedAt
	started := h.voiceRooms()["started"].(map[uuid.UUID]time.Time)[ch.ID]
	h.unregister(old)
	next := testClient(h, u)
	drainTypes(t, next)
	h.joinVoice(next, ch)
	if got := h.voiceSnapshot()[ch.ID][u.ID].JoinedAt; !got.Equal(joined) {
		t.Fatal("reconnect restarted participant duration")
	}
	if got := h.voiceRooms()["started"].(map[uuid.UUID]time.Time)[ch.ID]; !got.Equal(started) {
		t.Fatal("reconnect restarted room duration")
	}
	if count(drainTypes(t, next), "voice_state_update") != 0 {
		t.Fatal("reconnect announced a duplicate voice join")
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.grace) != 0 {
		t.Fatal("resumed participant retained grace timer")
	}
}
