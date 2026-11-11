//go:build integration

package media

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMediaServingAndDashboardQueryFailures(t *testing.T) {
	f := mediaDB(t)
	for name, handler := range map[string]func(http.ResponseWriter, *http.Request) error{"stats": f.h.stats, "list": f.h.list, "serve": f.h.serve, "delete": f.h.delete, "prune": f.h.prune} {
		t.Run(name, func(t *testing.T) {
			r := f.req(cancelledRequest(), "id", uuid.NewString())
			r.URL.RawQuery = "days=1"
			if err := handler(httptest.NewRecorder(), r); err == nil {
				t.Fatal("canceled query succeeded")
			}
		})
	}
	for _, handler := range []func(http.ResponseWriter, *http.Request) error{f.h.serve, f.h.delete} {
		requireAPIError(t, handler(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodGet, "/", nil), "id", "invalid")), 400)
	}
	requireAPIError(t, f.h.serve(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodGet, "/", nil), "id", uuid.NewString())), 404)
	f.h.RetentionDays = 0
	for _, days := range []string{"", "0", "3651", "bad"} {
		r := httptest.NewRequest(http.MethodPost, "/?days="+days, nil)
		requireAPIError(t, f.h.prune(httptest.NewRecorder(), r), 400)
	}
	// Query normalization clamps a negative dashboard offset to zero.
	id := f.attachment(t, "uploads/list", false)
	w := httptest.NewRecorder()
	if err := f.h.list(w, httptest.NewRequest(http.MethodGet, "/?offset=-4", nil)); err != nil || !strings.Contains(w.Body.String(), id.String()) {
		t.Fatalf("negative offset dashboard=%s %v", w.Body.String(), err)
	}
}

type getFailureStore struct {
	Store
	err error
}

func (s getFailureStore) GetObjectFrom(context.Context, string, int64) (io.ReadCloser, error) {
	return nil, s.err
}

func TestMediaServeStorageFailure(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "uploads/a", false)
	sentinel := errors.New("object storage unavailable")
	f.h.Store = getFailureStore{Store: f.store, err: sentinel}
	r := f.req(httptest.NewRequest(http.MethodGet, "/", nil), "id", id.String())
	if err := f.h.serve(httptest.NewRecorder(), r); !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "fetch media object") {
		t.Fatalf("fetch failure=%v", err)
	}
}

func TestMediaStorageMissingAndSafeInlineImage(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "uploads/missing", false)
	if err := f.store.Delete(context.Background(), "uploads/missing"); err != nil {
		t.Fatal(err)
	}
	requireAPIError(t, f.h.serve(httptest.NewRecorder(), f.req(httptest.NewRequest(http.MethodGet, "/", nil), "id", id.String())), 410)
	execMedia(t, f.p, `UPDATE media SET mime_type='image/png',size_bytes=$1 WHERE id=$2`, len(pngBytes), id)
	f.store.Put("uploads/missing", pngBytes, time.Now())
	w := httptest.NewRecorder()
	if err := f.h.serve(w, f.req(httptest.NewRequest(http.MethodGet, "/", nil), "id", id.String())); err != nil || w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "inline") || w.Body.String() != string(pngBytes) {
		t.Fatalf("inline media code=%d error=%v headers=%v", w.Code, err, w.Header())
	}
	r := f.req(uploadRequest(t, formPart{name: "avatar", filename: "a.png", mime: "image/png"}), "", "")
	requireAPIError(t, f.h.uploadAvatar(httptest.NewRecorder(), r), 400)
	r = f.req(uploadRequest(t, formPart{name: "file", filename: "evil.png", mime: "image/png", value: string(htmlBytes)}), "channelID", f.channel.String())
	requireAPIError(t, f.h.upload(httptest.NewRecorder(), r), 415)
}
