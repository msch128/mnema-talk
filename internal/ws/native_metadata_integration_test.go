//go:build integration

package ws

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
)

func metadataTLSFixture(t *testing.T, a *nativeWSApp, authenticator NativeAuthenticator) (*httptest.Server, *websocket.Dialer) {
	t.Helper()
	s := httptest.NewTLSServer(a.hub.NativeMetadataWebSocketHandler(authenticator))
	t.Cleanup(s.Close)
	d := *websocket.DefaultDialer
	d.TLSClientConfig = s.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	if d.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("metadata TLS verification disabled")
	}
	return s, &d
}
func metadataDial(t *testing.T, s *httptest.Server, d *websocket.Dialer, access string) *websocket.Conn {
	t.Helper()
	c, _, err := d.Dial("wss"+strings.TrimPrefix(s.URL, "https"), http.Header{"Authorization": []string{"Bearer " + access}})
	if err != nil {
		t.Fatal("actual metadata TLS-WSS handshake failed")
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func metadataClosed(t *testing.T, c *websocket.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, _, err := c.ReadMessage()
		if err == nil {
			continue
		}
		if e, ok := err.(interface{ Timeout() bool }); ok && e.Timeout() {
			t.Fatal("invalid metadata connection not closed")
		}
		return
	}
}
func metadataDrainMembership(t *testing.T, h *Hub) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.RLock()
		n := len(h.clients) + len(h.pending)
		h.mu.RUnlock()
		if n == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("metadata closed member not removed")
}
func TestNativeMetadataRealHandshakeFramesAndFamilyQuota(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	s, d := metadataTLSFixture(t, a, a.native)
	for _, tc := range []struct {
		origin bool
		body   bool
		access string
		want   int
	}{{true, false, grant.Access, 403}, {false, true, grant.Access, 400}, {false, false, strings.Repeat("A", 43), 401}, {false, false, grant.Access, 400}} {
		var body io.Reader
		if tc.body {
			body = strings.NewReader("x")
		}
		req, err := http.NewRequest(http.MethodGet, s.URL, body)
		if err != nil {
			t.Fatal("fixture handshake request failed")
		}
		req.Header.Set("Authorization", "Bearer "+tc.access)
		if tc.origin {
			req.Header["Origin"] = []string{""}
		}
		res, err := s.Client().Do(req)
		if err != nil {
			t.Fatal("fixture handshake HTTPS failed")
		}
		_ = res.Body.Close()
		if res.StatusCode != tc.want || res.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("handshake status%d expected%d", res.StatusCode, tc.want)
		}
	}
	for _, frame := range []struct {
		kind int
		raw  string
	}{{websocket.BinaryMessage, "binary"}, {websocket.TextMessage, `{"type":"ping","payload":{"t":1,"t":2}}`}, {websocket.TextMessage, `{"type":"native_unknown","payload":{}}`}, {websocket.TextMessage, strings.Repeat("x", 1025)}} {
		c := metadataDial(t, s, d, grant.Access)
		readNativeEvent(t, c, "presence_snapshot")
		if c.WriteMessage(frame.kind, []byte(frame.raw)) != nil {
			t.Fatal("frame fixture send failed")
		}
		metadataClosed(t, c)
		metadataDrainMembership(t, a.hub)
	}
	active := make([]*websocket.Conn, 0, 4)
	for range 4 {
		c := metadataDial(t, s, d, grant.Access)
		readNativeEvent(t, c, "presence_snapshot")
		active = append(active, c)
	}
	fifth := metadataDial(t, s, d, grant.Access)
	metadataClosed(t, fifth)
	for _, c := range active {
		if c.WriteJSON(map[string]any{"type": "ping", "payload": map[string]int{"t": 1}}) != nil {
			t.Fatal("quota evicted existing socket")
		}
		readNativeEvent(t, c, "pong")
		_ = c.Close()
	}
	metadataDrainMembership(t, a.hub)
}

type metadataPausedRevalidator struct {
	NativeAuthenticator
	entered chan struct{}
	release chan struct{}
}

func (a *metadataPausedRevalidator) RevalidateNativeLease(ctx context.Context, p auth.NativePrincipal) (auth.NativeLease, error) {
	close(a.entered)
	select {
	case <-a.release:
	case <-ctx.Done():
		return auth.NativeLease{}, ctx.Err()
	}
	return a.NativeAuthenticator.RevalidateNativeLease(ctx, p)
}
func TestNativeMetadataPendingAdmissionRealFamilyRevoke(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	paused := &metadataPausedRevalidator{NativeAuthenticator: a.native, entered: make(chan struct{}), release: make(chan struct{})}
	s, d := metadataTLSFixture(t, a, paused)
	c := metadataDial(t, s, d, grant.Access)
	select {
	case <-paused.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("genuine final DB revalidation not reached")
	}
	a.hub.mu.RLock()
	pending, visible := len(a.hub.pending), len(a.hub.clients)
	a.hub.mu.RUnlock()
	if pending != 1 || visible != 0 {
		t.Fatal("metadata pending admission visible before DB revalidation")
	}
	lease, err := a.native.AuthenticateAccessLease(context.Background(), grant.Access)
	if err != nil {
		t.Fatal("actual revocation principal lookup failed")
	}
	if err := a.sessions.RevokeFamily(context.Background(), lease.Principal()); err != nil {
		t.Fatal("actual family revoke failed")
	}
	close(paused.release)
	metadataClosed(t, c)
	metadataDrainMembership(t, a.hub)
}
func TestNativeMetadataUnavailableAuthenticator(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	defer h.Close()
	res := httptest.NewRecorder()
	h.NativeMetadataWebSocketHandler(nil).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "https://community.example.invalid", nil))
	if res.Code != 503 || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unavailable metadata service admitted request")
	}
}
