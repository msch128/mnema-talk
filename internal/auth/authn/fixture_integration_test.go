//go:build integration

package authn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/testutil"
	"golang.org/x/crypto/bcrypt"
)

func TestMain(m *testing.M) {
	auth.SetPasswordCostForTests(bcrypt.MinCost)
	os.Exit(testutil.Main(m))
}

func authFixture(t *testing.T) (*db.Pool, *auth.User, *auth.User) {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	if err := auth.EnsureAdminUser(ctx, p, "cool_guy", testPassword, true); err != nil {
		t.Fatal(err)
	}
	admin, _, err := auth.Login(ctx, p, "cool_guy", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.CreateInvite(ctx, p, admin.ID, "member-invite-k7x2", nil, nil); err != nil {
		t.Fatal(err)
	}
	member, err := auth.Register(ctx, p, "member", "", testPassword, "member-invite-k7x2")
	if err != nil {
		t.Fatal(err)
	}
	return p, admin, member
}

// authQueryHook uses pgx's public tracing contract to coordinate a real
// concurrent change or cancel a real database query at a specific boundary.
// It does not replace the database, rows, errors or production auth functions.
type authQueryHook struct {
	match  string
	once   sync.Once
	before func()
	// after runs when the first matching query has finished.
	after  func()
	armed  bool
	cancel bool
	hit    bool
}

func (h *authQueryHook) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if strings.Contains(strings.Join(strings.Fields(d.SQL), " "), h.match) {
		h.once.Do(func() {
			h.hit = true
			h.armed = h.after != nil
			if h.before != nil {
				h.before()
			}
			if h.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
		})
	}
	return ctx
}

func (h *authQueryHook) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
	if h.armed {
		h.armed = false
		h.after()
	}
}

func authHookPool(t *testing.T, source *db.Pool, hook *authQueryHook) *db.Pool {
	t.Helper()
	config := source.Config().Copy()
	config.ConnConfig.Tracer = hook
	config.MinConns = 0
	p, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	t.Cleanup(func() {
		if !hook.hit {
			t.Errorf("query hook did not reach %q", hook.match)
		}
	})
	return &db.Pool{Pool: p}
}

func authRequest(t *testing.T, h httpx.HandlerFunc, user *auth.User, body string, id uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r = r.WithContext(auth.WithUser(r.Context(), user))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id.String())
	rc.URLParams.Add("userID", id.String())
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
	w := httptest.NewRecorder()
	httpx.Handle(h).ServeHTTP(w, r)
	return w
}

func requestWithToken(s *auth.Sessions, token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: s.CookieName(), Value: token})
	return r
}
