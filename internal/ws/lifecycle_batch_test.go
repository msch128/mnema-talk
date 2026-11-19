package ws

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/mnema-xyz/talk/internal/auth"
)

// recordingConn is a connection that keeps every write it is given.
type recordingConn struct {
	net.Conn
	mu     sync.Mutex
	writes [][]byte
}

func (r *recordingConn) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes = append(r.writes, bytes.Clone(p))
	if r.Conn != nil {
		return r.Conn.Write(p)
	}
	return len(p), nil
}

func (r *recordingConn) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.writes)
}

func TestBatchConnWritesThroughOutsideABatch(t *testing.T) {
	rec := &recordingConn{}
	b := &batchConn{Conn: rec}
	for _, p := range []string{"a", "b"} {
		if _, err := b.Write([]byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	if rec.count() != 2 {
		t.Fatalf("%d writes, want 2", rec.count())
	}
}

func TestBatchConnSendsABatchWithOneWriteInOrder(t *testing.T) {
	rec := &recordingConn{}
	b := &batchConn{Conn: rec}
	b.hold()
	for _, p := range []string{"one ", "two ", "three"} {
		if _, err := b.Write([]byte(p)); err != nil {
			t.Fatal(err)
		}
	}
	if rec.count() != 0 {
		t.Fatal("a held batch reached the network before flush")
	}
	if err := b.flush(); err != nil {
		t.Fatal(err)
	}
	if rec.count() != 1 || string(rec.writes[0]) != "one two three" {
		t.Fatalf("writes %q, want one write of everything in order", rec.writes)
	}
	// After the batch, writes go straight through again.
	_, _ = b.Write([]byte("x"))
	if rec.count() != 2 {
		t.Fatalf("%d writes after the batch, want 2", rec.count())
	}
}

// A batch never grows past batchBytes, and a burst's buffer is not kept.
func TestBatchConnSendsEarlyWhenTheBatchIsFull(t *testing.T) {
	rec := &recordingConn{}
	b := &batchConn{Conn: rec}
	b.hold()
	big := bytes.Repeat([]byte("x"), batchBytes-10)
	_, _ = b.Write(big)
	_, _ = b.Write([]byte("0123456789abc")) // would pass the cap: the gathered bytes go first
	if rec.count() != 2 || len(rec.writes[0]) != len(big) || string(rec.writes[1]) != "0123456789abc" {
		t.Fatalf("got %d writes, want the gathered batch and then the frame", rec.count())
	}
	if err := b.flush(); err != nil {
		t.Fatal(err)
	}
	if b.buf != nil {
		t.Fatalf("kept a %d byte buffer after the burst", cap(b.buf))
	}
}

// Frames that queue up for a connection (every member coming online after a
// restart is one presence frame to every connection) leave in a few writes,
// and the client still receives every frame, in order.
func TestQueuedFramesLeaveInFewWritesAndArriveInOrder(t *testing.T) {
	const frames = 200
	h := NewHub(nil, fakeAuth{user: auth.User{ID: uuid.New(), Username: "viewer"}}, nil, nil, openPerms())
	t.Cleanup(h.Close)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(h.HandleWebSocket))
	accepted := make(chan *recordingConn, 1)
	srv.Listener = recordingListener{Listener: srv.Listener, accepted: accepted}
	srv.Start()
	t.Cleanup(srv.Close)
	h.Origins = []string{srv.URL}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Origin": []string{srv.URL}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	rec := <-accepted
	greeting := readUntilType(t, conn, "voice_rooms")
	theBatchConn(t, h)

	// Stall the writer on the network while the frames queue up behind it.
	rec.mu.Lock()
	before := len(rec.writes)
	for i := range frames {
		h.Broadcast(PresenceUpdate{UserID: uuid.New(), Status: []string{"online", "away"}[i%2]})
	}
	rec.mu.Unlock()

	seen := 0
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for seen < frames {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("after %d of %d frames (greeting %d): %v", seen, frames, greeting, err)
		}
		var ev struct {
			Type    string `json:"type"`
			Payload struct {
				Status string `json:"status"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type != "presence_update" {
			continue
		}
		if want := []string{"online", "away"}[seen%2]; ev.Payload.Status != want {
			t.Fatalf("frame %d: status %q, want %q (out of order)", seen, ev.Payload.Status, want)
		}
		seen++
	}
	// One write per frame would be 200; the writer took them in batches.
	if n := rec.count() - before; n > frames/maxBatchFrames+3 {
		t.Fatalf("%d network writes for %d queued frames", n, frames)
	}
}

// readUntilType reads frames until one of type want arrived and returns how many it read.
func readUntilType(t *testing.T, conn *websocket.Conn, want string) int {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for n := 1; ; n++ {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("no %s: %v", want, err)
		}
		var ev Event
		if json.Unmarshal(raw, &ev) == nil && ev.Type == want {
			return n
		}
	}
}

// recordingListener records the writes of every connection it accepts.
type recordingListener struct {
	net.Listener
	accepted chan *recordingConn
}

func (l recordingListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	rec := &recordingConn{Conn: c}
	select {
	case l.accepted <- rec:
	default:
	}
	return rec, nil
}

func theBatchConn(t *testing.T, h *Hub) *batchConn {
	t.Helper()
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if b, ok := c.conn.NetConn().(*batchConn); ok {
			return b
		}
		t.Fatalf("the connection is a %T, not a batchConn", c.conn.NetConn())
	}
	t.Fatal("no client")
	return nil
}

// A close frame another goroutine writes while the write loop holds a batch
// (evict's 4001, renewLostPeer's 4002) still reaches the client when the
// connection is closed right after: otherwise it sees 1006 and reconnects.
func TestBatchConnCloseSendsTheHeldBatch(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			got <- nil
			return
		}
		defer c.Close()
		var all bytes.Buffer
		_, _ = all.ReadFrom(c)
		got <- all.Bytes()
	}()
	raw, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	b := &batchConn{Conn: raw}
	b.hold()
	frame := websocket.FormatCloseMessage(closeReplaced, "too many connections")
	_, _ = b.Write(frame)
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-got:
		if !bytes.Equal(data, frame) {
			t.Fatalf("client got %q, want the close frame %q", data, frame)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing arrived")
	}
}

// Close does not hang on a write that is stuck on the network.
func TestBatchConnCloseCutsOffAStuckWrite(t *testing.T) {
	server, client := net.Pipe() // nobody reads client: a write blocks
	t.Cleanup(func() { _ = client.Close() })
	b := &batchConn{Conn: server}
	go func() { _, _ = b.Write([]byte("stuck")) }()
	time.Sleep(20 * time.Millisecond)
	done := make(chan struct{})
	go func() { _ = b.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Close waited on a stuck write")
	}
}

// The hub closes clients one after another (shutdown, sign-out, kicks): a
// writer that sets gorilla's writeWait deadline while Close runs must not
// stretch Close's own short wait to ten seconds.
func TestBatchConnCloseKeepsItsDeadlineAgainstALaterOne(t *testing.T) {
	server, client := net.Pipe() // nobody reads client: a write blocks
	t.Cleanup(func() { _ = client.Close() })
	b := &batchConn{Conn: server}
	go func() { _, _ = b.Write([]byte("stuck")) }()
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				_ = b.SetWriteDeadline(time.Now().Add(10 * time.Second))
				time.Sleep(time.Millisecond)
			}
		}
	}()
	time.Sleep(20 * time.Millisecond)
	closed := make(chan struct{})
	go func() { _ = b.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(closeFlushWait + 250*time.Millisecond):
		t.Fatalf("Close still waiting after %v with a concurrent 10 s deadline", closeFlushWait+250*time.Millisecond)
	}
}
