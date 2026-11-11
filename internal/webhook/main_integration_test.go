//go:build integration

package webhook

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/ssrf"
	"github.com/msch128/mnema-talk/internal/testutil"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

var png = []byte{
	0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0x0d, 'I', 'H', 'D', 'R', 0, 0, 0, 1, 0, 0, 0, 1, 8, 6, 0, 0, 0,
	0x1f, 0x15, 0xc4, 0x89, 0, 0, 0, 0x0d, 'I', 'D', 'A', 'T', 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f, 0, 5, 0, 1, 0xff,
	0x89, 0x99, 0x3d, 0x1d, 0, 0, 0, 0, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
}

type fixture struct {
	t     *testing.T
	p     *db.Pool
	store *media.MemoryStore
	h     *Handler
	owner *auth.User
	ch    uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	f := &fixture{t: t, p: p, store: media.NewMemoryStore()}
	var owner uuid.UUID
	if err := p.QueryRow(context.Background(), `
		INSERT INTO users (username, display_name, password_hash, role) VALUES ('owner', 'owner', 'x', 'admin') RETURNING id`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	f.owner = &auth.User{ID: owner, Role: "admin"}
	if err := p.QueryRow(context.Background(), `INSERT INTO channels (name, type) VALUES ('c', 'text') RETURNING id`).Scan(&f.ch); err != nil {
		t.Fatal(err)
	}
	f.h = f.handler(p)
	return f
}

// handler builds a Handler on pool; the permission model reads the healthy database.
func (f *fixture) handler(p *db.Pool) *Handler {
	perms := perm.NewService(f.p)
	m := &media.Handler{DB: p, Perms: perms, Store: f.store, Bucket: "b", MaxUploadBytes: 1 << 20, Events: &events.Recorder{}}
	policy := &ssrf.Policy{}
	h := New(Handler{
		DB: p, Perms: perms, Events: &events.Recorder{}, Media: m, PublicURL: "http://example.test/", Policy: policy,
		Outgoing: NewDispatcher(p, ssrf.NewClient(ssrf.NewDialer(policy), time.Second), "test"),
	})
	h.SetLimits(10000, 10000)
	return h
}

// faulty is a handler whose pool cancels the first statement containing query.
func (f *fixture) faulty(tr *pgfault.Tracer) *Handler {
	pool := &db.Pool{Pool: pgfault.Pool(f.t, f.p.Pool, tr)}
	return f.handler(pool)
}

// failingRecords is the database with one named step of the handlers failing.
type failingRecords struct {
	handlerStore
	// failLoad fails the n-th load of a webhook (1 is the first, 2 the
	// read-back after a change); 0 fails none.
	failLoad                            int
	failThread, failInsert, failMessage bool

	loads int
	hit   bool
}

var errRecords = errors.New("webhook store down")

func (s *failingRecords) webhook(ctx context.Context, id uuid.UUID) (*record, error) {
	if s.loads++; s.loads == s.failLoad {
		s.hit = true
		return nil, errRecords
	}
	return s.handlerStore.webhook(ctx, id)
}

func (s *failingRecords) validateThread(ctx context.Context, channel uuid.UUID, thread *uuid.UUID) error {
	if s.failThread {
		s.hit = true
		return errRecords
	}
	return s.handlerStore.validateThread(ctx, channel, thread)
}

func (s *failingRecords) insertMessage(ctx context.Context, nm messages.NewMessage, also func(pgx.Tx, uuid.UUID) error) (uuid.UUID, error) {
	if s.failInsert {
		s.hit = true
		return uuid.Nil, errRecords
	}
	return s.handlerStore.insertMessage(ctx, nm, also)
}

func (s *failingRecords) message(ctx context.Context, id uuid.UUID) (*core.Message, error) {
	if s.failMessage {
		s.hit = true
		return nil, errRecords
	}
	return s.handlerStore.message(ctx, id)
}

// failing is a handler on the healthy database whose store fails the step s
// names. The test fails when the handler never reached that step.
func (f *fixture) failing(s failingRecords) *Handler {
	f.t.Helper()
	h := f.handler(f.p)
	s.handlerStore = pgStore{db: f.p}
	h.recordsOverride = &s
	f.t.Cleanup(func() {
		if !s.hit {
			f.t.Errorf("the failing step of %+v was never reached", s)
		}
	})
	return h
}

func (f *fixture) serve(h *Handler, method, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	f.t.Helper()
	r := chi.NewRouter()
	h.MountPublic(r)
	r.Route("/admin", func(a chi.Router) {
		a.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), f.owner)))
			})
		})
		h.MountAdmin(a)
		h.MountAdminUploads(a)
	})
	req := httptest.NewRequest(method, path, body)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func (f *fixture) json(h *Handler, method, path, body string) *httptest.ResponseRecorder {
	return f.serve(h, method, path, strings.NewReader(body), map[string]string{"Content-Type": "application/json"})
}

func (f *fixture) newHook(name string) (*record, string) {
	f.t.Helper()
	rec, token, err := create(context.Background(), f.p, f.ch, f.owner.ID, name, DefaultSettings)
	if err != nil {
		f.t.Fatal(err)
	}
	return rec, token
}

func wantStatus(t *testing.T, w *httptest.ResponseRecorder, status int, what string) {
	t.Helper()
	if w.Code != status {
		t.Errorf("%s: status %d, want %d: %s", what, w.Code, status, w.Body)
	}
}
