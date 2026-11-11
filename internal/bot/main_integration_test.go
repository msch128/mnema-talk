//go:build integration

package bot

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

var png = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0,
	0x1f, 0x15, 0xc4, 0x89, 0, 0, 0, 0x0d, 'I', 'D', 'A', 'T', 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0, 5, 0, 1, 0xff,
	0x89, 0x99, 0x3d, 0x1d, 0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

type env struct {
	t     *testing.T
	p     *db.Pool
	owner *auth.User
	ch    uuid.UUID
	more  []uuid.UUID
	role  uuid.UUID
	store *media.MemoryStore
}

func newEnv(t *testing.T) *env {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	e := &env{t: t, p: p, store: media.NewMemoryStore()}
	var id uuid.UUID
	if err := p.QueryRow(context.Background(), `
		INSERT INTO users (username, display_name, password_hash, role) VALUES ('owner', 'owner', 'x', 'admin') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	e.owner = &auth.User{ID: id, Role: "admin"}
	if err := p.QueryRow(context.Background(), `INSERT INTO channels (name, type) VALUES ('c', 'text') RETURNING id`).Scan(&e.ch); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"c", "d"} {
		var other uuid.UUID
		if err := p.QueryRow(context.Background(), `INSERT INTO channels (name, type) VALUES ($1, 'text') RETURNING id`, name).Scan(&other); err != nil {
			t.Fatal(err)
		}
		e.more = append(e.more, other)
	}
	if err := p.QueryRow(context.Background(), `INSERT INTO roles (name, position, permissions) VALUES ('bots', 1, $1) RETURNING id`, int64(perm.SendMessages)).Scan(&e.role); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) handler(pool *db.Pool) *Handler {
	m := &media.Handler{DB: pool, Perms: perm.NewService(e.p), Store: e.store, Bucket: "b", MaxUploadBytes: 1 << 20, Events: &events.Recorder{}}
	return &Handler{DB: pool, Perms: perm.NewService(pool), Media: m,
		OnRevoke: func(uuid.UUID, *uuid.UUID) {}}
}

func (e *env) serve(h *Handler, method, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	return e.serveWith(h, true, method, path, body, hdr)
}

// serveWith routes to the handlers behind Manage bots (guarded), or straight
// to them, so that their own permission lookups are reached by a failure.
func (e *env) serveWith(h *Handler, guarded bool, method, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	r.Route("/admin", func(a chi.Router) {
		a.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), e.owner)))
			})
		})
		if guarded {
			h.MountAdmin(a)
			h.MountAdminUploads(a)
			return
		}
		h.mountUnguarded(a)
	})
	req := httptest.NewRequest(method, path, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func jsonBody(s string) (io.Reader, map[string]string) {
	return strings.NewReader(s), map[string]string{"Content-Type": "application/json"}
}

func avatarBody() (io.Reader, map[string]string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="avatar"; filename="a.png"`)
	h.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(png)
	_ = mw.Close()
	return &buf, map[string]string{"Content-Type": mw.FormDataContentType()}
}

// fixtureBot is a bot with a role, a token and a command.
type fixtureBot struct {
	bot   *Bot
	token Token
	cmd   Command
}

func (e *env) newBot() fixtureBot {
	e.t.Helper()
	ctx := context.Background()
	if _, err := e.p.Exec(ctx, `DELETE FROM users WHERE is_bot`); err != nil {
		e.t.Fatal(err)
	}
	b, err := Create(ctx, e.p, "helper", 0, e.owner.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.p.Exec(ctx, `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)`, b.UserID, e.role); err != nil {
		e.t.Fatal(err)
	}
	for _, ch := range append([]uuid.UUID{e.ch}, e.more...) {
		if _, err := e.p.Exec(ctx, `INSERT INTO permission_overwrites (channel_id, role_id, allow) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, ch, e.role, int64(perm.ViewChannel)); err != nil {
			e.t.Fatal(err)
		}
	}
	tok, _, err := IssueToken(ctx, e.p, b.ID, "t", nil, uuid.Nil)
	if err != nil {
		e.t.Fatal(err)
	}
	cmd, err := UpsertCommand(ctx, e.p, b.ID, CommandInput{Name: "ping", Description: "pong", Options: []Option{}})
	if err != nil {
		e.t.Fatal(err)
	}
	return fixtureBot{bot: b, token: tok, cmd: cmd}
}

// mountUnguarded registers the routes without the permission guard, for
// tests that reach the handlers' own lookups.
func (h *Handler) mountUnguarded(r chi.Router) {
	h.routes(r)
	r.Post("/bots/{id}/avatar", httpx.Handle(h.avatar))
}
