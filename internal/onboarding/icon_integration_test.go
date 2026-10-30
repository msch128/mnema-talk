//go:build integration

package onboarding

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/testutil"
)

// brokenStore fails every read, as storage does during an outage.
type brokenStore struct{ media.Store }

func (brokenStore) GetObjectFrom(context.Context, string, int64) (io.ReadCloser, error) {
	return nil, errors.New("storage down")
}

func TestIconErrorsAreNeverCached(t *testing.T) {
	p := testutil.DB(t)
	testutil.Reset(t, p)
	if _, err := p.Exec(context.Background(), `UPDATE instance_settings SET community_icon_version = 'v1', community_icon_updated_at = NOW()`); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(p, nil)
	r := chi.NewRouter()
	h.MountPublic(r)

	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/community/icon?size=128&v=v1", nil))
		return w
	}
	for name, store := range map[string]media.Store{"storage down": brokenStore{}, "object missing": media.NewMemoryStore()} {
		h.Store = store
		w := get()
		if w.Code < 400 {
			t.Fatalf("%s: %d", name, w.Code)
		}
		cc := w.Header().Get("Cache-Control")
		if cc != "no-store" || strings.Contains(cc, "immutable") || w.Header().Get("ETag") != "" {
			t.Errorf("%s: an error must carry no-store and no validators: Cache-Control %q, ETag %q", name, cc, w.Header().Get("ETag"))
		}
	}
}
