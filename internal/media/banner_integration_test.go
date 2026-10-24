//go:build integration

package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/imgproc"
)

func bannerPNG(t *testing.T, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func bannerRequest(t *testing.T, f *mediaFixture, method string, parts ...formPart) *http.Request {
	t.Helper()
	r := uploadRequest(t, parts...)
	r.Method = method
	return r.WithContext(auth.WithUser(r.Context(), f.user))
}

func TestBannerRoutesNeedFileStorage(t *testing.T) {
	f := mediaDB(t)
	f.h.Store = nil
	put := bannerRequest(t, f, http.MethodPut, formPart{name: "banner", filename: "b.png", mime: "image/png", value: bannerPNG(t, 600, 200)})
	requireAPIError(t, f.h.putBanner(httptest.NewRecorder(), put), http.StatusServiceUnavailable)
	del := bannerRequest(t, f, http.MethodDelete)
	requireAPIError(t, f.h.deleteBanner(httptest.NewRecorder(), del), http.StatusServiceUnavailable)
}

func TestBannerUploadWithoutFileOrWithBrokenImageIsRefused(t *testing.T) {
	f := mediaDB(t)
	w := httptest.NewRecorder()
	requireAPIError(t, f.h.putBanner(w, bannerRequest(t, f, http.MethodPut, formPart{name: "other", value: "x"})), http.StatusBadRequest)
	// A PNG header that decodes to nothing passes the type check and fails the redraw.
	broken := "\x89PNG\r\n\x1a\n" + "not really a picture, only a signature and filler bytes"
	requireAPIError(t, f.h.putBanner(w, bannerRequest(t, f, http.MethodPut, formPart{name: "banner", filename: "b.png", mime: "image/png", value: broken})), http.StatusBadRequest)
	if f.store.Len() != 0 {
		t.Fatalf("a refused upload stored %d objects", f.store.Len())
	}
}

func TestBannerStorageFailureLeavesNothingBehind(t *testing.T) {
	f := mediaDB(t)
	sentinel := errors.New("storage unavailable")
	f.store.uploadErr = sentinel
	err := f.h.putBanner(httptest.NewRecorder(), bannerRequest(t, f, http.MethodPut, formPart{name: "banner", filename: "b.png", mime: "image/png", value: bannerPNG(t, 600, 200)}))
	if !errors.Is(err, sentinel) {
		t.Fatalf("store error: %v", err)
	}
}

func TestBannerThatCannotBeRecordedIsRemovedFromStorage(t *testing.T) {
	f := mediaDB(t)
	banner, err := imgproc.ProcessBanner([]byte(bannerPNG(t, 600, 200)))
	if err != nil {
		t.Fatal(err)
	}
	// No such account: the row cannot be written, so the object must not stay.
	if err := f.h.setProfileImage(context.Background(), bannerImage, uuid.New(), banner); err == nil {
		t.Fatal("recording a banner for an unknown account must fail")
	}
	if f.store.Len() != 0 {
		t.Fatalf("the unreferenced object stayed in storage: %d", f.store.Len())
	}
	if _, err := f.h.clearProfileImage(context.Background(), bannerImage, uuid.New()); err == nil {
		t.Fatal("clearing the banner of an unknown account must fail")
	}
}
