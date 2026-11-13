package mnema

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"
)

func TestRunIdentifiesWithTokenIntentsAndPresenceOnTheHeaderAuthenticatedUpgrade(t *testing.T) {
	g := newFakeGateway(t, 60000)
	c := g.client(t, Options{Intents: IntentGuilds | IntentGuildMessages, Presence: &Presence{Status: StatusDND, Activity: &Activity{Name: "chess"}}})
	runClient(t, c)
	fc := g.next(t)
	if got := fc.header.Get("Authorization"); got != "Bot "+testToken {
		t.Fatalf("Authorization on the upgrade = %q", got)
	}
	f := fc.expect(t, opIdentify)
	var id struct {
		Token      string         `json:"token"`
		Intents    int64          `json:"intents"`
		Properties map[string]any `json:"properties"`
		Presence   presenceWire   `json:"presence"`
	}
	if err := json.Unmarshal(f.D, &id); err != nil {
		t.Fatal(err)
	}
	if id.Token != testToken || id.Intents != 513 || id.Properties["os"] == nil {
		t.Fatalf("identify = %+v", id)
	}
	if id.Presence.Status != "dnd" || len(id.Presence.Activities) != 1 || id.Presence.Activities[0].Name != "chess" {
		t.Fatalf("identify presence = %+v", id.Presence)
	}
}

func TestDispatchesAreDecodedAndDeliveredToTheirHandlers(t *testing.T) {
	g := newFakeGateway(t, 60000)
	c := g.client(t, Options{Intents: IntentGuildMessages})
	ready := make(chan *Ready, 1)
	created := make(chan *Message, 1)
	deleted := make(chan *MessageDelete, 1)
	reactions := make(chan *ReactionsUpdate, 1)
	interactions := make(chan *Interaction, 1)
	raw := make(chan string, 16)
	c.OnReady(func(_ context.Context, r *Ready) { ready <- r })
	c.OnMessageCreate(func(_ context.Context, m *Message) { created <- m })
	c.OnMessageDelete(func(_ context.Context, m *MessageDelete) { deleted <- m })
	c.OnReactionsUpdate(func(_ context.Context, r *ReactionsUpdate) { reactions <- r })
	c.OnInteraction(func(_ context.Context, i *Interaction) { interactions <- i })
	c.OnDispatch(func(_ context.Context, name string, _ json.RawMessage) { raw <- name })
	runClient(t, c)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s-1")
	if r := waitFor(t, ready, "READY"); r.SessionID != "s-1" || r.User.ID != "u-bot" || r.User.BotID != "b-1" || !r.User.Bot {
		t.Fatalf("ready = %+v", r)
	}
	fc.dispatch(t, 2, "MESSAGE_CREATE", map[string]any{"id": "m-1", "channel_id": "c-1", "content": "hi", "mentions": []string{"u-bot"}})
	if m := waitFor(t, created, "MESSAGE_CREATE"); m.ID != "m-1" || m.Content != "hi" || !m.MentionsUser("u-bot") {
		t.Fatalf("message = %+v", m)
	}
	fc.dispatch(t, 3, "MESSAGE_DELETE", map[string]any{"id": "m-1", "channel_id": "c-1"})
	if d := waitFor(t, deleted, "MESSAGE_DELETE"); d.ID != "m-1" || d.ChannelID != "c-1" {
		t.Fatalf("delete = %+v", d)
	}
	fc.dispatch(t, 4, "MESSAGE_REACTIONS_UPDATE", map[string]any{"message_id": "m-1", "channel_id": "c-1", "reactions": []map[string]any{{"emoji": "👍", "count": 2, "users": []string{"a", "b"}}}})
	if r := waitFor(t, reactions, "MESSAGE_REACTIONS_UPDATE"); r.Reactions[0].Count != 2 {
		t.Fatalf("reactions = %+v", r)
	}
	fc.dispatch(t, 5, "INTERACTION_CREATE", map[string]any{"id": "i-1", "token": "tok", "type": 2, "channel_id": "c-1",
		"data":   map[string]any{"id": "cmd", "name": "ping", "options": []map[string]any{{"name": "n", "type": 4, "value": 3}}},
		"member": map[string]any{"user": map[string]any{"id": "u-1", "username": "max"}}})
	i := waitFor(t, interactions, "INTERACTION_CREATE")
	if i.CommandName() != "ping" || i.Option("n").Int() != 3 || i.User.Username != "max" || i.rest != c.REST {
		t.Fatalf("interaction = %+v", i)
	}
	fc.dispatch(t, 6, "SOMETHING_NEW", map[string]any{})
	seen := map[string]bool{}
	for !seen["SOMETHING_NEW"] {
		seen[waitFor(t, raw, "raw dispatch")] = true
	}
}

