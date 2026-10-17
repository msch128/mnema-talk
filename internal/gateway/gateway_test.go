package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestFrameLimiter(t *testing.T) {
	var l frameLimiter
	now := time.Now()
	for i := 0; i < framesPerMinute; i++ {
		if !l.allow(now) {
			t.Fatalf("frame %d refused", i)
		}
	}
	if l.allow(now) {
		t.Fatal("over the allowance")
	}
	if !l.allow(now.Add(time.Minute + time.Second)) {
		t.Fatal("a new minute starts over")
	}
}

func TestWebSocketURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://talk.example.org/": "wss://talk.example.org/api/bot/v1/gateway",
		"http://localhost:8080":     "ws://localhost:8080/api/bot/v1/gateway",
		"talk.example.org":          "talk.example.org/api/bot/v1/gateway",
	} {
		if got := (&Gateway{PublicURL: in}).wsURL(); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

func dataOf(t *testing.T, s *session) []int64 {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	var seqs []int64
	for _, e := range s.ring {
		seqs = append(seqs, e.seq)
	}
	return seqs
}

func TestSessionKeepsTheLastEventsForResume(t *testing.T) {
	s := &session{id: "s"}
	for i := 0; i < bufferSize+88; i++ {
		s.enqueue("X", json.RawMessage(`{}`))
	}
	seqs := dataOf(t, s)
	if len(seqs) != bufferSize || seqs[0] != 89 || seqs[len(seqs)-1] != bufferSize+88 {
		t.Fatalf("ring: %d entries from %d to %d", len(seqs), seqs[0], seqs[len(seqs)-1])
	}
	c := newConnection(nil)
	if _, ok := s.resumeOn(c, 10); ok {
		t.Fatal("events before the buffer cannot be replayed")
	}
	if _, ok := s.resumeOn(c, s.seq+1); ok {
		t.Fatal("a sequence ahead of the session")
	}
	empty := &session{}
	empty.seq = 5
	if _, ok := empty.resumeOn(c, 2); ok {
		t.Fatal("nothing buffered but events missed")
	}
	if _, ok := empty.resumeOn(c, 5); !ok {
		t.Fatal("nothing missed resumes")
	}
	c = newConnection(nil)
	if _, ok := s.resumeOn(c, s.seq-3); !ok || len(c.send) != 4 {
		t.Fatalf("replay: %d frames queued", len(c.send))
	}
}

func TestConnectionNeverBlocksThePublisher(t *testing.T) {
	c := newConnection(nil)
	for i := 0; i < sendQueue; i++ {
		c.trySend([]byte("x"))
	}
	if c.closed() {
		t.Fatal("closed before the queue is full")
	}
	c.trySend([]byte("one too many"))
	if !c.closed() || c.closeCode != websocket.CloseTryAgainLater {
		t.Fatalf("a full queue closes the connection: %d", c.closeCode)
	}
	c.trySend([]byte("after close")) // ignored
	c.closeWith(1000, "again")       // the first code stays
	if c.closeCode != websocket.CloseTryAgainLater {
		t.Fatal("the first close wins")
	}
}

func TestRegisterLimitsAndEviction(t *testing.T) {
	g := New(Config{})
	bot1 := uuid.New()
	mk := func(live bool, age time.Duration) *session {
		s := &session{id: newSessionID(), botID: bot1, detachedAt: time.Now().Add(-age)}
		if live {
			s.conn = newConnection(nil)
		}
		return s
	}
	var oldest *session
	for i := 0; i < maxSessionsPerBot; i++ {
		s := mk(false, time.Duration(i+1)*time.Minute)
		if i == maxSessionsPerBot-1 {
			oldest = s
		}
		if !g.register(s) {
			t.Fatal("register")
		}
	}
	if !g.register(mk(false, 0)) {
		t.Fatal("a detached session makes room")
	}
	g.mu.Lock()
	_, kept := g.sessions[oldest.id]
	n := len(g.byBot[bot1])
	g.mu.Unlock()
	if kept || n != maxSessionsPerBot {
		t.Fatalf("the oldest detached session goes: kept=%v n=%d", kept, n)
	}
	// Live connections are capped.
	g2 := New(Config{})
	b2 := uuid.New()
	for i := 0; i < maxConnsPerBot; i++ {
		if !g2.register(&session{id: newSessionID(), botID: b2, conn: newConnection(nil)}) {
			t.Fatal("register live")
		}
	}
	if g2.register(&session{id: newSessionID(), botID: b2}) {
		t.Fatal("over the connection cap")
	}
	g2.Close()
	g2.Close()
	if g2.register(&session{id: newSessionID(), botID: uuid.New()}) {
		t.Fatal("a closed gateway takes no sessions")
	}
}

func TestExpireDropsStaleSessions(t *testing.T) {
	g := New(Config{})
	g.SetTimings(Timings{Heartbeat: time.Second, Identify: time.Second, Resume: time.Minute, TokenCheck: time.Hour})
	b := uuid.New()
	stale := &session{id: "stale", botID: b, detachedAt: time.Now().Add(-time.Hour)}
	fresh := &session{id: "fresh", botID: b, detachedAt: time.Now()}
	live := &session{id: "live", botID: b, conn: newConnection(nil)}
	for _, s := range []*session{stale, fresh, live} {
		g.register(s)
	}
	g.expire()
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sessions["stale"] != nil || g.sessions["fresh"] == nil || g.sessions["live"] == nil {
		t.Fatalf("sessions: %v", g.sessions)
	}
}

// Events are not queued when the permission state cannot be read, nor for a
// payload that cannot be encoded.
func TestObserveFailsClosed(t *testing.T) {
	user := uuid.New()
	ch := uuid.New()
	newGateway := func(p *perm.Service) (*Gateway, *session) {
		g := New(Config{Perms: p})
		s := &session{id: "s", botID: uuid.New(), userID: user, intents: bot.AllIntents}
		g.register(s)
		return g, s
	}

	g, s := newGateway(perm.NewService(nil)) // no state to read
	g.Observe(ch, "message_create", map[string]any{"id": 1})
	g.Observe(uuid.Nil, "member_joined", map[string]any{"id": uuid.New()})
	if s.seq != 0 {
		t.Fatal("delivered without a permission state")
	}

	f := perm.NewFixture().Bot(user)
	channel := f.Channel(uuid.Nil)
	role := f.Role(1, perm.SendMessages)
	f.Assign(user, role)
	f.RoleOverwrite(true, channel, role, perm.ViewChannel, 0)
	g, s = newGateway(f.Service())
	g.Observe(channel, "message_create", make(chan int)) // cannot be encoded
	g.Observe(uuid.Nil, "member_joined", make(chan int))
	g.Observe(uuid.Nil, "member_joined", map[string]any{"username": "no id"})
	g.Observe(uuid.Nil, "typing", map[string]any{})
	g.Observe(channel, "unknown_event", map[string]any{})
	if s.seq != 0 {
		t.Fatalf("delivered %d events", s.seq)
	}
	g.Observe(channel, "message_create", map[string]any{"id": 1})
	if s.seq != 1 {
		t.Fatal("a viewable channel's event is delivered")
	}
}

func TestDeliverInteractionNeedsALiveSession(t *testing.T) {
	g := New(Config{})
	b := uuid.New()
	if g.DeliverInteraction(b, json.RawMessage(`{}`)) {
		t.Fatal("no session")
	}
	detached := &session{id: "d", botID: b}
	g.register(detached)
	if g.DeliverInteraction(b, json.RawMessage(`{}`)) {
		t.Fatal("a detached session is not live")
	}
	older := &session{id: "o", botID: b, conn: newConnection(nil), startedAt: time.Now().Add(-time.Hour)}
	newer := &session{id: "n", botID: b, conn: newConnection(nil), startedAt: time.Now()}
	g.register(older)
	g.register(newer)
	if !g.DeliverInteraction(b, json.RawMessage(`{}`)) || newer.seq != 1 || older.seq != 0 {
		t.Fatalf("the newest live session gets it: %d %d", newer.seq, older.seq)
	}
}

// socketPair gives a server-side connection (with its writer running) and
// the client's socket.
func socketPair(t *testing.T) (*connection, *websocket.Conn) {
	t.Helper()
	got := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err == nil {
			got <- ws
		}
	}))
	t.Cleanup(srv.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	c := newConnection(<-got)
	go c.writeLoop()
	return c, client
}

