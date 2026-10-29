package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

// session is one user's WebSocket, reading everything the server sends.
type session struct {
	idx int
	tok lt.Token
	r   *run

	everUp atomic.Bool
	wmu    sync.Mutex
	conn   *websocket.Conn
}

// xff is a stable fake client address for SPOOF_XFF: one per session, in a
// range that is never routed.
func (s *session) xff() string {
	return fmt.Sprintf("10.%d.%d.%d", (s.idx>>16)&255, (s.idx>>8)&255, s.idx&255)
}

func (s *session) headers() http.Header {
	base := s.r.cfg.base
	h := http.Header{}
	h.Set("Origin", base.Scheme+"://"+base.Host)
	h.Set("Cookie", s.r.cfg.cookie+"="+s.tok.Token)
	if s.r.cfg.spoofXFF {
		h.Set("X-Forwarded-For", s.xff())
	}
	return h
}

func (s *session) dial(ctx context.Context) (*websocket.Conn, error) {
	base := s.r.cfg.base
	scheme := map[string]string{"http": "ws", "https": "wss"}[base.Scheme]
	d := websocket.Dialer{HandshakeTimeout: 20 * time.Second, ReadBufferSize: 16 << 10, WriteBufferSize: 4 << 10}
	conn, resp, err := d.DialContext(ctx, scheme+"://"+base.Host+"/api/ws", s.headers())
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dial: %s", resp.Status)
		}
		return nil, fmt.Errorf("dial: %w", err)
	}
	conn.SetReadLimit(4 << 20)
	return conn, nil
}

// loop keeps the session connected until ctx ends: it connects, reads, and
// after an unexpected end connects again, paced like the first connection and
// backing off.
func (s *session) loop(ctx context.Context, pace *pacer) {
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		if err := pace.wait(ctx); err != nil {
			return
		}
		began := time.Now()
		conn, err := s.dial(ctx)
		if err != nil {
			s.r.wsErrors.Add(1)
			backoff = sleepBackoff(ctx, backoff)
			continue
		}
		backoff = 2 * time.Second
		s.serve(ctx, conn, began)
		if ctx.Err() == nil {
			s.r.reconnects.Add(1)
		}
	}
}

func sleepBackoff(ctx context.Context, d time.Duration) time.Duration {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
	return min(d*2, 30*time.Second)
}

// serve reads the connection until it ends. The first frame (the server's
// presence snapshot) completes the join and stamps the connect time.
func (s *session) serve(ctx context.Context, conn *websocket.Conn, began time.Time) {
	s.wmu.Lock()
	s.conn = conn
	s.wmu.Unlock()
	closed := make(chan struct{})
	defer close(closed)
	go func() {
		select {
		case <-ctx.Done():
			s.close()
		case <-closed:
		}
	}()
	defer conn.Close()

	up := false
	defer func() {
		if up {
			s.r.sessionDown()
		}
	}()
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if ctx.Err() == nil {
				s.r.wsErrors.Add(1)
			}
			return
		}
		if !up {
			up = true
			s.markUp(ctx, began)
		}
		s.r.onFrame(data)
	}
}

// markUp counts the session as connected.
func (s *session) markUp(ctx context.Context, began time.Time) {
	s.r.sessionUp()
	if !s.everUp.Swap(true) {
		s.r.sessionsEver.Add(1)
	}
	s.r.connectMs.Add(time.Since(began))
	if s.r.cfg.history {
		go s.fetchHistory(ctx)
	}
}

// close ends the connection politely.
func (s *session) close() {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.conn != nil {
		_ = s.conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	}
}

// sendTyping tells the channel that this user is typing.
func (s *session) sendTyping() {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if s.conn == nil {
		return
	}
	msg := map[string]any{"type": "typing", "payload": map[string]string{"channel_id": s.r.cfg.channel}}
	_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if s.conn.WriteJSON(msg) == nil {
		s.r.typingSent.Add(1)
	}
}

// frame is the envelope of every WebSocket message.
type frame struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// onFrame accounts for one message from the server.
func (r *run) onFrame(data []byte) {
	var f frame
	if json.Unmarshal(data, &f) != nil {
		r.framesBad.Add(1)
		return
	}
	switch f.Type {
	case "message_create":
		r.onMessage(f.Payload, kindMessage)
	case "message_update":
		r.onMessage(f.Payload, kindEdit)
	case "message_reaction":
		r.onReaction(f.Payload)
	case "typing":
		r.typingReceived.Add(1)
	}
}

// parseMarked reads the sequence number out of the content of a message this
// run sent ("lt <run> <seq> ..."), and the rest of the content after it.
func parseMarked(marker, content string) (int, string, bool) {
	rest, ok := strings.CutPrefix(content, marker)
	if !ok {
		return 0, "", false
	}
	num, tail, _ := strings.Cut(rest, " ")
	seq, err := strconv.Atoi(num)
	if err != nil {
		return 0, "", false
	}
	return seq, tail, true
}

// editedTag marks the content of an edited message.
const editedTag = "edit"

func (r *run) onMessage(payload json.RawMessage, k kind) {
	if !bytes.Contains(payload, []byte(r.marker)) {
		return
	}
	var m struct {
		Content string `json:"content"`
	}
	if json.Unmarshal(payload, &m) != nil {
		return
	}
	seq, tail, ok := parseMarked(r.marker, m.Content)
	if !ok {
		return
	}
	// A message_update is an edit only when it carries the edited content;
	// anything else (an embed arriving later) is not what was measured.
	if edited := strings.HasPrefix(tail, editedTag); edited != (k == kindEdit) {
		return
	}
	r.track.delivered(k, seq)
}

func (r *run) onReaction(payload json.RawMessage) {
	var m struct {
		MessageID string `json:"message_id"`
	}
	if json.Unmarshal(payload, &m) != nil {
		return
	}
	if seq, ok := r.track.ids.Load(m.MessageID); ok {
		r.track.delivered(kindReaction, seq.(int))
	}
}

// fetchHistory loads the latest messages the way opening the channel does.
func (s *session) fetchHistory(ctx context.Context) {
	url := s.r.cfg.base.String() + "/api/channels/" + s.r.cfg.channel + "/messages?limit=50"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	s.decorate(req)
	if resp, err := s.r.http.Do(req); err == nil {
		_, _ = readAll(resp)
	}
}
