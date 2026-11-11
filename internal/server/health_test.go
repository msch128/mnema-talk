package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/msch128/mnema-talk/internal/community"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
	"github.com/msch128/mnema-talk/internal/webbundle"
	"github.com/msch128/mnema-talk/internal/ws"
)

func TestHealthCachesThePing(t *testing.T) {
	var pings atomic.Int32
	hc := &healthCheck{ttl: time.Hour, ping: func(context.Context) error {
		pings.Add(1)
		return nil
	}}
	h := health(hc, "1.2.3", nil)

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("status %d", rec.Code)
			}
		}()
	}
	wg.Wait()
	if n := pings.Load(); n != 1 {
		t.Fatalf("database pinged %d times for 50 requests within the TTL, want 1", n)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	var body Health
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Status != "ok" || body.Version != "1.2.3" || body.WebClientAPI != 1 {
		t.Fatalf("body %s err %v", rec.Body, err)
	}
}

func TestHealthRechecksAfterTTL(t *testing.T) {
	var pings atomic.Int32
	fail := atomic.Bool{}
	fail.Store(true)
	hc := &healthCheck{ttl: 250 * time.Millisecond, ping: func(context.Context) error {
		pings.Add(1)
		if fail.Load() {
			return errors.New("down")
		}
		return nil
	}}
	h := health(hc, "", nil)
	get := func() int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		return rec.Code
	}
	if c := get(); c != http.StatusServiceUnavailable {
		t.Fatalf("db down: status %d", c)
	}
	fail.Store(false)
	if c := get(); c != http.StatusServiceUnavailable {
		t.Fatalf("cached failure expected within the TTL, got %d", c)
	}
	// Once the TTL is over the next request pings again.
	deadline := time.Now().Add(5 * time.Second)
	c := get()
	for c != http.StatusOK && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		c = get()
	}
	if c != http.StatusOK {
		t.Fatalf("db back: status %d", c)
	}
	if pings.Load() != 2 {
		t.Fatalf("pings = %d, want 2", pings.Load())
	}
}

// While object storage is down the answer stays 503, but it still says what
// the server is: a web app needs the feature list and a desktop client the
// contract number, and chat and Talks work without storage.
func TestHealthDescribesTheServerWhileStorageIsDown(t *testing.T) {
	storageErr := errors.New("s3 down")
	hc := &healthCheck{ttl: 0, ping: func(context.Context) error { return nil }, storage: func() error { return storageErr }}
	h := health(hc, "1.2.3", community.New(nil, "https://talk.example.org"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	var body HealthUnavailable
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || body.Error == nil || body.Error.Code != httpx.CodeUnavailable || !strings.Contains(body.Error.Message, "storage") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if body.Status != "unavailable" || !slices.Equal(body.Unavailable, []string{"storage"}) {
		t.Fatalf("status %q, unavailable %v, want unavailable and [storage]", body.Status, body.Unavailable)
	}
	if body.Version != "1.2.3" || body.WebClientAPI != WebClientAPI || body.CommunityName != "talk.example.org" || !slices.Equal(body.Features, Features) {
		t.Fatalf("the 503 leaves out what the 200 says about the server: %s", rec.Body)
	}
	storageErr = nil
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "unavailable") {
		t.Fatalf("storage back: status %d body %s", rec.Code, rec.Body)
	}
}

// refusingQuerier fails the test when the community name is read from the database.
type refusingQuerier struct {
	db.Querier
	t *testing.T
}

func (q refusingQuerier) QueryRow(context.Context, string, ...any) pgx.Row {
	q.t.Error("health read the community name from a database that does not answer")
	return nil
}

func TestHealthNamesTheDatabaseFirstAndDoesNotWaitForIt(t *testing.T) {
	hc := &healthCheck{ttl: 0, ping: func(context.Context) error { return errors.New("down") }, storage: func() error { return errors.New("s3 down") }}
	rec := httptest.NewRecorder()
	health(hc, "1.2.3", community.New(refusingQuerier{t: t}, "https://talk.example.org")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	var body HealthUnavailable
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || body.Error == nil || body.Error.Message != "database unavailable" || !slices.Equal(body.Unavailable, []string{"database", "storage"}) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	if body.CommunityName != "talk.example.org" || body.WebClientAPI != WebClientAPI {
		t.Fatalf("body %s", rec.Body)
	}
}

// The liveness probe is for orchestrators that restart on a failed probe: it
// must stay 200 while the database and object storage are both down.
func TestLivenessAnswersWhileTheDependenciesAreDown(t *testing.T) {
	router, err := NewRouter(Deps{
		Config:       &config.Config{AllowedOrigins: []string{"http://example.test"}, JWTSecret: "router-test-secret-router-test-secret"},
		Perms:        permtest.NewFixture().Service(),
		StorageReady: func() error { return errors.New("s3 down") },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if rec := get("/api/health"); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"unavailable":["database","storage"]`) {
		t.Fatalf("health: %d %s", rec.Code, rec.Body)
	}
	rec := get("/api/health/live")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("live: %d %s", rec.Code, rec.Body)
	}
}

func TestRouterCloseStopsBackgroundWork(t *testing.T) {
	r := &Router{}
	r.Close() // no cancel set: must not panic
	ctx, cancel := context.WithCancel(context.Background())
	r = &Router{cancel: cancel, Hub: ws.NewHub(nil, nil, nil, nil, permtest.NewFixture().Service())}
	r.Close()
	if ctx.Err() == nil {
		t.Fatal("Close did not cancel the router context")
	}
	response := httptest.NewRecorder()
	r.Hub.HandleWebSocket(response, httptest.NewRequest(http.MethodGet, "/api/ws", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal("Close did not stop WebSocket admission")
	}
}

// The desktop bundle declares the web_client_api it speaks in
// web/desktop-compat.json; the server advertises WebClientAPI. A release that
// changes one without the other would make every desktop client refuse it.
func TestDesktopCompatFileMatchesServerContract(t *testing.T) {
	c, err := webbundle.LoadCompat("../../web/desktop-compat.json")
	if err != nil {
		t.Fatal(err)
	}
	if c.WebClientAPI != WebClientAPI {
		t.Fatalf("web/desktop-compat.json declares web_client_api %d, server advertises %d", c.WebClientAPI, WebClientAPI)
	}
}

// A client asks for these only when the server lists them: an older server
// rejects the unknown field (messages.nonce) or reads the path as a report id
// (reports.open_count).
func TestHealthListsTheFeaturesWebClientsWaitFor(t *testing.T) {
	hc := &healthCheck{ttl: time.Hour, ping: func(context.Context) error { return nil }}
	rec := httptest.NewRecorder()
	health(hc, "1.2.3", nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	var body Health
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"messages.nonce", "reports.open_count"} {
		if !slices.Contains(body.Features, want) {
			t.Errorf("features %v lack %s, which web clients need before they use it", body.Features, want)
		}
	}
}