func TestHeartbeatsCarryTheLastSequenceNumber(t *testing.T) {
	g := newFakeGateway(t, 40)
	c := g.client(t, Options{Intents: IntentGuilds})
	runClient(t, c)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s-1")
	fc.dispatch(t, 7, "CHANNELS_UPDATE", map[string]any{})
	for {
		f := fc.expect(t, opHeartbeat)
		fc.write(t, opHeartbeatACK, nil)
		if string(f.D) == "7" {
			return
		}
	}
}

func TestADroppedConnectionIsResumedWithTheSessionAndSequence(t *testing.T) {
	g := newFakeGateway(t, 60000)
	c := g.client(t, Options{Intents: IntentGuilds})
	resumed := make(chan struct{}, 1)
	c.OnResumed(func(context.Context) { resumed <- struct{}{} })
	runClient(t, c)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s-1")
	fc.dispatch(t, 41, "CHANNELS_UPDATE", map[string]any{})
	time.Sleep(20 * time.Millisecond)
	fc.closeWith(t, CloseSessionTimedOut, "")

	fc2 := g.next(t)
	f := fc2.expect(t, opResume)
	var r struct {
		Token     string `json:"token"`
		SessionID string `json:"session_id"`
		Seq       int64  `json:"seq"`
	}
	_ = json.Unmarshal(f.D, &r)
	if r.SessionID != "s-1" || r.Seq != 41 || r.Token != testToken {
		t.Fatalf("resume = %+v", r)
	}
	fc2.dispatch(t, 42, "RESUMED", map[string]any{})
	waitFor(t, resumed, "RESUMED")
}

func TestInvalidSessionIdentifiesAgainOnTheSameConnection(t *testing.T) {
	g := newFakeGateway(t, 60000)
	c := g.client(t, Options{Intents: IntentGuilds})
	runClient(t, c)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s-1")
	time.Sleep(20 * time.Millisecond)
	fc.closeWith(t, 1006, "")
	fc2 := g.next(t)
	fc2.expect(t, opResume)
	fc2.write(t, opInvalidSession, false)
	fc2.expect(t, opIdentify)
	if s := c.getSession(); s.id != "" {
		t.Fatalf("session after INVALID_SESSION = %+v", s)
	}
}

func TestFatalCloseCodesEndRunWithACloseError(t *testing.T) {
	for _, code := range []int{CloseAuthFailed, CloseInvalidIntents, CloseDisallowedIntents} {
		g := newFakeGateway(t, 60000)
		c := g.client(t, Options{Intents: IntentGuilds})
		stop, done := runClient(t, c)
		fc := g.next(t)
		fc.expect(t, opIdentify)
		fc.closeWith(t, code, "nope")
		waitFor(t, done, "Run to return")
		var ce *CloseError
		if err := stop(); !errors.As(err, &ce) || ce.Code != code || ce.Reason != "nope" {
			t.Fatalf("code %d: Run = %v", code, err)
		}
	}
}

