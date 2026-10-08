//go:build integration

package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

const nativeFixturePassword = "native-fixture-password"

type nativeWSGrant struct {
	Access       string    `json:"access_token"`
	Refresh      string    `json:"refresh_token"`
	Family       uuid.UUID `json:"family_id"`
	Instance     uuid.UUID `json:"client_instance_id"`
	Sequence     uint64    `json:"refresh_sequence"`
	AccessExpiry time.Time `json:"access_expires_at"`
	FamilyExpiry time.Time `json:"family_expires_at"`
	User         auth.User `json:"user"`
}

type nativeWSApp struct {
	pool     *db.Pool
	hub      *Hub
	accounts *auth.Handler
	sessions *auth.NativeSessions
	native   *auth.NativeHandler
	user     auth.User
	server   *httptest.Server
	dialer   websocket.Dialer
}

func newNativeWSApp(t *testing.T, voice *sfu.SFU, wrap func(NativeAuthenticator) NativeAuthenticator) *nativeWSApp {
	t.Helper()
	auth.SetPasswordCostForTests(bcrypt.MinCost)
	p := testutil.DB(t)
	u := auth.User{ID: uuid.New(), Username: "nws-" + uuid.NewString()[:20], Role: auth.RoleUser}
	hash, err := auth.HashPassword(nativeFixturePassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(context.Background(), `INSERT INTO users(id,username,display_name,password_hash,role)VALUES($1,$2,$2,$3,'user')`, u.ID, u.Username, hash); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = p.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, u.ID) })
	accounts := auth.NewHandler(&auth.Sessions{DB: p, Secret: strings.Repeat("s", 32), TTL: 30 * 24 * time.Hour, Secure: true}, nil)
	sessions, err := auth.NewNativeSessions(p, auth.NativePolicy{SessionLifetime: 30 * 24 * time.Hour, FamilyLifetime: 30 * 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	native, err := auth.NewNativeHandler(accounts, sessions)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHub(p, accounts.Sessions, voice, nil)
	accounts.Live = h
	if err := sessions.BindNativeFamilyControl(h); err != nil {
		t.Fatal(err)
	}
	a := &nativeWSApp{pool: p, hub: h, accounts: accounts, sessions: sessions, native: native, user: u}
	var socketAuth NativeAuthenticator = native
	if wrap != nil {
		socketAuth = wrap(socketAuth)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/native/v1/ws", h.NativeWebSocketHandler(socketAuth))
	mux.Handle("/api/native/v1/", http.StripPrefix("/api/native/v1", native.Handler()))
	mux.HandleFunc("/api/ws", h.HandleWebSocket)
	a.server = httptest.NewTLSServer(mux)
	h.Origins = []string{a.server.URL}
	t.Cleanup(a.server.Close)
	t.Cleanup(h.Close)
	a.dialer = *websocket.DefaultDialer
	a.dialer.TLSClientConfig = a.server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	if a.dialer.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("fixture TLS verification disabled")
	}
	return a
}

func (a *nativeWSApp) post(t *testing.T, path string, body any, want int) []byte {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal("fixture JSON encoding failed")
	}
	r, err := http.NewRequest(http.MethodPost, a.server.URL+"/api/native/v1"+path, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal("fixture request failed")
	}
	r.Header.Set("Content-Type", "application/json")
	response, err := a.server.Client().Do(r)
	if err != nil {
		t.Fatal("fixture native HTTP failed")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != want || response.Header.Get("Cache-Control") != "no-store" || len(response.Cookies()) != 0 {
		t.Fatalf("native HTTP status/cache/cookie mismatch: %d expected %d", response.StatusCode, want)
	}
	return data
}

func (a *nativeWSApp) login(t *testing.T) nativeWSGrant {
	t.Helper()
	id := uuid.New()
	data := a.post(t, "/auth/login", map[string]string{"username": a.user.Username, "password": nativeFixturePassword, "client_instance_id": id.String()}, http.StatusOK)
	var grant nativeWSGrant
	if json.Unmarshal(data, &grant) != nil || len(grant.Access) != 43 || len(grant.Refresh) != 43 || grant.Family == uuid.Nil || grant.Instance != id || grant.Sequence != 0 || grant.User.ID != a.user.ID {
		t.Fatal("native HTTP grant contract invalid")
	}
	return grant
}

func (a *nativeWSApp) dial(t *testing.T, grant nativeWSGrant) *websocket.Conn {
	t.Helper()
	c, response, err := a.dialer.Dial("wss"+strings.TrimPrefix(a.server.URL, "https")+"/api/native/v1/ws", http.Header{"Authorization": []string{"Bearer " + grant.Access}})
	if err != nil || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("real native WSS upgrade failed")
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func readNativeEvent(t *testing.T, c *websocket.Conn, want string) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var event struct {
			Type string `json:"type"`
		}
		if err := c.ReadJSON(&event); err != nil {
			t.Fatalf("native event missing: %s", want)
		}
		if event.Type == want {
			return
		}
	}
}

