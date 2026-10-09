//go:build integration

package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
)

func TestNativeWSSDatabaseTimeoutClosesActualSocket(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	conn := a.dial(t, grant)
	readNativeEvent(t, conn, "voice_rooms")
	client := a.client(t, grant.Family)
	tx, err := a.pool.Begin(context.Background())
	if err != nil {
		t.Fatal("fixture lock transaction failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(context.Background(), `LOCK TABLE native_access_tokens IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal("fixture relation lock failed")
	}
	// The actual watchdog fires after five seconds. Its real PG lease lookup
	// times out in two seconds; a DB outage must not extend authorization.
	_ = conn.SetReadDeadline(time.Now().Add(9 * time.Second))
	for {
		_, _, err = conn.ReadMessage()
		if err == nil {
			continue
		}
		if e, ok := err.(interface{ Timeout() bool }); ok && e.Timeout() {
			t.Fatal("real watchdog DB timeout failed to close socket")
		}
		break
	}
	if client.nativeLive() || !client.native.securityClosed.Load() {
		t.Fatal("DB failure retained native authorization")
	}
}

func TestNativeWSSOldRealQueryCompletionCannotKillRenewedGeneration(t *testing.T) {
	for _, failOld := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "unauthorized"}[failOld], func(t *testing.T) {
			barrier := &realNativeBarrier{pauseAt: 3, checked: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			unblock := func() { release.Do(func() { close(barrier.release) }) }
			defer unblock()
			a := newNativeWSApp(t, nil, func(original NativeAuthenticator) NativeAuthenticator { barrier.original = original; return barrier })
			grant := a.login(t)
			conn := a.dial(t, grant)
			readNativeEvent(t, conn, "voice_rooms")
			client := a.client(t, grant.Family)
			if _, err := a.pool.Exec(context.Background(), `UPDATE native_session_families SET refresh_after=created_at WHERE id=$1`, grant.Family); err != nil {
				t.Fatal("fixture rotation eligibility failed")
			}
			var fresh nativeWSGrant
			if json.Unmarshal(a.post(t, "/auth/refresh", map[string]string{"refresh_token": grant.Refresh}, http.StatusOK), &fresh) != nil {
				t.Fatal("fixture grant decode failed")
			}
			if failOld {
				if _, err := a.pool.Exec(context.Background(), `DELETE FROM native_access_tokens WHERE family_id=$1 AND expires_at=$2`, grant.Family, grant.AccessExpiry); err != nil {
					t.Fatal("fixture access tombstone failed")
				}
			}
			done := make(chan struct{})
			go func() { client.revalidateNative(); close(done) }()
			select {
			case <-barrier.checked:
			case <-time.After(2 * time.Second):
				t.Fatal("genuine old query did not pause")
			}
			if conn.WriteJSON(map[string]any{"type": "native_access_renew", "payload": map[string]string{"access_token": fresh.Access}}) != nil {
				t.Fatal("actual native renewal write failed")
			}
			readNativeEvent(t, conn, "native_access_renewed")
			unblock()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("old validation did not finish")
			}
			if !client.nativeLive() {
				t.Fatal("old real query killed renewed generation")
			}
			a.hub.Broadcast("after-old-query", nil)
			readNativeEvent(t, conn, "after-old-query")
			generation, lease, _ := client.native.snapshot()
			current, err := a.native.AuthenticateAccessLease(context.Background(), fresh.Access)
			if err != nil || generation != 2 || !lease.Principal().SameNativeAccess(current.Principal()) {
				t.Fatal("old query changed current token generation")
			}
		})
	}
}

func TestNativeWSSFamilyQuotaPendingAndActive(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	var conns []*websocket.Conn
	for i := 0; i < 4; i++ {
		c := a.dial(t, grant)
		readNativeEvent(t, c, "voice_rooms")
		conns = append(conns, c)
	}
	denied := a.dial(t, grant) // upgraded candidate must receive no snapshots/membership.
	_ = denied.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := denied.ReadMessage(); err == nil {
		t.Fatal("family quota candidate received private frame")
	}
	a.hub.mu.RLock()
	count := 0
	for c := range a.hub.clients {
		if c.native != nil && c.native.binding.FamilyID() == grant.Family {
			count++
		}
	}
	pending := len(a.hub.pending)
	a.hub.mu.RUnlock()
	if count != 4 || pending != 0 {
		t.Fatal("family quota admitted excess active/pending connection")
	}
	_ = conns[0].Close()
	mediaProbeWait(t, func() bool { a.hub.mu.RLock(); defer a.hub.mu.RUnlock(); return len(a.hub.clients) == 3 }, "quota socket did not unregister")
	replacement := a.dial(t, grant)
	readNativeEvent(t, replacement, "voice_rooms")
}

