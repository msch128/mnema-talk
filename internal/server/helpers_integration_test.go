//go:build integration

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// TestMain removes the shared Postgres test container after the run.
func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

type app struct {
	t      *testing.T
	srv    *httptest.Server
	db     *db.Pool
	store  *media.MemoryStore
	events *events.Recorder
	router *Router
}

// newApp boots the real router against the shared test database. When
// withHub is true the WebSocket hub is the event publisher (for WS tests);
// otherwise events are recorded.
func newApp(t *testing.T, withHub bool) *app {
	t.Helper()
	return newAppWith(t, withHub, nil)
}

// newAppWithConfig is newApp (no hub) with a hook to adjust the config.
func newAppWithConfig(t *testing.T, adjust func(*config.Config)) *app {
	t.Helper()
	return newAppWith(t, false, adjust)
}

func newAppWith(t *testing.T, withHub bool, adjust func(*config.Config)) *app {
	t.Helper()
	pool := testutil.DB(t)
	testutil.Reset(t, pool)

	a := &app{t: t, db: pool, store: media.NewMemoryStore(), events: &events.Recorder{}}
	a.srv = httptest.NewUnstartedServer(nil)
	url := "http://" + a.srv.Listener.Addr().String()

	cfg, err := config.FromEnv(func(k string) (string, bool) {
		v, ok := map[string]string{
			"DATABASE_URL":         "unused",
			"PUBLIC_URL":           url,
			"JWT_SECRET":           "integration-test-secret-integration-test",
			"MEDIA_RETENTION_DAYS": "0",
		}[k]
		return v, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	if adjust != nil {
		adjust(cfg)
	}
	deps := Deps{Config: cfg, DB: pool, Store: a.store}
	if !withHub {
		deps.Events = a.events
	}
	a.router, err = NewRouter(deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.router.Close)
	a.srv.Config.Handler = a.router
	a.srv.Start()
	t.Cleanup(a.srv.Close)
	return a
}

func (a *app) origin() string { return a.srv.URL }

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

// client is a browser-like session with its own cookie jar.
type client struct {
	a    *app
	http *http.Client
	user *auth.User
}

func (a *app) anon() *client {
	jar, _ := cookiejar.New(nil)
	return &client{a: a, http: &http.Client{Jar: jar}}
}

type response struct {
	status int
	body   []byte
	header http.Header
}

func (r response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func (r response) code(t *testing.T) string {
	t.Helper()
	var e struct {
		Error struct{ Code string } `json:"error"`
	}
	_ = json.Unmarshal(r.body, &e)
	return e.Error.Code
}

func (c *client) do(method, path string, body any, hdr map[string]string) response {
	c.a.t.Helper()
	var rd io.Reader
	isJSON := false
	switch b := body.(type) {
	case nil:
	case io.Reader:
		rd = b
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
		isJSON = true
	}
	req, _ := http.NewRequest(method, c.a.srv.URL+path, rd)
	if isJSON {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", c.a.origin())
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.a.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return response{status: res.StatusCode, body: b, header: res.Header}
}

func (c *client) get(path string) response            { return c.do(http.MethodGet, path, nil, nil) }
func (c *client) post(path string, body any) response { return c.do(http.MethodPost, path, body, nil) }
func (c *client) put(path string, body any) response  { return c.do(http.MethodPut, path, body, nil) }
func (c *client) delete(path string) response         { return c.do(http.MethodDelete, path, nil, nil) }
func (c *client) patch(path string, body any) response {
	return c.do(http.MethodPatch, path, body, nil)
}

// upload posts a multipart file with an explicit part Content-Type.
func (c *client) upload(path, field, filename, contentType string, data []byte, fields map[string]string) response {
	c.a.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="`+field+`"; filename="`+filename+`"`)
	h.Set("Content-Type", contentType)
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(data)
	_ = mw.Close()
	return c.do(http.MethodPost, path, &buf, map[string]string{"Content-Type": mw.FormDataContentType()})
}

// seedAdmin creates the admin account directly and logs it in.
func (a *app) seedAdmin() *client {
	a.t.Helper()
	if err := auth.EnsureAdminUser(context.Background(), a.db, "Herzog", "admin-password-123"); err != nil {
		a.t.Fatal(err)
	}
	return a.login("Herzog", "admin-password-123")
}

func (a *app) login(username, password string) *client {
	a.t.Helper()
	c := a.anon()
	res := c.post("/api/auth/login", map[string]string{"username": username, "password": password})
	if res.status != http.StatusOK {
		a.t.Fatalf("login %s: %d %s", username, res.status, res.body)
	}
	var out struct{ User auth.User }
	res.decode(a.t, &out)
	c.user = &out.User
	return c
}

// register creates a regular member via a fresh invite from admin.
func (a *app) register(admin *client, username string) *client {
	a.t.Helper()
	inv := admin.post("/api/admin/invites", map[string]any{})
	if inv.status != http.StatusCreated {
		a.t.Fatalf("create invite: %d %s", inv.status, inv.body)
	}
	var invite auth.Invite
	inv.decode(a.t, &invite)

	c := a.anon()
	res := c.post("/api/auth/register", map[string]string{
		"username": username, "password": "member-password-123", "invite_code": invite.Code,
	})
	if res.status != http.StatusCreated {
		a.t.Fatalf("register %s: %d %s", username, res.status, res.body)
	}
	var out struct{ User auth.User }
	res.decode(a.t, &out)
	c.user = &out.User
	return c
}

// createChannel makes a community channel through the admin API.
func (a *app) createChannel(admin *client, name, kind string) uuid.UUID {
	a.t.Helper()
	res := admin.post("/api/admin/channels", map[string]any{"name": name, "type": kind})
	if res.status != http.StatusCreated {
		a.t.Fatalf("create channel: %d %s", res.status, res.body)
	}
	var ch struct{ ID uuid.UUID }
	res.decode(a.t, &ch)
	return ch.ID
}

func (a *app) send(c *client, channel uuid.UUID, content string) uuid.UUID {
	a.t.Helper()
	res := c.post("/api/channels/"+channel.String()+"/messages", map[string]any{"content": content})
	if res.status != http.StatusCreated {
		a.t.Fatalf("send message: %d %s", res.status, res.body)
	}
	var m struct{ ID uuid.UUID }
	res.decode(a.t, &m)
	return m.ID
}