func expectNativeClosed(t *testing.T, c *websocket.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := c.ReadMessage()
		if err == nil {
			continue
		}
		if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
			t.Fatal("native socket did not close")
		}
		return
	}
}

func (a *nativeWSApp) client(t *testing.T, family uuid.UUID) *Client {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.hub.mu.RLock()
		var found *Client
		for c := range a.hub.clients {
			if c.native != nil && c.native.binding.FamilyID() == family {
				found = c
			}
		}
		a.hub.mu.RUnlock()
		if found != nil {
			return found
		}
		if time.Now().After(deadline) {
			t.Fatal("native candidate never entered SAME Hub")
		}
		time.Sleep(time.Millisecond)
	}
}

type realNativeBarrier struct {
	original         NativeAuthenticator
	pauseAt          int32
	calls            atomic.Int32
	checked, release chan struct{}
}

func (b *realNativeBarrier) pause() {
	if b.calls.Add(1) == b.pauseAt {
		close(b.checked)
		<-b.release
	}
}
func (b *realNativeBarrier) AuthenticateNativeRequest(r *http.Request) (auth.NativeLease, error) {
	l, e := b.original.AuthenticateNativeRequest(r)
	b.pause()
	return l, e
}
func (b *realNativeBarrier) RevalidateNativeLease(ctx context.Context, p auth.NativePrincipal) (auth.NativeLease, error) {
	l, e := b.original.RevalidateNativeLease(ctx, p)
	b.pause()
	return l, e
}
func (b *realNativeBarrier) AuthenticateAccessLease(ctx context.Context, raw string) (auth.NativeLease, error) {
	return b.original.AuthenticateAccessLease(ctx, raw)
}

func TestNativeWSSAdmissionRejectsRealCommittedRevocationDuringBothChecks(t *testing.T) {
	for _, action := range []string{"family", "refresh-reuse", "global-version", "disabled"} {
		for _, pauseAt := range []int32{1, 2} {
			t.Run(action+map[int32]string{1: "/initial", 2: "/final"}[pauseAt], func(t *testing.T) {
				barrier := &realNativeBarrier{pauseAt: pauseAt, checked: make(chan struct{}), release: make(chan struct{})}
				a := newNativeWSApp(t, nil, func(original NativeAuthenticator) NativeAuthenticator { barrier.original = original; return barrier })
				grant := a.login(t)
				type result struct {
					conn *websocket.Conn
					err  error
				}
				done := make(chan result, 1)
				go func() {
					c, _, err := a.dialer.Dial("wss"+strings.TrimPrefix(a.server.URL, "https")+"/api/native/v1/ws", http.Header{"Authorization": []string{"Bearer " + grant.Access}})
					done <- result{c, err}
				}()
				var once sync.Once
				release := func() { once.Do(func() { close(barrier.release) }) }
				t.Cleanup(release)
				select {
				case <-barrier.checked:
				case <-time.After(3 * time.Second):
					t.Fatal("real native check did not reach barrier")
				}
				a.hub.Broadcast("private-before-release", nil)
				a.hub.SendToUsers([]uuid.UUID{a.user.ID}, "private-directed", nil)
				a.hub.mu.RLock()
				if len(a.hub.clients) != 0 || len(a.hub.online) != 0 || len(a.hub.profiles) != 0 {
					t.Error("unverified native handshake published membership")
				}
				if pauseAt == 2 && len(a.hub.pending) != 1 {
					t.Error("upgraded native handshake missing pending ownership")
				}
				for c := range a.hub.pending {
					if len(c.send) != 0 {
						t.Error("pending native socket queued private events")
					}
				}
				a.hub.mu.RUnlock()
				principal, err := a.sessions.AuthenticateAccess(context.Background(), grant.Access)
				if err != nil {
					t.Fatal("real native principal lookup failed")
				}
				switch action {
				case "family":
					err = a.sessions.RevokeFamily(context.Background(), principal)
				case "refresh-reuse":
					if _, err := a.pool.Exec(context.Background(), `UPDATE native_session_families SET refresh_after=created_at WHERE id=$1`, grant.Family); err != nil {
						t.Fatal(err)
					}
					a.post(t, "/auth/refresh", map[string]string{"refresh_token": grant.Refresh}, http.StatusOK)
					a.post(t, "/auth/refresh", map[string]string{"refresh_token": grant.Refresh}, http.StatusUnauthorized)
				case "global-version":
					err = auth.RevokeSessions(context.Background(), a.pool, a.user.ID)
					a.hub.DisconnectUser(a.user.ID)
				case "disabled":
					err = auth.SetDisabled(context.Background(), a.pool, a.user.ID, true)
					a.hub.KickFromVoice(a.user.ID)
					a.hub.DisconnectUser(a.user.ID)
				}
				if err != nil {
					t.Fatal("real revocation failed")
				}
				release()
				var got result
				select {
				case got = <-done:
				case <-time.After(3 * time.Second):
					t.Fatal("native dial did not finish")
				}
				if got.err == nil {
					t.Cleanup(func() { _ = got.conn.Close() })
					_ = got.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
					if _, _, err := got.conn.ReadMessage(); err == nil {
						t.Fatal("revoked native admission received a private/snapshot frame")
					} else if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
						t.Fatal("revoked admission stayed open")
					}
				}
				a.hub.mu.RLock()
				defer a.hub.mu.RUnlock()
				if len(a.hub.pending) != 0 || len(a.hub.clients) != 0 || len(a.hub.online) != 0 {
					t.Fatal("revoked native admission retained Hub state")
				}
			})
		}
	}
}

