package mnema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const writeTimeout = 10 * time.Second

// frame is a gateway message; S and T are only set on dispatches.
type frame struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d"`
	S  *int64          `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

// conn is one gateway connection. The read loop runs in the goroutine of
// run; a writer goroutine owns all writes (fed by out, within 120 frames a
// minute), and a heartbeat goroutine sends heartbeats. All of them end when
// done is closed.
type conn struct {
	c    *Client
	ws   *websocket.Conn
	out  chan []byte
	done chan struct{}
	wg   sync.WaitGroup

	once        sync.Once
	acked       atomic.Bool
	healthy     atomic.Bool
	abortCode   atomic.Int64
	closeReason string
}

func newConn(c *Client, ws *websocket.Conn) *conn {
	cn := &conn{c: c, ws: ws, out: make(chan []byte, 16), done: make(chan struct{})}
	cn.acked.Store(true)
	return cn
}

// stop ends the connection: it sends a close frame with code and closes the
// socket, which ends the read loop.
func (cn *conn) stop(code int, reason string) {
	cn.once.Do(func() {
		msg := websocket.FormatCloseMessage(code, reason)
		_ = cn.ws.WriteControl(websocket.CloseMessage, msg, time.Now().Add(time.Second))
		close(cn.done)
		_ = cn.ws.Close()
	})
}

// abort ends the connection for a reason of the client's own (missed ACK,
// RECONNECT); the code becomes the outcome's.
func (cn *conn) abort(code int, reason string) {
	cn.abortCode.CompareAndSwap(0, int64(code))
	cn.stop(websocket.CloseServiceRestart, reason)
}

func (cn *conn) run(ctx context.Context, d *dispatcher) outcome {
	defer cn.wg.Wait()
	defer cn.stop(websocket.CloseNormalClosure, "")
	interval, err := cn.readHello()
	if err != nil {
		return cn.outcome(err)
	}
	cn.wg.Add(3)
	go cn.writer()
	go cn.heartbeat(interval)
	go cn.watch(ctx)
	if err := cn.greet(ctx); err != nil {
		return cn.outcome(err)
	}
	cn.c.mu.Lock()
	cn.c.conn = cn
	cn.c.mu.Unlock()
	defer func() {
		cn.c.mu.Lock()
		if cn.c.conn == cn {
			cn.c.conn = nil
		}
		cn.c.mu.Unlock()
	}()
	for {
		var f frame
		if err := cn.ws.ReadJSON(&f); err != nil {
			return cn.outcome(err)
		}
		if err := cn.handle(ctx, f, d); err != nil {
			return cn.outcome(err)
		}
	}
}

func (cn *conn) outcome(err error) outcome {
	// A connection the client closed because its server flooded it is no
	// healthy one: the backoff grows instead of starting over.
	out := outcome{err: err, healthy: cn.healthy.Load() && !errors.Is(err, errUnshedOverflow)}
	var ce *websocket.CloseError
	switch {
	case cn.abortCode.Load() != 0:
		out.code = int(cn.abortCode.Load())
	case errors.As(err, &ce):
		out.code = ce.Code
		cn.closeReason = ce.Text
	}
	return out
}

func (cn *conn) readHello() (time.Duration, error) {
	_ = cn.ws.SetReadDeadline(time.Now().Add(cn.c.helloTimeout))
	var f frame
	if err := cn.ws.ReadJSON(&f); err != nil {
		return 0, err
	}
	_ = cn.ws.SetReadDeadline(time.Time{})
	var h struct {
		HeartbeatInterval int `json:"heartbeat_interval"`
	}
	if f.Op != opHello || json.Unmarshal(f.D, &h) != nil || h.HeartbeatInterval <= 0 {
		return 0, fmt.Errorf("mnema: expected HELLO, got op %d", f.Op)
	}
	return time.Duration(h.HeartbeatInterval) * time.Millisecond, nil
}

// greet resumes the session when there is one, and identifies otherwise.
func (cn *conn) greet(ctx context.Context) error {
	s := cn.c.getSession()
	if s.id == "" {
		return cn.identify(ctx)
	}
	return cn.send(ctx, opResume, map[string]any{"token": cn.c.opts.Token, "session_id": s.id, "seq": s.seq})
}

func (cn *conn) identify(ctx context.Context) error {
	d := map[string]any{
		"token":      cn.c.opts.Token,
		"intents":    int64(cn.c.opts.Intents),
		"properties": map[string]string{"os": runtime.GOOS, "browser": "mnema-talk-sdk-go", "device": "mnema-talk-sdk-go"},
	}
	if p := cn.c.currentPresence(); p != nil {
		d["presence"] = p.wire()
	}
	return cn.send(ctx, opIdentify, d)
}

func (cn *conn) handle(ctx context.Context, f frame, d *dispatcher) error {
	switch f.Op {
	case opDispatch:
		return cn.handleDispatch(ctx, f, d)
	case opHeartbeat:
		return cn.sendHeartbeat()
	case opHeartbeatACK:
		cn.acked.Store(true)
	case opReconnect:
		cn.abort(opReconnectCode, "reconnect requested")
	case opInvalidSession:
		cn.c.logger.Info("mnema: session cannot be resumed, identifying")
		cn.c.updateSession(func(s *session) { *s = session{} })
		return cn.identify(ctx)
	}
	return nil
}

// opReconnectCode is the outcome of a RECONNECT frame: resume elsewhere.
const opReconnectCode = 4901

func (cn *conn) handleDispatch(ctx context.Context, f frame, d *dispatcher) error {
	switch f.T {
	case "READY":
		var r Ready
		if err := json.Unmarshal(f.D, &r); err != nil {
			return fmt.Errorf("mnema: undecodable READY: %w", err)
		}
		cn.c.updateSession(func(s *session) { s.id, s.resumeURL = r.SessionID, r.ResumeGatewayURL })
		cn.healthy.Store(true)
	case "RESUMED":
		cn.healthy.Store(true)
		if p := cn.c.currentPresence(); p != nil {
			// The status ends with the bot's last connection, so it is set again.
			if err := cn.send(ctx, opPresenceUpdate, p.wire()); err != nil {
				return err
			}
		}
	}
	// The sequence number is kept only for an event that was handed on: one
	// the dispatcher could not take ends the connection, and the resume asks
	// for it again.
	if err := d.dispatch(f.T, f.D); err != nil {
		return err
	}
	if f.S != nil {
		cn.c.updateSession(func(s *session) { s.seq = *f.S })
	}
	return nil
}

// send queues a frame for the writer.
func (cn *conn) send(ctx context.Context, op int, d any) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("mnema: encode frame: %w", err)
	}
	b, err := json.Marshal(frame{Op: op, D: raw})
	if err != nil {
		return fmt.Errorf("mnema: encode frame: %w", err)
	}
	if len(b) > maxFrameBytes {
		return ErrFrameTooLarge
	}
	select {
	case cn.out <- b:
		return nil
	case <-cn.done:
		return errors.New("mnema: the gateway connection is closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (cn *conn) sendHeartbeat() error {
	var seq any
	if s := cn.c.getSession(); s.seq > 0 {
		seq = s.seq
	}
	return cn.send(context.Background(), opHeartbeat, seq)
}

func (cn *conn) writer() {
	defer cn.wg.Done()
	limit := window{limit: framesPerMinute, every: time.Minute}
	for {
		var b []byte
		select {
		case <-cn.done:
			return
		case b = <-cn.out:
		}
		for d := limit.delay(time.Now()); d > 0; d = limit.delay(time.Now()) {
			select {
			case <-cn.done:
				return
			case <-time.After(d):
			}
		}
		limit.take(time.Now())
		_ = cn.ws.SetWriteDeadline(time.Now().Add(writeTimeout))
		if err := cn.ws.WriteMessage(websocket.TextMessage, b); err != nil {
			cn.stop(websocket.CloseAbnormalClosure, "")
			return
		}
	}
}

// heartbeat sends HEARTBEAT every interval, the first after a random part of
// it; a heartbeat whose ACK has not come by the next one ends the connection
// so that it is resumed on a new one.
func (cn *conn) heartbeat(interval time.Duration) {
	defer cn.wg.Done()
	t := time.NewTimer(time.Duration(rand.Int64N(int64(interval))))
	defer t.Stop()
	for {
		select {
		case <-cn.done:
			return
		case <-t.C:
		}
		if !cn.acked.Swap(false) {
			cn.c.logger.Warn("mnema: no heartbeat ACK, reconnecting")
			cn.abort(closeHeartbeatNotAcked, "no heartbeat ack")
			return
		}
		_ = cn.sendHeartbeat()
		t.Reset(interval)
	}
}

// watch closes the connection when ctx ends.
func (cn *conn) watch(ctx context.Context) {
	defer cn.wg.Done()
	select {
	case <-ctx.Done():
		cn.stop(websocket.CloseNormalClosure, "")
	case <-cn.done:
	}
}