func TestAMissedHeartbeatACKReconnectsAndResumes(t *testing.T) {
	g := newFakeGateway(t, 30)
	c := g.client(t, Options{Intents: IntentGuilds})
	runClient(t, c)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s-1")
	// No ACK: the client gives up on this connection.
	fc2 := g.next(t)
	fc2.expect(t, opResume)
}

func TestSessionTakenOverIdentifiesAfresh(t *testing.T) {
	g := newFakeGateway(t, 60000)
	c := g.client(t, Options{Intents: IntentGuilds})
	runClient(t, c)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s-1")
	time.Sleep(20 * time.Millisecond)
	fc.closeWith(t, CloseSessionTakenOver, "")
	g.next(t).expect(t, opIdentify)
}

func TestRateLimitedCloseBacksOffLonger(t *testing.T) {
	g := newFakeGateway(t, 60000)
	c := g.client(t, Options{Intents: IntentGuilds})
	runClient(t, c)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	start := time.Now()
	fc.closeWith(t, CloseRateLimited, "")
	g.next(t)
	if waited := time.Since(start); waited < c.rateLimitedBackoff {
		t.Fatalf("reconnected after %v, want at least %v", waited, c.rateLimitedBackoff)
	}
}

func TestBackoffGrowsWithJitterAndIsCapped(t *testing.T) {
	c := &Client{minBackoff: time.Second, maxBackoff: 60 * time.Second, rateLimitedBackoff: 30 * time.Second}
	lo, hi := func() float64 { return 0 }, func() float64 { return 1 }
	cases := []struct {
		attempt, code int
		random        func() float64
		want          time.Duration
	}{
		{0, 1006, lo, 500 * time.Millisecond},
		{0, 1006, hi, time.Second},
		{3, 1006, hi, 8 * time.Second},
		{10, 1006, hi, 60 * time.Second},
		{100, 1006, lo, 30 * time.Second},
		{0, CloseRateLimited, lo, 30 * time.Second},
	}
	for _, tc := range cases {
		if got := c.backoff(tc.attempt, tc.code, tc.random); got != tc.want {
			t.Errorf("backoff(%d, %d) = %v, want %v", tc.attempt, tc.code, got, tc.want)
		}
	}
}

func TestAPanickingHandlerDoesNotStopTheClient(t *testing.T) {
	g := newFakeGateway(t, 60000)
	c := g.client(t, Options{Intents: IntentGuildMessages})
	got := make(chan string, 2)
	c.OnMessageCreate(func(_ context.Context, m *Message) {
		if m.Content == "boom" {
			panic("handler bug")
		}
		got <- m.Content
	})
	runClient(t, c)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s")
	fc.dispatch(t, 2, "MESSAGE_CREATE", map[string]any{"id": "1", "content": "boom"})
	fc.dispatch(t, 3, "MESSAGE_CREATE", map[string]any{"id": "2", "content": "ok"})
	if s := waitFor(t, got, "the second message"); s != "ok" {
		t.Fatalf("got %q", s)
	}
}

func TestASlowHandlerDoesNotStallHeartbeats(t *testing.T) {
	g := newFakeGateway(t, 30)
	c := g.client(t, Options{Intents: IntentGuildMessages})
	release := make(chan struct{})
	c.OnMessageCreate(func(ctx context.Context, _ *Message) {
		select {
		case <-release:
		case <-ctx.Done():
		}
	})
	runClient(t, c)
	defer close(release)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s")
	fc.dispatch(t, 2, "MESSAGE_CREATE", map[string]any{"id": "1"})
	for range 3 {
		fc.expect(t, opHeartbeat)
		fc.write(t, opHeartbeatACK, nil)
	}
}