func TestNativeWSSFamilyIsolationRenewAndActualSendCutoff(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	c := a.dial(t, grant)
	readNativeEvent(t, c, "voice_rooms")
	other := a.login(t)
	otherConn := a.dial(t, other)
	readNativeEvent(t, otherConn, "voice_rooms")
	cookie := httptest.NewRecorder()
	if err := a.accounts.Sessions.Start(cookie, &a.user, 0); err != nil {
		t.Fatal(err)
	}
	browser, _, err := a.dialer.Dial("wss"+strings.TrimPrefix(a.server.URL, "https")+"/api/ws", http.Header{"Origin": []string{a.server.URL}, "Cookie": []string{cookie.Result().Cookies()[0].String()}})
	if err != nil {
		t.Fatal("browser could not share native Hub")
	}
	t.Cleanup(func() { _ = browser.Close() })
	readNativeEvent(t, browser, "voice_rooms")
	if _, err := a.pool.Exec(context.Background(), `UPDATE native_session_families SET refresh_after=created_at WHERE id=$1`, grant.Family); err != nil {
		t.Fatal(err)
	}
	data := a.post(t, "/auth/refresh", map[string]string{"refresh_token": grant.Refresh}, http.StatusOK)
	var next nativeWSGrant
	if json.Unmarshal(data, &next) != nil || next.Family != grant.Family || next.Instance != grant.Instance || next.Sequence != 1 {
		t.Fatal("refresh family/instance/sequence metadata invalid")
	}
	if err := c.WriteJSON(map[string]any{"type": "native_access_renew", "payload": map[string]string{"access_token": next.Access}}); err != nil {
		t.Fatal("renew control write failed")
	}
	readNativeEvent(t, c, "native_access_renewed")
	client := a.client(t, grant.Family)
	_, current, _ := client.native.snapshot()
	principal, err := a.sessions.AuthenticateAccess(context.Background(), next.Access)
	if err != nil || !current.Principal().SameNativeAccess(principal) {
		t.Fatal("renew did not install genuine rotated access")
	}
	if err := a.sessions.RevokeFamily(context.Background(), principal); err != nil {
		t.Fatal(err)
	}
	a.hub.Broadcast("post-revoke-private", nil)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var event struct {
			Type string `json:"type"`
		}
		err := c.ReadJSON(&event)
		if err != nil {
			if timeout, ok := err.(interface{ Timeout() bool }); ok && timeout.Timeout() {
				t.Fatal("revoked socket stayed open")
			}
			break
		}
		if event.Type == "post-revoke-private" {
			t.Fatal("revoked native socket wrote post-revoke private event")
		}
	}
	if client.nativeLive() || len(client.send) > 0 { // previously queued data can remain physically buffered but never written
		if client.nativeLive() {
			t.Fatal("revoked native lease remained active")
		}
	}
	readNativeEvent(t, otherConn, "post-revoke-private")
	readNativeEvent(t, browser, "post-revoke-private")
	// Inspect actual wire after closure: a revoked connection cannot receive
	// subsequent private messages even with a queued event or another family.
	if _, _, err := c.ReadMessage(); err == nil {
		t.Fatal("revoked native socket wrote another event")
	}
}