func TestNativeWSSRenewBudgetBinaryAndMalformedClose(t *testing.T) {
	for _, mode := range []string{"binary", "malformed", "unknown-control", "budget"} {
		t.Run(mode, func(t *testing.T) {
			a := newNativeWSApp(t, nil, nil)
			grant := a.login(t)
			conn := a.dial(t, grant)
			readNativeEvent(t, conn, "voice_rooms")
			switch mode {
			case "binary":
				if conn.WriteMessage(websocket.BinaryMessage, []byte(`{"type":"ping"}`)) != nil {
					t.Fatal("fixture frame failed")
				}
			case "malformed":
				if conn.WriteJSON(map[string]any{"type": "native_access_renew", "payload": map[string]string{"access_token": "invalid"}}) != nil {
					t.Fatal("fixture frame failed")
				}
			case "unknown-control":
				if conn.WriteJSON(map[string]any{"type": "native_future_secret", "payload": grant.Access}) != nil {
					t.Fatal("fixture frame failed")
				}
			case "budget":
				for i := 0; i < 4; i++ {
					if conn.WriteJSON(map[string]any{"type": "native_access_renew", "payload": map[string]string{"access_token": grant.Access}}) != nil {
						t.Fatal("fixture renewal failed")
					}
					readNativeEvent(t, conn, "native_access_renewed")
				}
				if conn.WriteJSON(map[string]any{"type": "native_access_renew", "payload": map[string]string{"access_token": grant.Access}}) != nil {
					t.Fatal("fixture excess renewal failed")
				}
			}
			expectNativeClosed(t, conn)
		})
	}
}

func TestNativeWSSRenewDoesNotExtendOriginalHardDeadlines(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	first, err := a.native.AuthenticateAccessLease(context.Background(), grant.Access)
	if err != nil {
		t.Fatal("genuine initial lease failed")
	}
	state := newNativeSocketState(a.native, first)
	defer state.cancel()
	originalAccess, originalFamily := state.accessDeadline, state.familyDeadline
	fresh, err := a.native.AuthenticateAccessLease(context.Background(), grant.Access)
	if err != nil || !state.install(1, fresh, true) {
		t.Fatal("genuine same-access renewal failed")
	}
	if state.accessDeadline != originalAccess || state.familyDeadline != originalFamily {
		t.Fatal("same access requery extended monotonic hard deadline")
	}
	state.mu.Lock()
	state.generation = math.MaxUint64
	state.mu.Unlock()
	if state.install(math.MaxUint64, fresh, true) {
		t.Fatal("generation overflow accepted")
	}
	// A native packet gate must remain independent of any lease-state mutex.
	client := newClient(a.hub, nil, a.user, 0)
	client.native = state
	state.mu.Lock()
	done := make(chan bool, 1)
	go func() { done <- client.nativeLive() }()
	select {
	case live := <-done:
		if !live {
			t.Fatal("valid atomic lease rejected")
		}
	case <-time.After(time.Second):
		state.mu.Unlock()
		t.Fatal("packet gate waited on lease mutex")
	}
	state.mu.Unlock()
	if a.hub.expireNativeClient(client) {
		t.Fatal("obsolete expiry timer killed current live lease")
	}
	a.hub.terminateNativeGeneration(client, 1)
	if !client.nativeLive() {
		t.Fatal("obsolete query error killed current generation")
	}
	a.hub.terminateNativeClient(&Client{}) // browser/default path must remain unaffected.
	state.securityClosed.Store(true)
	if client.nativeLive() {
		t.Fatal("atomic terminal cutoff failed")
	}
	// A queued application frame after the seal must not consume typing state.
	client.handle("typing", []byte(`{"channel_id":"`+uuid.NewString()+`"}`))
	client.mu.Lock()
	typingEntries := len(client.typing)
	client.mu.Unlock()
	if typingEntries != 0 {
		t.Fatal("sealed client processed a queued typing frame")
	}
}

