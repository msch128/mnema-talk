// Package chattest holds the test fixtures shared by internal/chat and its
// sub-packages, so a test file can follow the code it tests into a sub-package
// without dragging along a copy of the helpers. It must not import
// internal/chat or any of its sub-packages (they import it from their tests).
package chattest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
)

// Request builds a POST request for u with the given body, chi URL parameters
// and raw query string, ready to hand to a handler method directly.
func Request(u *auth.User, body string, params map[string]string, query string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/?"+query, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	route := chi.NewRouteContext()
	for key, value := range params {
		route.URLParams.Add(key, value)
	}
	ctx := context.WithValue(auth.WithUser(r.Context(), u), chi.RouteCtxKey, route)
	return r.WithContext(ctx)
}

// RequireError fails the test unless err is non-nil and mentions text.
func RequireError(t *testing.T, err error, text string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), text) {
		t.Fatalf("error = %v; want containing %q", err, text)
	}
}

// Ptr returns a pointer to v.
func Ptr[T any](v T) *T { return &v }

// DriftPool models a database with an incompatible relation in its schema.
// It uses real PostgreSQL views so both SQL errors and pgx decoding errors
// travel through the same protocol as production, without replacing pgx.
func DriftPool(t *testing.T, p *db.Pool, views map[string]string) *db.Pool {
	t.Helper()
	schema := "chat_drift_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := p.Exec(context.Background(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	for relation, selectSQL := range views {
		if _, err := p.Exec(context.Background(), "CREATE VIEW "+schema+"."+relation+" AS "+selectSQL); err != nil {
			t.Fatal(err)
		}
	}
	cfg := p.Config().Copy()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &db.Pool{Pool: pool}
}
