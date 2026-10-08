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
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/testutil"
)

type relayTestApp struct {
	*previewTestApp
	relay                 *NativeCiphertextPreviewRouter
	channel, otherChannel uuid.UUID
}

func newRelayTestApp(t *testing.T, status NativeCompatibility) *relayTestApp {
	t.Helper()
	pool := testutil.DB(t)
	testutil.Reset(t, pool)
	server := httptest.NewUnstartedServer(nil)
	origin := "https://" + server.Listener.Addr().String()
	cfg, err := config.FromEnv(func(key string) (string, bool) {
		v, ok := map[string]string{"DATABASE_URL": "unused", "PUBLIC_URL": origin, "JWT_SECRET": "relay-fixture-secret-relay-fixture-secret", "MEDIA_RETENTION_DAYS": "0"}[key]
		return v, ok
	})
	if err != nil {
		t.Fatal("relay fixture configuration failed")
	}
	router, err := NewNativeCiphertextPreviewRouter(Deps{Config: cfg, DB: pool, Version: "relay-fixture"}, NativePreviewOptions{CommunityID: "native-relay-fixture", InstanceOrigin: origin, Compatibility: status})
	if err != nil {
		t.Fatal("relay router construction failed")
	}
	server.Config.Handler = router
	server.Config.ReadHeaderTimeout = 5 * time.Second
	server.Config.ReadTimeout = 5 * time.Second
	server.StartTLS()
	t.Cleanup(server.Close)
	t.Cleanup(router.Close)
	if server.Client().Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify {
		t.Fatal("relay TLS validation disabled")
	}
	a := &relayTestApp{previewTestApp: &previewTestApp{server: server, router: router.NativePreviewRouter, pool: pool}, relay: router, channel: uuid.New(), otherChannel: uuid.New()}
	hash, err := auth.HashPassword(nativePreviewFixturePassword)
	if err != nil {
		t.Fatal("relay fixture password hash failed")
	}
	a.user = auth.User{ID: uuid.New(), Username: "relay-user", Role: auth.RoleUser}
	a.admin = auth.User{ID: uuid.New(), Username: "relay-admin", Role: auth.RoleAdmin}
	for _, u := range []auth.User{a.user, a.admin} {
		if _, err = pool.Exec(context.Background(), `INSERT INTO users(id,username,display_name,password_hash,role)VALUES($1,$2,$2,$3,$4)`, u.ID, u.Username, hash, u.Role); err != nil {
			t.Fatal("relay fixture account failed")
		}
	}
	for _, id := range []uuid.UUID{a.channel, a.otherChannel} {
		if _, err = pool.Exec(context.Background(), `INSERT INTO channels(id,name,type)VALUES($1,'relay-channel','text')`, id); err != nil {
			t.Fatal("relay fixture channel failed")
		}
	}
	return a
}
func relayPath(channel uuid.UUID) string {
	return NativePreviewPrefix + "/channels/" + channel.String() + "/ciphertext-events"
}
func relayWire(t *testing.T, raw []byte) chat.NativeCiphertextWire {
	t.Helper()
	var wire chat.NativeCiphertextWire
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &wire) != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 8 || wire.ID == uuid.Nil || wire.Number < 1 || wire.ChannelID == uuid.Nil || wire.UserID == uuid.Nil || wire.CreatedAt.IsZero() {
		t.Fatal("opaque eight-field record invalid")
	}
	return wire
}
func relayPage(t *testing.T, raw []byte) nativeCiphertextPage {
	t.Helper()
	var page nativeCiphertextPage
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &page) != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || page.Events == nil || page.NextAfter < 0 {
		t.Fatal("opaque page invalid")
	}
	return page
}
func (a *relayTestApp) rawRequest(t *testing.T, method, path string, raw []byte, access string, headers http.Header, want int) []byte {
	t.Helper()
	req, err := http.NewRequest(method, a.server.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal("relay request construction failed")
	}
	req.Header.Set("Content-Type", "application/json")
	if access != "" {
		req.Header.Set("Authorization", "Bearer "+access)
	}
	for key, v := range headers {
		req.Header[key] = v
	}
	res, err := a.server.Client().Do(req)
	if err != nil {
		t.Fatal("relay actual HTTPS failed")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil || res.StatusCode != want || res.Header.Get("Cache-Control") != "no-store" || len(res.Cookies()) != 0 {
		t.Fatalf("relay boundary status %d expected %d", res.StatusCode, want)
	}
	return body
}
func TestNativeCiphertextPreviewHTTPSExactRetryPaginationAndIsolation(t *testing.T) {
	a := newRelayTestApp(t, NativeSupported)
	grant := a.login(t, a.user)
	other := a.login(t, a.admin)
	event := uuid.New()
	group := []byte{1, 2, 3}
	cipher := []byte{0, 255, 128, 7}
	body := relayUnitBody(event, group, cipher)
	path := relayPath(a.channel)
	first := relayWire(t, a.rawRequest(t, http.MethodPost, path, body, grant.Access, nil, 201))
	if first.ChannelID != a.channel || first.UserID != a.user.ID || first.ClientEventID != event || !bytes.Equal(first.GroupID, group) || !bytes.Equal(first.Ciphertext, cipher) {
		t.Fatal("blind relay changed exact authorization/opaque bytes")
	}
	retry := relayWire(t, a.rawRequest(t, http.MethodPost, path, body, grant.Access, nil, 200))
	if retry.ID != first.ID || retry.Number != first.Number || !retry.CreatedAt.Equal(first.CreatedAt) || !bytes.Equal(retry.Ciphertext, cipher) {
		t.Fatal("retry changed original wire identity")
	}
	a.rawRequest(t, http.MethodPost, path, relayUnitBody(event, group, []byte{42}), grant.Access, nil, 409)
	a.rawRequest(t, http.MethodPost, path, relayUnitBody(event, []byte{2}, cipher), grant.Access, nil, 409)
	a.rawRequest(t, http.MethodPost, relayPath(a.otherChannel), body, grant.Access, nil, 409)
	independent := relayWire(t, a.rawRequest(t, http.MethodPost, path, body, other.Access, nil, 201))
	if independent.ID == first.ID || independent.UserID != a.admin.ID {
		t.Fatal("separate author event identity collided")
	}
	third := relayWire(t, a.rawRequest(t, http.MethodPost, path, relayUnitBody(uuid.New(), group, []byte{9}), grant.Access, nil, 201))
	p1 := relayPage(t, a.request(t, http.MethodGet, path+"?after=0&limit=2", nil, grant.Access, nil, 200))
	if len(p1.Events) != 2 || p1.Events[0].ID != first.ID || p1.Events[1].ID != independent.ID || p1.NextAfter != independent.Number {
		t.Fatal("first bounded cursor page wrong")
	}
	p2 := relayPage(t, a.request(t, http.MethodGet, path+"?after="+strconv.FormatInt(p1.NextAfter, 10)+"&limit=2", nil, grant.Access, nil, 200))
	if len(p2.Events) != 1 || p2.Events[0].ID != third.ID || p2.NextAfter != third.Number {
		t.Fatal("cursor repeated/dropped event")
	}
	empty := relayPage(t, a.request(t, http.MethodGet, path+"?after="+strconv.FormatInt(p2.NextAfter, 10), nil, grant.Access, nil, 200))
	if len(empty.Events) != 0 || empty.NextAfter != p2.NextAfter {
		t.Fatal("empty cursor advanced or returned null")
	}
	a.request(t, http.MethodGet, relayPath(uuid.New()), nil, grant.Access, nil, 404)
	a.rawRequest(t, http.MethodPost, relayPath(uuid.New()), body, grant.Access, nil, 404)
	// Existing previews still announce unavailable content: transport auth is no Core grant.
	descriptor := a.request(t, http.MethodGet, "/.well-known/mnema", nil, "", nil, 200)
	if !bytes.Contains(descriptor, []byte(`"content_authorization":"unavailable"`)) {
		t.Fatal("blind relay advertised cryptographic readiness")
	}
	a.request(t, http.MethodGet, NativePreviewPrefix+"/channels/"+a.channel.String()+"/messages", nil, grant.Access, nil, 503)
	var rows int
	if a.pool.QueryRow(context.Background(), `SELECT count(*) FROM native_ciphertext_events`).Scan(&rows) != nil || rows != 3 {
		t.Fatal("retry/conflict changed record count")
	}
}
func TestNativeCiphertextPreviewHTTPSClosedSchemaAndBoundary(t *testing.T) {
	a := newRelayTestApp(t, NativeSupported)
	grant := a.login(t, a.user)
	path := relayPath(a.channel)
	event := uuid.MustParse("abcdefab-1234-4567-8901-123456789abc")
	body := relayUnitBody(event, []byte{1}, []byte{1})
	for _, headers := range []http.Header{{"Origin": []string{""}}, {"Cookie": []string{""}}, {"Authorization": []string{"Bearer " + grant.Access, "Bearer " + grant.Access}}, {"Authorization": []string{"Bearer " + grant.Access + ", Bearer " + grant.Access}}} {
		want := 403
		if _, ok := headers["Authorization"]; ok {
			want = 401
		}
		a.rawRequest(t, http.MethodPost, path, body, grant.Access, headers, want)
	}
	a.rawRequest(t, http.MethodPost, path, body, "", nil, 401)
	for _, q := range []string{"?", "?token=x", "?after=1", "?limit=1&after=0"} {
		a.rawRequest(t, http.MethodPost, path+q, body, grant.Access, nil, 400)
	}
	for _, q := range []string{"?", "?token=x", "?limit=101", "?after=-1", "?after=00", "?after=0&after=0", "?limit=1&after=0"} {
		a.request(t, http.MethodGet, path+q, nil, grant.Access, nil, 400)
	}
	a.rawRequest(t, http.MethodGet, path, body, grant.Access, nil, 400)
	a.request(t, http.MethodDelete, path, nil, grant.Access, nil, 405)
	a.rawRequest(t, http.MethodPost, NativePreviewPrefix+"/channels/"+strings.ToUpper(event.String())+"/ciphertext-events", body, grant.Access, nil, 400)
	a.request(t, http.MethodGet, NativePreviewPrefix+"/channels/"+strings.ToUpper(event.String())+"/ciphertext-events", nil, grant.Access, nil, 400)
	for _, raw := range [][]byte{[]byte(`{"client_event_id":null}`), []byte(`{"unknown":"x"}`), append(append([]byte{}, body...), []byte(` {}`)...), []byte(`{"client_event_id":"` + event.String() + `","client_event_id":"` + event.String() + `","group_id":"AQ==","ciphertext":"AQ=="}`), relayUnitBody(uuid.Nil, []byte{1}, []byte{1}), relayUnitBody(event, []byte{1}, make([]byte, 65537))} {
		a.rawRequest(t, http.MethodPost, path, raw, grant.Access, nil, 400)
	}
	a.rawRequest(t, http.MethodPost, path, body, grant.Access, http.Header{"Content-Type": []string{"text/plain"}}, 415)
	a.rawRequest(t, http.MethodPost, path, bytes.Repeat([]byte("x"), int(nativeCiphertextBodyMax)+1), grant.Access, nil, 413)
	max := relayWire(t, a.rawRequest(t, http.MethodPost, path, relayUnitBody(uuid.New(), make([]byte, 128), make([]byte, 65536)), grant.Access, nil, 201))
	if len(max.GroupID) != 128 || len(max.Ciphertext) != 65536 {
		t.Fatal("maximum ciphertext roundtrip truncated")
	}
}
func TestNativeCiphertextPreviewHTTPSConcurrentRetry(t *testing.T) {
	a := newRelayTestApp(t, NativeSupported)
	grant := a.login(t, a.user)
	path := relayPath(a.channel)
	body := relayUnitBody(uuid.New(), []byte{7}, []byte{0, 255, 7})
	const n = 8
	type result struct {
		status int
		wire   chat.NativeCiphertextWire
		err    bool
	}
	results := make(chan result, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, e := http.NewRequest(http.MethodPost, a.server.URL+path, bytes.NewReader(body))
			if e != nil {
				results <- result{err: true}
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+grant.Access)
			res, e := a.server.Client().Do(req)
			if e != nil {
				results <- result{err: true}
				return
			}
			defer res.Body.Close()
			var w chat.NativeCiphertextWire
			e = json.NewDecoder(res.Body).Decode(&w)
			results <- result{status: res.StatusCode, wire: w, err: e != nil || res.Header.Get("Cache-Control") != "no-store"}
		}()
	}
	wg.Wait()
	close(results)
	created := 0
	var id uuid.UUID
	var number int64
	for r := range results {
		if r.err || (r.status != 201 && r.status != 200) {
			t.Fatal("concurrent TLS relay failed")
		}
		if r.status == 201 {
			created++
		}
		if id == uuid.Nil {
			id = r.wire.ID
			number = r.wire.Number
		}
		if r.wire.ID != id || r.wire.Number != number || !bytes.Equal(r.wire.Ciphertext, []byte{0, 255, 7}) {
			t.Fatal("concurrent retry changed event")
		}
	}
	var rows int
	if created != 1 || a.pool.QueryRow(context.Background(), `SELECT count(*) FROM native_ciphertext_events`).Scan(&rows) != nil || rows != 1 {
		t.Fatal("concurrent retry inserted more than one record")
	}
}
func relayWaitForChannelLock(t *testing.T, a *relayTestApp) {
	t.Helper()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var waiting bool
		err := a.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT id FROM channels WHERE id=%')`).Scan(&waiting)
		if err != nil {
			t.Fatal("lock observation failed")
		}
		if waiting {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("real relay SQL did not reach held channel lock")
}
func TestNativeCiphertextPreviewHTTPSCancellationRollsBack(t *testing.T) {
	a := newRelayTestApp(t, NativeSupported)
	grant := a.login(t, a.user)
	lock, err := a.pool.Begin(context.Background())
	if err != nil {
		t.Fatal("channel lock transaction failed")
	}
	defer lock.Rollback(context.Background())
	if _, err = lock.Exec(context.Background(), `SELECT id FROM channels WHERE id=$1 FOR UPDATE`, a.channel); err != nil {
		t.Fatal("channel barrier failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.server.URL+relayPath(a.channel), bytes.NewReader(relayUnitBody(uuid.New(), []byte{1}, []byte{9})))
	if err != nil {
		t.Fatal("cancel request failed")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+grant.Access)
	done := make(chan error, 1)
	go func() {
		res, e := a.server.Client().Do(req)
		if res != nil {
			_ = res.Body.Close()
		}
		done <- e
	}()
	relayWaitForChannelLock(t, a)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled HTTPS request succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not terminate request")
	}
	if lock.Rollback(context.Background()) != nil {
		t.Fatal("channel barrier release failed")
	}
	// Poll genuine rollback completion, then prove no event and the same family usable.
	deadline := time.Now().Add(time.Second)
	for {
		var waiting bool
		if a.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT id FROM channels WHERE id=%')`).Scan(&waiting) != nil {
			t.Fatal("rollback observation failed")
		}
		if !waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("canceled SQL retained lock waiter")
		}
		time.Sleep(5 * time.Millisecond)
	}
	var rows int
	if a.pool.QueryRow(context.Background(), `SELECT count(*) FROM native_ciphertext_events`).Scan(&rows) != nil || rows != 0 {
		t.Fatal("canceled relay committed ciphertext")
	}
	a.rawRequest(t, http.MethodPost, relayPath(a.channel), relayUnitBody(uuid.New(), []byte{1}, []byte{8}), grant.Access, nil, 201)
}
func TestNativeCiphertextPreviewHTTPSRevokeLinearizesWithBlockedWrite(t *testing.T) {
	a := newRelayTestApp(t, NativeSupported)
	grant := a.login(t, a.user)
	principal, err := a.relay.Sessions.AuthenticateAccess(context.Background(), grant.Access)
	if err != nil {
		t.Fatal("actual principal failed")
	}
	lock, err := a.pool.Begin(context.Background())
	if err != nil {
		t.Fatal("revoke fixture transaction failed")
	}
	defer lock.Rollback(context.Background())
	if _, err = lock.Exec(context.Background(), `SELECT id FROM channels WHERE id=$1 FOR UPDATE`, a.channel); err != nil {
		t.Fatal("revoke channel barrier failed")
	}
	done := make(chan int, 1)
	go func() {
		req, e := http.NewRequest(http.MethodPost, a.server.URL+relayPath(a.channel), bytes.NewReader(relayUnitBody(uuid.New(), []byte{1}, []byte{7})))
		if e != nil {
			done <- 0
			return
		}
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
	revoked := make(chan error, 1)
	go func() { revoked <- a.relay.Sessions.RevokeFamily(context.Background(), principal) }()
	// Observe the real revoker blocked behind this authorized transaction's account lock.
	deadline := time.Now().Add(time.Second)
	for {
		var waiting bool
		if a.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT %FROM users WHERE id=%FOR UPDATE%')`).Scan(&waiting) != nil {
			t.Fatal("revoke lock observation failed")
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("revoke did not contend on account lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if lock.Rollback(context.Background()) != nil {
		t.Fatal("revoke channel barrier release failed")
	}
	select {
	case status := <-done:
		if status != 201 {
			t.Fatal("authorized earlier transaction failed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocked write did not finish")
	}
	select {
	case e := <-revoked:
		if e != nil {
			t.Fatal("family revoke failed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("revoke did not finish")
	}
	a.request(t, http.MethodGet, relayPath(a.channel), nil, grant.Access, nil, 401)
	a.rawRequest(t, http.MethodPost, relayPath(a.channel), relayUnitBody(uuid.New(), []byte{1}, []byte{2}), grant.Access, nil, 401)
	var rows int
	if a.pool.QueryRow(context.Background(), `SELECT count(*) FROM native_ciphertext_events`).Scan(&rows) != nil || rows != 1 {
		t.Fatal("post-revoke relay mutation occurred")
	}
}
func TestNativeCiphertextPreviewHTTPSReaderCannotSkipPendingCommit(t *testing.T) {
	a := newRelayTestApp(t, NativeSupported)
	writerGrant := a.login(t, a.user)
	readerGrant := a.login(t, a.admin)
	writer, err := a.relay.Sessions.AuthenticateAccess(context.Background(), writerGrant.Access)
	if err != nil {
		t.Fatal("cursor writer principal failed")
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- a.relay.Sessions.WithAuthorizedTransaction(context.Background(), writer, func(ctx context.Context, tx pgx.Tx, _ auth.User) error {
			if _, e := tx.Exec(ctx, `SELECT id FROM channels WHERE id=$1 FOR UPDATE`, a.channel); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, `INSERT INTO native_ciphertext_events(id,channel_id,user_id,client_event_id,group_id,ciphertext)VALUES($1,$2,$3,$4,$5,$6)`, uuid.New(), a.channel, a.user.ID, uuid.New(), []byte{1}, []byte{11}); e != nil {
				return e
			}
			close(entered)
			<-release
			return nil
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("pending cursor writer not reached")
	}
	listed := make(chan []byte, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodGet, a.server.URL+relayPath(a.channel)+"?after=0&limit=1", nil)
		req.Header.Set("Authorization", "Bearer "+readerGrant.Access)
		res, e := a.server.Client().Do(req)
		if e != nil {
			listed <- nil
			return
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			listed <- nil
			return
		}
		data, _ := io.ReadAll(res.Body)
		listed <- data
	}()
	relayWaitForChannelLock(t, a)
	select {
	case <-listed:
		t.Fatal("reader advanced over pending lower event")
	default:
	}
	close(release)
	if <-writerDone != nil {
		t.Fatal("pending event commit failed")
	}
	select {
	case raw := <-listed:
		page := relayPage(t, raw)
		if len(page.Events) != 1 || !bytes.Equal(page.Events[0].Ciphertext, []byte{11}) || page.NextAfter != page.Events[0].Number {
			t.Fatal("reader skipped pending lower event")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reader failed after earlier commit")
	}
}