func TestNativeWSSHandshakeIPBudgetNoStore(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	// All rejected attempts share the actual handler IP budget, even before auth.
	for i := 0; i < 31; i++ {
		req, err := http.NewRequest(http.MethodGet, a.server.URL+"/api/native/v1/ws", nil)
		if err != nil {
			t.Fatal("fixture request failed")
		}
		response, err := a.server.Client().Do(req)
		if err != nil {
			t.Fatal("fixture handshake request failed")
		}
		status := response.StatusCode
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		want := http.StatusUnauthorized
		if i == 30 {
			want = http.StatusTooManyRequests
		}
		if status != want || response.Header.Get("Cache-Control") != "no-store" || len(response.Cookies()) != 0 {
			t.Fatal("actual handshake IP budget/cache/cookie mismatch")
		}
	}
}

func TestNativeWSSBlockedTypingLookupCanceledByCommittedRevoke(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	revoked := a.login(t)
	conn := a.dial(t, revoked)
	readNativeEvent(t, conn, "voice_rooms")
	other := mediaProbeUser(t, a)
	good := mediaProbeGrant(t, a, other)
	viewer := a.dial(t, good)
	readNativeEvent(t, viewer, "voice_rooms")
	channel := uuid.New()
	if _, err := a.pool.Exec(context.Background(), `INSERT INTO channels(id,name,type)VALUES($1,'native-typing-fixture','text')`, channel); err != nil {
		t.Fatal("fixture text channel failed")
	}
	defer func() { _, _ = a.pool.Exec(context.Background(), `DELETE FROM channels WHERE id=$1`, channel) }()
	tx, err := a.pool.Begin(context.Background())
	if err != nil {
		t.Fatal("fixture lock transaction failed")
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err = tx.Exec(context.Background(), `LOCK TABLE channels IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal("fixture channels lock failed")
	}
	if conn.WriteJSON(map[string]any{"type": "typing", "payload": map[string]any{"channel_id": channel}}) != nil {
		t.Fatal("fixture typing frame failed")
	}
	mediaProbeWait(t, func() bool {
		var count int
		err := a.pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock' AND query LIKE '%FROM channels WHERE id%'`).Scan(&count)
		return err == nil && count > 0
	}, "actual typing SQL never waited on relation lock")
	principal, err := a.sessions.AuthenticateAccess(context.Background(), revoked.Access)
	if err != nil {
		t.Fatal("genuine revoke principal failed")
	}
	if a.sessions.RevokeFamily(context.Background(), principal) != nil {
		t.Fatal("committed revoke failed")
	}
	expectNativeClosed(t, conn)
	if tx.Rollback(context.Background()) != nil {
		t.Fatal("fixture lock release failed")
	}
	a.hub.Broadcast("after-blocked-typing", nil)
	_ = viewer.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var event struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if viewer.ReadJSON(&event) != nil {
			t.Fatal("control viewer lost after blocked typing")
		}
		if event.Type == "typing" && strings.Contains(string(event.Payload), revoked.User.ID.String()) {
			t.Fatal("revoked blocked typing emitted late event")
		}
		if event.Type == "after-blocked-typing" {
			break
		}
	}
}

