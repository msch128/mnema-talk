//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestNativeCiphertextPreviewHTTPSForgedBearerIPBudget(t *testing.T) {
	a := newRelayTestApp(t, NativeSupported)
	for i := 0; i < 120; i++ {
		a.request(t, http.MethodGet, relayPath(a.channel), nil, strings.Repeat("A", 43), nil, 401)
	}
	a.request(t, http.MethodGet, relayPath(a.channel), nil, strings.Repeat("A", 43), nil, 429)
	// A separate fixed metadata boundary and browser route retain their budgets.
	a.request(t, http.MethodGet, NativePublicLegalPath, nil, "", nil, 200)
	a.request(t, http.MethodGet, "/api/legal", nil, "", nil, 200)
}

func TestNativeCiphertextPreviewMissingTypedPagingFailsClosed(t *testing.T) {
	a := newRelayTestApp(t, NativeSupported)
	grant := a.login(t, a.user)
	request := httptest.NewRequest(http.MethodGet, a.server.URL+relayPath(a.channel), nil)
	request.Header.Set("Authorization", "Bearer "+grant.Access)
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("channelID", a.channel.String())
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, ctx))
	response := httptest.NewRecorder()
	// Authenticate through the actual middleware, without the relay's validated
	// paging context. Accidental future handler miswiring must fail before SQL.
	a.relay.Native.RequireUser(httpx.Handle(a.relay.listCiphertext)).ServeHTTP(response, request)
	if response.Code != 400 {
		t.Fatal("read handler accepted missing typed pagination")
	}
}