func TestNativeWSSHandshakeBoundaryAndTokenKinds(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	for _, headers := range []http.Header{
		{}, {"Authorization": []string{"Bearer " + grant.Refresh}},
		{"Authorization": []string{"Bearer " + grant.Access, "Bearer " + grant.Access}},
		{"Authorization": []string{"Bearer " + grant.Access}, "Origin": []string{""}},
		{"Authorization": []string{"Bearer " + grant.Access}, "Cookie": []string{""}},
		{"Authorization": []string{"Bearer " + grant.Access}, "Sec-WebSocket-Protocol": []string{"private"}},
	} {
		c, response, err := a.dialer.Dial("wss"+strings.TrimPrefix(a.server.URL, "https")+"/api/native/v1/ws", headers)
		if err == nil {
			c.Close()
			t.Fatal("native handshake boundary admitted invalid headers")
		}
		if response == nil || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("native rejection lacked no-store")
		}
	}
	if c, _, err := a.dialer.Dial("wss"+strings.TrimPrefix(a.server.URL, "https")+"/api/ws", http.Header{"Authorization": []string{"Bearer " + grant.Access}}); err == nil {
		c.Close()
		t.Fatal("browser endpoint accepted native bearer without cookie/origin")
	}
	native := a.dial(t, grant)
	readNativeEvent(t, native, "voice_rooms")
	if err := native.WriteJSON(map[string]any{"type": "native_access_renew", "payload": map[string]string{"access_token": a.login(t).Access}}); err != nil {
		t.Fatal("foreign renew write failed")
	}
	expectNativeClosed(t, native)
}

func TestNativeWSSAccessExpiryAndStaleQueryCannotExtendLease(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	if _, err := a.pool.Exec(context.Background(), `UPDATE native_access_tokens SET expires_at=clock_timestamp()+interval '1 second' WHERE family_id=$1`, grant.Family); err != nil {
		t.Fatal(err)
	}
	c := a.dial(t, grant)
	readNativeEvent(t, c, "voice_rooms")
	client := a.client(t, grant.Family)
	_, _, deadline := client.native.snapshot()
	if deadline.After(time.Now().Add(time.Second)) {
		t.Fatal("short access did not clamp actual socket lease")
	}
	expectNativeClosed(t, c)
	a.hub.Broadcast("expired-private", nil)
	if client.nativeLive() {
		t.Fatal("expired native connection still authorized")
	}
	// No fake authorization: acquire genuine DB-backed leases for both tokens,
	// then model the stale completion at the state installation boundary.
	grant = a.login(t)
	old, err := a.native.AuthenticateAccessLease(context.Background(), grant.Access)
	if err != nil {
		t.Fatal(err)
	}
	state := newNativeSocketState(a.native, old)
	defer state.cancel()
	if _, err := a.pool.Exec(context.Background(), `UPDATE native_session_families SET refresh_after=created_at WHERE id=$1`, grant.Family); err != nil {
		t.Fatal(err)
	}
	var next nativeWSGrant
	if json.Unmarshal(a.post(t, "/auth/refresh", map[string]string{"refresh_token": grant.Refresh}, http.StatusOK), &next) != nil {
		t.Fatal("rotation decode failed")
	}
	fresh, err := a.native.AuthenticateAccessLease(context.Background(), next.Access)
	if err != nil {
		t.Fatal(err)
	}
	if !state.install(1, fresh, true) || state.install(1, old, false) {
		t.Fatal("stale validation overwrote rotated native generation")
	}
	if _, installed, _ := state.snapshot(); !installed.Principal().SameNativeAccess(fresh.Principal()) {
		t.Fatal("stale completion altered native access")
	}
	state.securityClosed.Store(true)
	if state.install(2, fresh, true) {
		t.Fatal("terminal native state resurrected")
	}
	if _, err := a.sessions.AuthenticateAccessLease(context.Background(), grant.Refresh); !errors.Is(err, auth.ErrNativeUnauthorized) {
		t.Fatal("refresh accepted as access lease")
	}
}