func TestNativeWSSPendingQuotaAndShutdownUsesActualHub(t *testing.T) {
	for _, action := range []string{"quota", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			barrier := &realNativeBarrier{pauseAt: 2, checked: make(chan struct{}), release: make(chan struct{})}
			var once sync.Once
			release := func() { once.Do(func() { close(barrier.release) }) }
			defer release()
			a := newNativeWSApp(t, nil, func(original NativeAuthenticator) NativeAuthenticator { barrier.original = original; return barrier })
			grant := a.login(t)
			pending := a.dial(t, grant)
			select {
			case <-barrier.checked:
			case <-time.After(2 * time.Second):
				t.Fatal("actual upgraded candidate never paused")
			}
			a.hub.mu.RLock()
			var candidate *Client
			for c := range a.hub.pending {
				candidate = c
			}
			pendingCount, activeCount := len(a.hub.pending), len(a.hub.clients)
			a.hub.mu.RUnlock()
			if candidate == nil || pendingCount != 1 || activeCount != 0 || len(candidate.send) != 0 {
				t.Fatal("pending candidate received membership/snapshots")
			}
			if action == "shutdown" {
				a.hub.Close()
				expectNativeClosed(t, pending)
				release()
				mediaProbeWait(t, func() bool {
					a.hub.mu.RLock()
					defer a.hub.mu.RUnlock()
					return len(a.hub.clients) == 0 && len(a.hub.pending) == 0
				}, "shutdown pending candidate leaked")
				if candidate.nativeLive() {
					t.Fatal("shutdown candidate retained native authorization")
				}
				return
			}
			for i := 0; i < 3; i++ {
				c := a.dial(t, grant)
				readNativeEvent(t, c, "voice_rooms")
			}
			fifth := a.dial(t, grant)
			expectNativeClosed(t, fifth)
			a.hub.mu.RLock()
			pendingCount, activeCount = len(a.hub.pending), len(a.hub.clients)
			a.hub.mu.RUnlock()
			if pendingCount != 1 || activeCount != 3 || len(candidate.send) != 0 {
				t.Fatal("mixed pending/active family quota failed")
			}
			release()
			readNativeEvent(t, pending, "voice_rooms")
		})
	}
}

func TestNativeWSSRejectsNonemptyBrokenBodyAndNonUpgrade(t *testing.T) {
	a := newNativeWSApp(t, nil, nil)
	grant := a.login(t)
	for _, broken := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "https://example.invalid/api/native/v1/ws", strings.NewReader("private-body"))
		if broken {
			req.Body = nativeBrokenBody{}
		}
		req.Header.Set("Authorization", "Bearer "+grant.Access)
		response := httptest.NewRecorder()
		a.hub.NativeWebSocketHandler(a.native).ServeHTTP(response, req)
		if response.Code != http.StatusBadRequest || response.Header().Get("Cache-Control") != "no-store" || strings.Contains(response.Body.String(), grant.Access) {
			t.Fatal("native nonempty/broken body accepted or credential escaped")
		}
	}
	req, err := http.NewRequest(http.MethodGet, a.server.URL+"/api/native/v1/ws", nil)
	if err != nil {
		t.Fatal("fixture request failed")
	}
	req.Header.Set("Authorization", "Bearer "+grant.Access)
	response, err := a.server.Client().Do(req)
	if err != nil {
		t.Fatal("fixture nonupgrade request failed")
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("native non-WebSocket request upgraded")
	}
}

// Observe the actual readPump stopped inside currentVoice behind the owned
// client mutex. Stack text is used only as a boolean rendezvous and never logged.
func nativeFrameBlockedAt(t *testing.T, client *Client, function string) bool {
	t.Helper()
	buffer := make([]byte, 1<<20)
	n := runtime.Stack(buffer, true)
	receiver := fmt.Sprintf("(*Client).readPump(%p)", client)
	for _, goroutine := range bytes.Split(buffer[:n], []byte("\n\n")) {
		if bytes.Contains(goroutine, []byte(receiver)) && bytes.Contains(goroutine, []byte(function)) {
			return true
		}
	}
	return false
}