func TestNativeCiphertextPreviewHTTPSLeaseExpiresWhileSQLBlocked(t *testing.T) {
	a := newRelayTestApp(t, NativeSupported)
	grant := a.login(t, a.user)
	// A genuine database access deadline is shortened before admission. Middleware
	// captures that exact deadline; a later blocked write must roll back after it.
	if _, err := a.pool.Exec(context.Background(), `UPDATE native_access_tokens SET expires_at=clock_timestamp()+interval '900 milliseconds' WHERE family_id=$1`, grant.Family); err != nil {
		t.Fatal("short fixture lease failed")
	}
	lock, err := a.pool.Begin(context.Background())
	if err != nil {
		t.Fatal("expiry barrier failed")
	}
	defer lock.Rollback(context.Background())
	if _, err = lock.Exec(context.Background(), `SELECT id FROM channels WHERE id=$1 FOR UPDATE`, a.channel); err != nil {
		t.Fatal("expiry channel lock failed")
	}
	done := make(chan int, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, a.server.URL+relayPath(a.channel), bytes.NewReader(relayUnitBody(uuid.New(), []byte{1}, []byte{21})))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+grant.Access)
		res, e := a.server.Client().Do(req)
		if e != nil {
			done <- 0
			return
		}
		_ = res.Body.Close()
		done <- res.StatusCode
	}()
	relayWaitForChannelLock(t, a)
	deadline := time.Now().Add(1300 * time.Millisecond)
	for {
		var expired bool
		if a.pool.QueryRow(context.Background(), `SELECT expires_at<=clock_timestamp() FROM native_access_tokens WHERE family_id=$1`, grant.Family).Scan(&expired) != nil {
			t.Fatal("real expiry observation failed")
		}
		if expired {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("database lease did not expire")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if lock.Rollback(context.Background()) != nil {
		t.Fatal("expiry barrier release failed")
	}
	select {
	case status := <-done:
		if status != 401 {
			t.Fatal("expired transaction committed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("expired write did not terminate")
	}
	var rows int
	if a.pool.QueryRow(context.Background(), `SELECT count(*) FROM native_ciphertext_events`).Scan(&rows) != nil || rows != 0 {
		t.Fatal("expired transaction persisted ciphertext")
	}
}
func TestNativeCiphertextPreviewHTTPSStorageFaultIsRedactedAndAtomic(t *testing.T) {
	a := newRelayTestApp(t, NativeSupported)
	grant := a.login(t, a.user)
	_, err := a.pool.Exec(context.Background(), `CREATE FUNCTION fixture_relay_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic_private_storage_marker'; END $$; CREATE TRIGGER fixture_relay_fail BEFORE INSERT ON native_ciphertext_events FOR EACH ROW EXECUTE FUNCTION fixture_relay_fail()`)
	if err != nil {
		t.Fatal("storage fault injection failed")
	}
	t.Cleanup(func() {
		_, _ = a.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS fixture_relay_fail ON native_ciphertext_events;DROP FUNCTION IF EXISTS fixture_relay_fail()`)
	})
	raw := a.rawRequest(t, http.MethodPost, relayPath(a.channel), relayUnitBody(uuid.New(), []byte{1}, []byte{99}), grant.Access, nil, 503)
	if bytes.Contains(raw, []byte("synthetic_private_storage_marker")) || bytes.Contains(raw, []byte("INSERT")) {
		t.Fatal("private storage diagnostics reached native response")
	}
	var rows int
	if a.pool.QueryRow(context.Background(), `SELECT count(*) FROM native_ciphertext_events`).Scan(&rows) != nil || rows != 0 {
		t.Fatal("failed transaction persisted event")
	}
	if _, err = a.pool.Exec(context.Background(), `DROP TRIGGER fixture_relay_fail ON native_ciphertext_events;DROP FUNCTION fixture_relay_fail()`); err != nil {
		t.Fatal("storage fault teardown failed")
	}
	a.rawRequest(t, http.MethodPost, relayPath(a.channel), relayUnitBody(uuid.New(), []byte{1}, []byte{88}), grant.Access, nil, 201)
}
func TestNativeCiphertextPreviewHTTPSCurrentDisabledAndVersionBudget(t *testing.T) {
	a := newRelayTestApp(t, NativeSupported)
	grant := a.login(t, a.user)
	other := a.login(t, a.admin)
	path := relayPath(a.channel)
	if _, err := a.pool.Exec(context.Background(), `UPDATE users SET disabled_at=clock_timestamp() WHERE id=$1`, a.user.ID); err != nil {
		t.Fatal("disable fixture failed")
	}
	a.request(t, http.MethodGet, path, nil, grant.Access, nil, 401)
	a.rawRequest(t, http.MethodPost, path, relayUnitBody(uuid.New(), []byte{1}, []byte{1}), grant.Access, nil, 401)
	// Another account/family remains authorized.
	a.request(t, http.MethodGet, path, nil, other.Access, nil, 200)
	if _, err := a.pool.Exec(context.Background(), `UPDATE users SET disabled_at=NULL,token_version=token_version+1 WHERE id=$1`, a.user.ID); err != nil {
		t.Fatal("version fixture failed")
	}
	a.request(t, http.MethodGet, path, nil, grant.Access, nil, 401)
	renewed := a.login(t, a.user)
	a.request(t, http.MethodGet, path, nil, renewed.Access, nil, 200)
	// The exact relay budget is per current account, separately from shared browser
	// metadata budgets. GET and POST share the same bounded relay admission counter.
	for i := 0; i < 59; i++ {
		a.request(t, http.MethodGet, path, nil, renewed.Access, nil, 200)
	}
	a.rawRequest(t, http.MethodPost, path, relayUnitBody(uuid.New(), []byte{1}, []byte{2}), renewed.Access, nil, 429)
	a.request(t, http.MethodGet, path, nil, other.Access, nil, 200)
}
func TestNativeCiphertextPreviewMetadataSocketNeverReceivesCiphertext(t *testing.T) {
	a := newRelayTestApp(t, NativeSupported)
	grant := a.login(t, a.user)
	c := a.dial(t, grant)
	previewReadUntil(t, c, "presence_snapshot", nil)
	a.rawRequest(t, http.MethodPost, relayPath(a.channel), relayUnitBody(uuid.New(), []byte{7}, []byte{51, 52, 53}), grant.Access, nil, 201)
	// A queued ping creates a positive causal marker after publication. Inspect all
	// received events through it; no ciphertext event/bytes may reach metadata WSS.
	if c.WriteJSON(map[string]any{"type": "ping", "payload": map[string]int{"t": 27}}) != nil {
		t.Fatal("metadata socket ping failed")
	}
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, raw, err := c.ReadMessage()
		if err != nil {
			t.Fatal("metadata socket positive control absent")
		}
		var event struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &event) != nil {
			t.Fatal("metadata event invalid")
		}
		if bytes.Contains(raw, []byte(`"ciphertext"`)) || bytes.Contains(raw, []byte(`"group_id"`)) || event.Type == "ciphertext_event" {
			t.Fatal("opaque relay event entered metadata socket")
		}
		if event.Type == "pong" {
			break
		}
	}
}
func TestNativeCiphertextPreviewHTTPSLimitedReaderAndCompatibility(t *testing.T) {
	for _, status := range []NativeCompatibility{NativeDeprecated, NativeUnsupported} {
		t.Run(string(status), func(t *testing.T) {
			a := newRelayTestApp(t, status)
			if status == NativeUnsupported {
				a.request(t, http.MethodGet, relayPath(a.channel), nil, "", nil, 503)
				return
			}
			grant := a.login(t, a.user)
			a.request(t, http.MethodGet, relayPath(a.channel), nil, grant.Access, nil, 200)
		})
	}
	a := newRelayTestApp(t, NativeSupported)
	grant := a.login(t, a.user)
	// Unknown-length JSON is bounded in the actual HTTPS reader, including a body
	// which only becomes invalid after exhausting the maximum permitted bytes.
	raw := append([]byte(`{"ciphertext":"`), bytes.Repeat([]byte("A"), int(nativeCiphertextBodyMax))...)
	req, _ := http.NewRequest(http.MethodPost, a.server.URL+relayPath(a.channel), io.NopCloser(bytes.NewReader(raw)))
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	req.Header.Set("Authorization", "Bearer "+grant.Access)
	req.Header.Set("Content-Type", "application/json")
	res, err := a.server.Client().Do(req)
	if err != nil {
		t.Fatal("bounded chunked HTTPS failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 413 || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("unbounded streaming body accepted")
	}
	// A canonical maximum cursor is accepted without overflow or a fabricated page.
	p := relayPage(t, a.request(t, http.MethodGet, relayPath(a.channel)+"?after="+strconv.FormatInt(9223372036854775807, 10)+"&limit=100", nil, grant.Access, nil, 200))
	if len(p.Events) != 0 || p.NextAfter != 9223372036854775807 {
		t.Fatal("maximum cursor changed")
	}
}
