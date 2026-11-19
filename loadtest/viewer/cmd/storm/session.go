package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mnema-xyz/talk/loadtest/viewer/internal/lt"
)

// session is one browser tab: a socket, the client's backoff and its resync.
type session struct {
	idx  int
	tok  lt.Token
	r    *run
	http *http.Client
	// back: the session has been back once since the restart.
	back bool
}

func (s *session) xff() string {
	return fmt.Sprintf("10.%d.%d.%d", (s.idx>>16)&255, (s.idx>>8)&255, s.idx&255)
}

func (s *session) decorate(h http.Header) {
	h.Set("Cookie", s.r.cfg.cookie+"="+s.tok.Token)
	h.Set("Origin", lt.Origin(s.r.cfg.base))
	h.Set("X-Forwarded-For", s.xff())
}

// get sends one GET and counts its outcome under name.
func (s *session) get(ctx context.Context, name, path string) int {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.r.cfg.base.String()+path, nil)
	if err != nil {
		return 0
	}
	s.decorate(req.Header)
	began := time.Now()
	resp, err := s.http.Do(req)
	if err != nil {
		s.r.count(name, 0)
		return 0
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	s.r.count(name, resp.StatusCode)
	s.r.latencyOf(name).Add(time.Since(began))
	return resp.StatusCode
}

func (s *session) dial(ctx context.Context) (*websocket.Conn, error) {
	base := s.r.cfg.base
	scheme := map[string]string{"http": "ws", "https": "wss"}[base.Scheme]
	d := websocket.Dialer{HandshakeTimeout: connectTimeout, ReadBufferSize: 16 << 10, WriteBufferSize: 4 << 10}
	h := http.Header{}
	s.decorate(h)
	began := time.Now()
	conn, resp, err := d.DialContext(ctx, scheme+"://"+base.Host+"/api/ws?dm=1", h)
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	s.r.count("ws upgrade", status)
	if err != nil {
		return nil, err
	}
	s.r.upgrade.Load().Add(time.Since(began))
	conn.SetReadLimit(4 << 20)
	return conn, nil
}

// connectTimeout is the client's CONNECT_TIMEOUT_MS.
const connectTimeout = 10 * time.Second

// resync is what stores/chat/socket.ts resyncState asks for after an open, for
// a member without admin rights who has a text channel open. Everything goes
// out at once, as the browser sends it; the direct message settings follow the
// list of conversations.
func (s *session) resync(ctx context.Context, reconnect bool) {
	paths := []string{
		"/api/permissions", "/api/roles", "/api/emoji", "/api/health", "/api/channels",
		"/api/members", "/api/read-state", "/api/notification-settings", "/api/threads/following",
		"/api/push/config", "/api/channels/" + s.r.cfg.channel + "/messages?limit=50",
	}
	if reconnect {
		paths = append(paths, "/api/me/blocks")
	}
	var wg sync.WaitGroup
	for _, p := range paths {
		wg.Go(func() { s.get(ctx, endpoint(p), p) })
	}
	wg.Go(func() {
		if s.get(ctx, "/api/dms", "/api/dms") == http.StatusOK {
			s.get(ctx, "/api/users/me/dm-settings", "/api/users/me/dm-settings")
		}
	})
	wg.Wait()
}

// endpoint names a path without its ids and query.
func endpoint(p string) string {
	if len(p) > len("/api/channels/") && p[:len("/api/channels/")] == "/api/channels/" {
		return "/api/channels/{id}/messages"
	}
	return p
}

// open dials and waits for the first frame (the greeting). It returns the
// open connection, or nil when the attempt failed.
func (s *session) open(ctx context.Context) *websocket.Conn {
	conn, err := s.dial(ctx)
	if err != nil {
		return nil
	}
	_ = conn.SetReadDeadline(time.Now().Add(connectTimeout))
	if _, _, err := conn.ReadMessage(); err != nil {
		conn.Close()
		return nil
	}
	_ = conn.SetReadDeadline(time.Time{})
	return conn
}

// signedIn is checkAuth: true signed in, false the session is gone, and an
// unreachable or failing server keeps the client trying.
func (s *session) signedIn(ctx context.Context) (ok, gone bool) {
	switch s.get(ctx, "/api/auth/me", "/api/auth/me") {
	case http.StatusOK:
		return true, false
	case http.StatusUnauthorized:
		return false, true
	}
	return false, false
}

// reconnect waits out the backoff windows until checkAuth answers and a
// socket opens. nil: the session is gone or the run is over.
func (s *session) reconnect(ctx context.Context) *websocket.Conn {
	window := backoffBase
	for ctx.Err() == nil {
		if !sleep(ctx, s.r.cfg.wait(window)) {
			return nil
		}
		window = nextWindow(window)
		ok, gone := s.signedIn(ctx)
		if gone {
			return nil
		}
		if !ok {
			continue
		}
		if conn := s.open(ctx); conn != nil {
			return conn
		}
	}
	return nil
}

// hold reads the connection until it ends.
func hold(conn *websocket.Conn) {
	defer conn.Close()
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