func TestSetPresenceSendsOpThreeOnTheLiveConnection(t *testing.T) {
	g := newFakeGateway(t, 60000)
	c := g.client(t, Options{Intents: IntentGuilds})
	ready := make(chan struct{}, 1)
	c.OnReady(func(context.Context, *Ready) { ready <- struct{}{} })
	runClient(t, c)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s")
	waitFor(t, ready, "READY")
	if err := c.SetPresence(context.Background(), Presence{Status: StatusIdle, Activity: &Activity{Name: "tests", Type: ActivityWatching}}); err != nil {
		t.Fatal(err)
	}
	var p presenceWire
	_ = json.Unmarshal(fc.expect(t, opPresenceUpdate).D, &p)
	if p.Status != "idle" || p.Activities[0].Type != ActivityWatching {
		t.Fatalf("presence = %+v", p)
	}
	if err := c.SetPresence(context.Background(), Presence{Status: "away"}); err == nil {
		t.Fatal("an unknown status was accepted")
	}
	if err := c.SetPresence(context.Background(), Presence{Status: StatusOnline, Activity: &Activity{Name: "x", Type: 1}}); err == nil {
		t.Fatal("a streaming activity was accepted")
	}
}

func TestOutgoingFramesAboveTheLimitAreRefused(t *testing.T) {
	cn := &conn{out: make(chan []byte, 1), done: make(chan struct{})}
	big := make([]byte, maxFrameBytes)
	for i := range big {
		big[i] = 'a'
	}
	if err := cn.send(context.Background(), opPresenceUpdate, string(big)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("send = %v", err)
	}
}

func TestRunReturnsOnCancelWithoutLeakingGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	func() {
		g := newFakeGateway(t, 20)
		c := g.client(t, Options{Intents: IntentGuildMessages})
		c.OnMessageCreate(func(ctx context.Context, _ *Message) { <-ctx.Done() })
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- c.Run(ctx) }()
		fc := g.next(t)
		fc.expect(t, opIdentify)
		fc.ready(t, "s")
		fc.dispatch(t, 2, "MESSAGE_CREATE", map[string]any{"id": "1"})
		time.Sleep(50 * time.Millisecond)
		cancel()
		if err := waitFor(t, done, "Run"); err != nil {
			t.Fatalf("Run after cancel = %v", err)
		}
		_ = fc.ws.Close()
		g.srv.CloseClientConnections()
		g.srv.Close()
		g.transport.CloseIdleConnections()
	}()
	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		buf := make([]byte, 1<<16)
		t.Fatalf("%d goroutines before, %d after:\n%s", before, n, buf[:runtime.Stack(buf, true)])
	}
}

func TestRunRefusesASecondConcurrentRun(t *testing.T) {
	g := newFakeGateway(t, 60000)
	c := g.client(t, Options{Intents: IntentGuilds})
	runClient(t, c)
	g.next(t).expect(t, opIdentify)
	if err := c.Run(context.Background()); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Run = %v", err)
	}
}

func TestAnUnauthorizedGatewayLookupEndsRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "401: Unauthorized", "code": 0})
	}))
	defer srv.Close()
	c, err := New(Options{BaseURL: srv.URL, Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	var re *RESTError
	if err := c.Run(context.Background()); !errors.As(err, &re) || re.Status != 401 {
		t.Fatalf("Run = %v", err)
	}
}

func TestSaturatedHandlersDoNotHoldUpHeartbeatACKs(t *testing.T) {
	g := newFakeGateway(t, 30)
	c := g.client(t, Options{Intents: IntentGuildMessages, HandlerConcurrency: 1})
	release := make(chan struct{})
	c.OnMessageCreate(func(ctx context.Context, _ *Message) {
		select {
		case <-release:
		case <-ctx.Done():
		}
	})
	runClient(t, c)
	defer close(release)
	fc := g.next(t)
	fc.expect(t, opIdentify)
	fc.ready(t, "s")
	for n := range 3 {
		fc.dispatch(t, int64(n+2), "MESSAGE_CREATE", map[string]any{"id": "1"})
	}
	for range 5 {
		fc.expect(t, opHeartbeat)
		fc.write(t, opHeartbeatACK, nil)
	}
	select {
	case <-g.conns:
		t.Fatal("the client reconnected although every heartbeat was acknowledged")
	default:
	}
}