func TestNativeWSSBlockedParticipantFramesCannotMutateAfterCommittedSeal(t *testing.T) {
	for _, kind := range []string{"voice_speaking", "voice_mute_state"} {
		t.Run(kind, func(t *testing.T) {
			a := newNativeWSApp(t, nil, nil)
			grant := a.login(t)
			conn := a.dial(t, grant)
			readNativeEvent(t, conn, "voice_rooms")
			goodUser := mediaProbeUser(t, a)
			goodGrant := mediaProbeGrant(t, a, goodUser)
			viewer := a.dial(t, goodGrant)
			readNativeEvent(t, viewer, "voice_rooms")
			channel := mediaProbeChannel(t, a)
			if conn.WriteJSON(map[string]any{"type": "voice_join", "payload": map[string]any{"channel_id": channel}}) != nil {
				t.Fatal("fixture voice join failed")
			}
			readNativeEvent(t, conn, "voice_state_update")
			readNativeEvent(t, viewer, "voice_state_update")
			client := a.client(t, grant.Family)
			client.voiceMu.Lock()
			client.mu.Lock()
			var unlockState, unlockVoice sync.Once
			stateRelease := func() { unlockState.Do(func() { client.mu.Unlock() }) }
			voiceRelease := func() { unlockVoice.Do(func() { client.voiceMu.Unlock() }) }
			defer voiceRelease()
			defer stateRelease()
			payload := map[string]any{"active": true, "muted": true, "deafened": true}
			if conn.WriteJSON(map[string]any{"type": kind, "payload": payload}) != nil {
				t.Fatal("fixture participant frame failed")
			}
			mediaProbeWait(t, func() bool { return nativeFrameBlockedAt(t, client, "(*Client).currentVoice") }, "actual WSS participant handler never blocked on state mutex")
			principal, err := a.sessions.AuthenticateAccess(context.Background(), grant.Access)
			if err != nil {
				t.Fatal("genuine participant principal unavailable")
			}
			revoked := make(chan error, 1)
			go func() { revoked <- a.sessions.RevokeFamily(context.Background(), principal) }()
			mediaProbeWait(t, func() bool { return client.native.securityClosed.Load() }, "committed family did not seal blocked participant")
			// A sealed client remains registered while physical voice cleanup waits.
			// Its idle mutation must also be denied at the locked state boundary.
			a.hub.setIdle(client, true)
			a.hub.mu.RLock()
			idle := client.idle
			a.hub.mu.RUnlock()
			if idle {
				t.Fatal("sealed participant changed idle state during delayed cleanup")
			}
			stateRelease()
			mediaProbeWait(t, func() bool { return nativeFrameBlockedAt(t, client, "(*Hub).leaveCurrentVoice") }, "actual readPump did not complete blocked participant frame")
			client.mu.Lock()
			speaking := client.speaking
			client.mu.Unlock()
			a.hub.mu.RLock()
			mute := a.hub.voiceMute[voiceKey{client.User.ID, channel}]
			a.hub.mu.RUnlock()
			if speaking || mute.Muted || mute.Deafened {
				t.Fatal("late participant frame mutated sealed state")
			}
			a.hub.Broadcast("after-blocked-participant", nil)
			_ = viewer.SetReadDeadline(time.Now().Add(2 * time.Second))
			for {
				var event struct {
					Type    string          `json:"type"`
					Payload json.RawMessage `json:"payload"`
				}
				if viewer.ReadJSON(&event) != nil {
					t.Fatal("other-family viewer lost participant control")
				}
				if event.Type == "voice_speaking" || event.Type == "voice_mute_state" {
					t.Fatal("blocked participant frame published after seal")
				}
				if event.Type == "after-blocked-participant" {
					break
				}
			}
			voiceRelease()
			select {
			case err := <-revoked:
				if err != nil {
					t.Fatal("participant revoke failed after lock release")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("participant revoke cleanup stayed blocked")
			}
		})
	}
}

type nativeCloseAfterInitial struct {
	NativeAuthenticator
	close func()
}

func (c nativeCloseAfterInitial) AuthenticateNativeRequest(r *http.Request) (auth.NativeLease, error) {
	lease, err := c.NativeAuthenticator.AuthenticateNativeRequest(r)
	if err == nil {
		c.close()
	}
	return lease, err
}
func TestNativeWSSShutdownAfterActualInitialCheck(t *testing.T) {
	var app *nativeWSApp
	app = newNativeWSApp(t, nil, func(original NativeAuthenticator) NativeAuthenticator {
		return nativeCloseAfterInitial{NativeAuthenticator: original, close: func() { app.hub.Close() }}
	})
	grant := app.login(t)
	c, response, err := app.dialer.Dial("wss"+strings.TrimPrefix(app.server.URL, "https")+"/api/native/v1/ws", http.Header{"Authorization": []string{"Bearer " + grant.Access}})
	if c != nil {
		_ = c.Close()
		t.Fatal("shutdown candidate upgraded")
	}
	if err == nil || response == nil || response.StatusCode != http.StatusServiceUnavailable || response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("shutdown after genuine first check did not reject")
	}
	_ = response.Body.Close()
}