func TestWriteLoopFlushesBeforeClosing(t *testing.T) {
	c, client := socketPair(t)
	c.trySend([]byte("one"))
	c.trySend([]byte("two"))
	c.closeWith(CloseAuthFailed, "bye")
	var got []string
	for {
		_, msg, err := client.ReadMessage()
		if err != nil {
			ce, ok := err.(*websocket.CloseError)
			if !ok || ce.Code != CloseAuthFailed || ce.Text != "bye" {
				t.Fatalf("close: %v", err)
			}
			break
		}
		got = append(got, string(msg))
	}
	if strings.Join(got, ",") != "one,two" {
		t.Fatalf("frames before the close: %v", got)
	}
}

func TestWriteLoopEndsWhenTheSocketIsGone(t *testing.T) {
	c, client := socketPair(t)
	client.Close()
	deadline := time.Now().Add(2 * time.Second)
	for !c.closed() && time.Now().Before(deadline) {
		c.trySend([]byte("anyone there?"))
		time.Sleep(10 * time.Millisecond)
	}
	if !c.closed() {
		t.Fatal("the connection never noticed")
	}
	// Frames queued during the close that cannot be written end the flush too.
	c2, client2 := socketPair(t)
	client2.Close()
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 5; i++ {
		c2.send <- []byte("late")
	}
	c2.closeWith(1000, "")
	<-c2.done
}
