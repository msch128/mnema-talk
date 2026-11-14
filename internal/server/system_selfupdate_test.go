package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/update"
)

func selfUpdateRequest() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"password":"fixture-password","target_version":"0.4.0"}`))
	return r.WithContext(auth.WithUser(r.Context(), &auth.User{ID: uuid.New(), Username: "fixture-admin", Role: "admin"}))
}

func TestConcurrentSelfUpdatesReserveOneSlot(t *testing.T) {
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.4.0","html_url":"https://github.com/msch128/mnema-talk/releases/tag/v0.4.0"}`))
	}))
	defer releases.Close()
	checker := update.NewChecker(releases.URL, "0.3.0")
	if err := checker.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	sidecarEntered, sidecarRelease := make(chan struct{}), make(chan struct{})
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(sidecarEntered)
		<-sidecarRelease
		w.WriteHeader(http.StatusAccepted)
	}))
	defer sidecar.Close()
	defer func() {
		select {
		case <-sidecarRelease:
		default:
			close(sidecarRelease)
		}
	}()
	updater, err := update.NewUpdater(sidecar.URL, strings.Repeat("fixture-", 5))
	if err != nil {
		t.Fatal(err)
	}
	confirmEntered, confirmRelease := make(chan struct{}, 2), make(chan struct{}, 2)
	h := &systemHandler{cfg: &config.Config{AppImage: "ghcr.io/mnema-xyz/mnema-talk:latest"}, version: "0.3.0", updates: checker,
		self: selfUpdate{updater: updater, now: time.Now, confirm: func(context.Context, uuid.UUID, string) (time.Duration, error) {
			confirmEntered <- struct{}{}
			<-confirmRelease
			return 0, nil
		}}}
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			rec := httptest.NewRecorder()
			if err := h.startSelfUpdate(rec, selfUpdateRequest()); err != nil {
				httpx.WriteError(rec, err)
			}
			results <- rec
		}()
	}
	for range 2 {
		select {
		case <-confirmEntered:
		case <-time.After(3 * time.Second):
			t.Fatal("both requests did not reach confirmation")
		}
	}
	confirmRelease <- struct{}{}
	select {
	case <-sidecarEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("updater was not called")
	}
	if retry, blocked := h.selfUpdateBlocked(); !blocked || retry != time.Minute {
		t.Fatalf("in-flight retry=%v blocked=%v", retry, blocked)
	}
	confirmRelease <- struct{}{}
	rejected := <-results
	if rejected.Code != http.StatusTooManyRequests {
		t.Fatalf("concurrent request=%d %s", rejected.Code, rejected.Body.String())
	}
	close(sidecarRelease)
	accepted := <-results
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("first request=%d %s", accepted.Code, accepted.Body.String())
	}
	h.self.lastCall = time.Now().Add(-2 * selfUpdateCooldown)
	if retry, blocked := h.selfUpdateBlocked(); blocked || retry != 0 {
		t.Fatalf("expired retry=%v blocked=%v", retry, blocked)
	}
}

func TestSelfUpdateRejectsMalformedBodyAndSanitizesErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := (&systemHandler{}).startSelfUpdate(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"password":`))); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	if got := updaterMessage(errors.New("private details")); got != "the updater could not start the update; see the updater's log" {
		t.Fatal(got)
	}
	if got := updaterMessage(update.ErrUpdaterUnreachable); got != update.ErrUpdaterUnreachable.Error() {
		t.Fatal(got)
	}
}
