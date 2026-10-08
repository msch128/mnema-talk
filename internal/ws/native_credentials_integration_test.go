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
	a.hub.mu.RLock()
	var pending *Client
	for c := range a.hub.pending {
		if c.User.ID == a.user.ID {
			pending = c
		}
	}
	a.hub.mu.RUnlock()
	if pending == nil {
		t.Fatal("real pending socket not owned by Hub")
	}
	// A different account is unaffected by account-wide credential retirement.
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
	if json.Unmarshal(a.post(t, "/auth/login", map[string]string{"username": other.Username, "password": nativeFixturePassword, "client_instance_id": uuid.NewString()}, http.StatusOK), &otherGrant) != nil {
		t.Fatal("fixture other grant failed")
	}
	otherSocket := a.dial(t, otherGrant)
	readNativeEvent(t, otherSocket, "voice_rooms")
	// Browser cookie and native families belong to the same genuine Hub.
	writer := httptest.NewRecorder()
	if err = a.accounts.Sessions.Start(writer, &a.user, 0); err != nil {
		t.Fatal("browser session fixture failed")
	}
	var cookie string
	for _, c := range writer.Result().Cookies() {
		cookie = c.Name + "=" + c.Value
	}
	browser, response, err := a.dialer.Dial("wss"+strings.TrimPrefix(a.server.URL, "https")+"/api/ws", http.Header{"Origin": {a.server.URL}, "Cookie": {cookie}})
	if err != nil || response.StatusCode != http.StatusSwitchingProtocols {
		t.Fatal("actual browser cookie WSS failed")
	}
	t.Cleanup(func() { _ = browser.Close() })
	readNativeEvent(t, browser, "voice_rooms")
	firstClient.voiceMu.Lock()
	secondClient.voiceMu.Lock()
	defer firstClient.voiceMu.Unlock()
	defer secondClient.voiceMu.Unlock()
	password, err := a.native.PasswordChangeHandler()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(password)
	defer server.Close()
	request, err := http.NewRequest(http.MethodPut, server.URL+"/auth/password", strings.NewReader(`{"current_password":"`+nativeFixturePassword+`","new_password":"synthetic-replacement-password"}`))
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
	if err != nil || result.StatusCode != http.StatusOK || len(result.Cookies()) != 0 || result.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("new credential TLS response failed")
	}
	var fresh nativeWSGrant
	if json.Unmarshal(data, &fresh) != nil || fresh.Family == first.Family || fresh.Instance != first.Instance || fresh.Sequence != 0 {
		t.Fatal("current initial replacement grant invalid")
	}
	for _, c := range []*Client{firstClient, secondClient, pending} {
		if c.nativeLive() || !c.native.securityClosed.Load() {
			t.Fatal("account native packet gate survived credential response")
		}
	}
	for _, socket := range []*websocket.Conn{firstSocket, secondSocket, pendingSocket, browser} {
		expectNativeClosed(t, socket)
	}
	unblock()
	// Old browser cookie is rejected by the current account token_version.
	_, oldResponse, err := a.dialer.Dial("wss"+strings.TrimPrefix(a.server.URL, "https")+"/api/ws", http.Header{"Origin": {a.server.URL}, "Cookie": {cookie}})
	if err == nil || oldResponse == nil || oldResponse.StatusCode != http.StatusUnauthorized {
		t.Fatal("old browser cookie survived password change")
	}
	_ = oldResponse.Body.Close()
	freshSocket := a.dial(t, fresh)
	readNativeEvent(t, freshSocket, "voice_rooms")
	if otherSocket.WriteJSON(map[string]any{"type": "ping", "payload": map[string]any{"t": 1}}) != nil {
		t.Fatal("unrelated account socket closed")
	}
	readNativeEvent(t, otherSocket, "pong")
}
