//go:build integration

package ws

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
)

func seedUnrelatedUserSocket(t *testing.T, a *nativeWSApp) *websocket.Conn {
	t.Helper()
	other := auth.User{ID: uuid.New(), Username: "cred-other-" + uuid.NewString()[:12]}
	hash, err := auth.HashPassword(nativeFixturePassword)
	if err != nil {
		t.Fatal("fixture hash failed")
	}
	if _, err = a.pool.Exec(context.Background(), `INSERT INTO users(id,username,display_name,password_hash,role) VALUES($1,$2,$2,$3,'user')`, other.ID, other.Username, hash); err != nil {
		t.Fatal("fixture account creation failed")
	}
	t.Cleanup(func() { _, _ = a.pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, other.ID) })
	var otherGrant nativeWSGrant
	if err := json.Unmarshal(a.post(t, "/auth/login", map[string]string{"username": other.Username, "password": nativeFixturePassword, "client_instance_id": uuid.NewString()}, http.StatusOK), &otherGrant); err != nil {
		t.Fatal("fixture other grant failed")
	}
	otherSocket := a.dial(t, otherGrant)
	readNativeEvent(t, otherSocket, "voice_rooms")
	return otherSocket
}

func dialBrowserCookieSocket(t *testing.T, a *nativeWSApp) (*websocket.Conn, string) {
	t.Helper()
	writer := httptest.NewRecorder()
	if err := a.accounts.Sessions.Start(writer, &a.user, 0); err != nil {
		t.Fatal("browser session fixture failed")
	}
	var cookie string
	for _, c := range writer.Result().Cookies() {
		cookie = c.Name + "=" + c.Value
	}
	wsURL := "wss" + strings.TrimPrefix(a.server.URL, "https") + "/api/ws"
	browser, response, err := a.dialer.Dial(wsURL, http.Header{"Origin": {a.server.URL}, "Cookie": {cookie}})
	if err != nil {
		t.Fatalf("actual browser cookie WSS failed: %v", err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("browser WSS status = %d, want 101", response.StatusCode)
	}
	t.Cleanup(func() { _ = browser.Close() })
	readNativeEvent(t, browser, "voice_rooms")
	return browser, cookie
}

func executePasswordChangeRequest(t *testing.T, a *nativeWSApp, first nativeWSGrant) nativeWSGrant {
	t.Helper()
	password, err := a.native.PasswordChangeHandler()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(password)
	defer server.Close()
	reqBody := `{"current_password":"` + nativeFixturePassword + `","new_password":"synthetic-replacement-password"}`
	request, err := http.NewRequest(http.MethodPut, server.URL+"/auth/password", strings.NewReader(reqBody))
	if err != nil {
		t.Fatal("fixture request failed")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+first.Access)
	client := server.Client()
	client.Timeout = 2 * time.Second
	result, err := client.Do(request)
	if err != nil {
		t.Fatal("password response waited for blocked voice cleanup")
	}
	defer result.Body.Close()
	data, err := io.ReadAll(result.Body)
	if err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("new credential status = %d, want 200", result.StatusCode)
	}
	if len(result.Cookies()) != 0 || result.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("unexpected cookies (%d) or cache control (%q)", len(result.Cookies()), result.Header.Get("Cache-Control"))
	}
	var fresh nativeWSGrant
	if err := json.Unmarshal(data, &fresh); err != nil {
		t.Fatal(err)
	}
	if fresh.Family == first.Family || fresh.Instance != first.Instance || fresh.Sequence != 0 {
		t.Fatalf("current initial replacement grant invalid: %+v", fresh)
	}
	return fresh
}

func assertCutoffSockets(t *testing.T, clients []*Client, sockets []*websocket.Conn) {
	t.Helper()
	for _, c := range clients {
		if c.commandsAllowed() {
			t.Fatal("account native commands still allowed")
		}
		if !c.native.securityClosed.Load() {
			t.Fatal("account native packet gate survived credential response")
		}
	}
	for _, socket := range sockets {
		expectNativeClosed(t, socket)
	}
}

func findPendingClient(t *testing.T, a *nativeWSApp) *Client {
	t.Helper()
	a.hub.mu.RLock()
	defer a.hub.mu.RUnlock()
	for c := range a.hub.pending {
		if c.User.ID == a.user.ID {
			return c
		}
	}
	t.Fatal("real pending socket not owned by Hub")
	return nil
}

func TestNativeCredentialsActualTLSBulkAccountCutoffPendingAndHeldVoiceCleanup(t *testing.T) {
	barrier := &realNativeBarrier{pauseAt: 6, checked: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	unblock := func() { once.Do(func() { close(barrier.release) }) }
	defer unblock()
	a := newNativeWSApp(t, nil, func(original NativeAuthenticator) NativeAuthenticator { barrier.original = original; return barrier })
	first, second := a.login(t), a.login(t)
	firstSocket, secondSocket := a.dial(t, first), a.dial(t, second)
	readNativeEvent(t, firstSocket, "voice_rooms")
	readNativeEvent(t, secondSocket, "voice_rooms")
	firstClient, secondClient := a.client(t, first.Family), a.client(t, second.Family)
	third := a.login(t)
	pendingSocket := a.dial(t, third)
	select {
	case <-barrier.checked:
	case <-time.After(2 * time.Second):
		t.Fatal("actual final admission did not pause")
	}
	pending := findPendingClient(t, a)
	// A different account is unaffected by account-wide credential retirement.
	otherSocket := seedUnrelatedUserSocket(t, a)
	// Browser cookie and native families belong to the same genuine Hub.
	browser, cookie := dialBrowserCookieSocket(t, a)
	firstClient.voiceMu.Lock()
	secondClient.voiceMu.Lock()
	defer firstClient.voiceMu.Unlock()
	defer secondClient.voiceMu.Unlock()
	fresh := executePasswordChangeRequest(t, a, first)
	assertCutoffSockets(t, []*Client{firstClient, secondClient, pending}, []*websocket.Conn{firstSocket, secondSocket, pendingSocket, browser})
	unblock()
	// Old browser cookie is rejected by the current account token_version.
	wsURL := "wss" + strings.TrimPrefix(a.server.URL, "https") + "/api/ws"
	_, oldResponse, err := a.dialer.Dial(wsURL, http.Header{"Origin": {a.server.URL}, "Cookie": {cookie}})
	if err == nil {
		t.Fatal("old browser cookie survived password change")
	}
	if oldResponse != nil {
		_ = oldResponse.Body.Close()
		if oldResponse.StatusCode != http.StatusUnauthorized {
			t.Fatalf("old browser status = %d, want 401", oldResponse.StatusCode)
		}
	}
	freshSocket := a.dial(t, fresh)
	readNativeEvent(t, freshSocket, "voice_rooms")
	if otherSocket.WriteJSON(map[string]any{"type": "ping", "payload": map[string]any{"t": 1}}) != nil {
		t.Fatal("unrelated account socket closed")
	}
	readNativeEvent(t, otherSocket, "pong")
}
